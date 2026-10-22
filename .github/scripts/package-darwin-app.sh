#!/usr/bin/env bash
# Package build/darwin-arm64/Cove.app into a zip that keeps the bundle
# layout, symlinks, and executable bits, then write a sibling .sha256 file.
# The version recorded in Info.plist must match mygo.json.

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: package-darwin-app.sh <zip-path>" >&2
  exit 2
fi

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}
need zip
need unzip
need sha256sum
need python3
need file

root=$(cd "$(dirname "$0")/../.." && pwd)
archive=$1
if [[ $archive != /* ]]; then
  archive=$root/$archive
fi
archive_dir=$(dirname "$archive")
archive_name=$(basename "$archive")
checksum=$archive_dir/${archive_name}.sha256
app_dir=$root/build/darwin-arm64
app=$app_dir/Cove.app
exe=$app/Contents/MacOS/Cove

mkdir -p "$archive_dir"

if [[ ! -d $app ]]; then
  echo "missing $app" >&2
  echo "build it with: CGO_ENABLED=0 go tool mygo build -platform darwin/arm64" >&2
  exit 1
fi
if [[ ! -f $exe ]]; then
  echo "missing $exe" >&2
  exit 1
fi
if [[ ! -x $exe ]]; then
  echo "$exe is not executable" >&2
  exit 1
fi

version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["version"])' "$root/mygo.json")

# zip -y stores symlinks as links. Dropping it copies the target instead,
# which breaks a bundle that links into itself. One function is the only
# place that flag is set, and the fixture fails when it is missing, even
# if this Cove.app has no links of its own.
zip_dir() {
  local dir=$1 dest=$2
  local parent name
  parent=$(dirname "$dir")
  name=$(basename "$dir")
  (
    cd "$parent"
    zip -ry "$dest" "$name"
  )
}

fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/dir"
echo target > "$fixture/dir/file"
ln -s file "$fixture/dir/link"
fixture_zip=$fixture/links.zip
zip_dir "$fixture/dir" "$fixture_zip" >/dev/null
python3 - "$fixture_zip" <<'PY'
import stat, sys, zipfile

archive = sys.argv[1]
with zipfile.ZipFile(archive) as z:
    info = z.getinfo("dir/link")
    mode = (info.external_attr >> 16) & 0xFFFF
    if not stat.S_ISLNK(mode):
        sys.exit(f"zip did not store a symlink (mode {mode:o}); use zip -ry")
    if z.read(info) != b"file":
        sys.exit(f"symlink target in zip is {z.read(info)!r}")
PY

rm -f "$archive" "$checksum"
zip_dir "$app" "$archive"

stage=$(mktemp -d)
# shellcheck disable=SC2064
trap "rm -rf $(printf '%q' "$fixture") $(printf '%q' "$stage")" EXIT
unzip -q "$archive" -d "$stage"

python3 - "$archive" "$app_dir" "$stage" "$version" <<'PY'
import os, plistlib, stat, sys, zipfile

archive, app_dir, stage, version = sys.argv[1:]
exe = "Cove.app/Contents/MacOS/Cove"
required = (
    "Cove.app/Contents/Info.plist",
    "Cove.app/Contents/PkgInfo",
    exe,
)

def unix_mode(info):
    return (info.external_attr >> 16) & 0xFFFF

with zipfile.ZipFile(archive) as z:
    infos = {i.filename: i for i in z.infolist()}
    for path in required:
        if path not in infos:
            sys.exit(f"{archive} is missing {path}")
    plist = plistlib.loads(z.read("Cove.app/Contents/Info.plist"))
    short = plist.get("CFBundleShortVersionString")
    bundle = plist.get("CFBundleVersion")
    if short != version or bundle != version:
        sys.exit(
            f"Info.plist version short={short!r} bundle={bundle!r} "
            f"does not match mygo.json {version!r}"
        )
    exe_mode = unix_mode(infos[exe])
    if exe_mode & 0o111 == 0:
        sys.exit(f"{exe} is mode {exe_mode:o} inside the zip, want an executable")

    for dirpath, dirnames, filenames in os.walk(app_dir, followlinks=False):
        # os.walk lists a symlinked directory in dirnames. Keep it in the
        # check below, but do not follow it: the zip must contain the link.
        entries = dirnames + filenames
        for name in list(dirnames):
            if os.path.islink(os.path.join(dirpath, name)):
                dirnames.remove(name)
        for name in entries:
            full = os.path.join(dirpath, name)
            rel = os.path.relpath(full, app_dir).replace(os.sep, "/")
            info = infos.get(rel)
            if info is None:
                info = infos.get(rel + "/")
            if info is None:
                sys.exit(f"{archive} is missing {rel}")
            mode = unix_mode(info)
            st = os.lstat(full)
            if stat.S_ISLNK(st.st_mode):
                if not stat.S_ISLNK(mode):
                    sys.exit(f"{rel} is a symlink but the zip stored a file; use zip -ry")
                target = os.readlink(full)
                stored = z.read(info).decode()
                if stored != target:
                    sys.exit(f"{rel} points at {stored!r} in the zip, not {target!r}")
            elif stat.S_ISREG(st.st_mode) and (st.st_mode & 0o111) and mode & 0o111 == 0:
                sys.exit(f"{rel} lost its executable bit in the zip (mode {mode:o})")

unpacked_exe = os.path.join(stage, "Cove.app", "Contents", "MacOS", "Cove")
unpacked_mode = os.lstat(unpacked_exe).st_mode
if not stat.S_ISREG(unpacked_mode) or unpacked_mode & 0o111 == 0:
    sys.exit(f"unzipped {exe} is mode {unpacked_mode:o}")
for rel in ("Contents/Info.plist", "Contents/PkgInfo", "Contents/Resources", "Contents/MacOS/Cove"):
    if not os.path.exists(os.path.join(stage, "Cove.app", rel)):
        sys.exit(f"unzipped bundle is missing Cove.app/{rel}")
resources = os.path.join(stage, "Cove.app", "Contents", "Resources")
if not os.path.isdir(resources) or not os.listdir(resources):
    sys.exit("unzipped Cove.app/Contents/Resources is empty")

for dirpath, dirnames, filenames in os.walk(app_dir, followlinks=False):
    entries = dirnames + filenames
    for name in list(dirnames):
        if os.path.islink(os.path.join(dirpath, name)):
            dirnames.remove(name)
    for name in entries:
        full = os.path.join(dirpath, name)
        if not os.path.islink(full):
            continue
        rel = os.path.relpath(full, app_dir)
        unpacked = os.path.join(stage, rel)
        if not os.path.islink(unpacked):
            sys.exit(f"unzipped {rel} is not a symlink")
        if os.readlink(unpacked) != os.readlink(full):
            sys.exit(f"unzipped {rel} points at {os.readlink(unpacked)!r}")

print(f"{exe} mode {exe_mode:o} in {os.path.basename(archive)}; version {version}")
PY

cmp "$exe" "$stage/Cove.app/Contents/MacOS/Cove"
if ! file "$stage/Cove.app/Contents/MacOS/Cove" | grep -q 'Mach-O.*arm64'; then
  file "$stage/Cove.app/Contents/MacOS/Cove" >&2
  echo "unzipped executable is not a darwin/arm64 Mach-O" >&2
  exit 1
fi

(
  cd "$archive_dir"
  sha256sum "$archive_name" > "${archive_name}.sha256"
  sha256sum -c "${archive_name}.sha256"
)

echo "wrote $archive_name and ${archive_name}.sha256"
