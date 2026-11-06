#!/usr/bin/env bash
# Package the unsigned Linux tar.gz and Windows zip that GitHub Releases
# attach next to the darwin zips. Does not publish a .deb, NSIS installer,
# AppImage, MSI, or an arm64 archive.
#
# Reads version from mygo.json. Writes, in the repository root:
#   Cove-<ver>-linux-amd64.tar.gz[+.sha256]
#   Cove-<ver>-windows-amd64.zip[+.sha256]

set -euo pipefail

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}
need python3
need sha256sum
need tar
need unzip
need zip
need file

root=$(cd "$(dirname "$0")/../.." && pwd)
version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["version"])' "$root/mygo.json")
linux_src=$root/build/linux-amd64/cove-${version}-linux-amd64.tar.gz
linux_dst=$root/Cove-${version}-linux-amd64.tar.gz
win_dir=$root/build/windows-amd64
win_dst=$root/Cove-${version}-windows-amd64.zip

if [[ ! -f $linux_src ]]; then
  echo "missing $linux_src" >&2
  echo "build it with: CGO_ENABLED=0 go tool mygo build -platform linux/amd64" >&2
  exit 1
fi
if [[ ! -f $win_dir/Cove.exe ]]; then
  echo "missing $win_dir/Cove.exe" >&2
  echo "build it with: CGO_ENABLED=0 go tool mygo build -platform windows/amd64" >&2
  exit 1
fi

rm -f "$linux_dst" "$linux_dst.sha256" "$win_dst" "$win_dst.sha256"
cp "$linux_src" "$linux_dst"

python3 - "$linux_dst" <<'PY'
import io, sys, tarfile

archive = sys.argv[1]
with tarfile.open(archive, "r:gz") as tar:
    names = {m.name.lstrip("./") for m in tar.getmembers()}
want = {"cove", "cove.desktop", "cove.png"}
missing = sorted(want - names)
if missing:
    sys.exit(f"{archive} is missing {missing}; has {sorted(names)}")
for banned in names:
    lower = banned.lower()
    if lower.endswith(".deb") or "setup" in lower or lower.endswith(".msi"):
        sys.exit(f"{archive} must not contain installer {banned}")
desktop = None
with tarfile.open(archive, "r:gz") as tar:
    for m in tar.getmembers():
        if m.name.lstrip("./") == "cove.desktop":
            desktop = tar.extractfile(m).read().decode()
            break
if desktop is None or "Icon=cove" not in desktop:
    sys.exit(f"{archive} cove.desktop Icon= does not point at cove")
print(f"linux archive {len(open(archive,'rb').read())} bytes; Icon=cove")
PY

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
# Ship the exe and the files MyGo copied next to it. Skip NSIS if a local
# build produced one. Skip Cove.icns: Windows does not load it. The in-app
# mark is the embedded PNG (app_icon.go); Explorer uses cove.ico.
python3 - "$win_dir" "$stage" <<'PY'
import os, shutil, sys

src, dest = sys.argv[1:]
copied = []
for name in sorted(os.listdir(src)):
    lower = name.lower()
    if lower.endswith(".exe") and name != "Cove.exe":
        continue
    if lower.endswith((".nsi", ".deb", ".msi", ".icns")) or lower == "install.sh":
        continue
    path = os.path.join(src, name)
    if os.path.isdir(path):
        shutil.copytree(path, os.path.join(dest, name))
    else:
        shutil.copy2(path, os.path.join(dest, name))
    copied.append(name)
if "Cove.exe" not in copied:
    sys.exit("windows stage has no Cove.exe")
print("windows zip contents:", ", ".join(copied))
PY
cp "$root/icon/winlinux/cove.ico" "$stage/cove.ico"

(
  cd "$stage"
  zip -r "$win_dst" .
)

python3 - "$win_dst" <<'PY'
import sys, zipfile

archive = sys.argv[1]
with zipfile.ZipFile(archive) as z:
    names = {i.filename.lstrip("./") for i in z.infolist() if not i.is_dir()}
if "Cove.exe" not in names:
    sys.exit(f"{archive} is missing Cove.exe; has {sorted(names)}")
for banned in names:
    lower = banned.lower()
    if "setup" in lower or lower.endswith((".nsi", ".deb", ".msi")):
        sys.exit(f"{archive} must not contain installer {banned}")
    if lower.endswith(".icns"):
        sys.exit(
            f"{archive} must not contain {banned}; "
            "Windows uses cove.ico and the embedded PNG, not Cove.icns"
        )
print(f"windows archive has Cove.exe and {len(names)} files")
PY

desc=$(file -b "$win_dir/Cove.exe")
echo "file: $desc"
if [[ $desc != *PE32+* || $desc != *x86-64* && $desc != *x86_64* ]]; then
  echo "Cove.exe is not a PE32+ x86-64 executable" >&2
  exit 1
fi

write_sum() {
  local archive=$1
  local dir name
  dir=$(dirname "$archive")
  name=$(basename "$archive")
  (
    cd "$dir"
    sha256sum "$name" > "${name}.sha256"
    sha256sum -c "${name}.sha256"
  )
}

write_sum "$linux_dst"
write_sum "$win_dst"

echo "wrote $(basename "$linux_dst") and $(basename "$win_dst")"
