#!/usr/bin/env bash
# Package build/<platform>/Cove.app into a zip that keeps the bundle
# layout, symlinks, and executable bits, then write a sibling .sha256 file.
# The version recorded in Info.plist must match mygo.json.
#
# platform defaults to darwin-arm64. darwin-universal is the fat arm64+x86_64
# app from `go tool mygo build -platform darwin/universal`.

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: package-darwin-app.sh <zip-path> [darwin-arm64|darwin-universal]" >&2
  exit 2
fi

platform=${2:-darwin-arm64}
case $platform in
  darwin-arm64|darwin-universal) ;;
  *)
    echo "unknown platform $platform (want darwin-arm64 or darwin-universal)" >&2
    exit 2
    ;;
esac

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
app_dir=$root/build/$platform
app=$app_dir/Cove.app
exe=$app/Contents/MacOS/Cove
go_platform=${platform/-/\/}

mkdir -p "$archive_dir"

if [[ ! -d $app ]]; then
  echo "missing $app" >&2
  echo "build it with: CGO_ENABLED=0 go tool mygo build -platform $go_platform" >&2
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

desc=$(file -b "$stage/Cove.app/Contents/MacOS/Cove")
echo "file: $desc"
case $platform in
  darwin-arm64)
    if [[ $desc != Mach-O\ 64-bit\ arm64\ executable* ]]; then
      echo "unzipped executable is not a thin darwin/arm64 Mach-O" >&2
      exit 1
    fi
    ;;
  darwin-universal)
    if [[ $desc != Mach-O\ universal\ binary* || $desc != *arm64* || $desc != *x86_64* ]]; then
      echo "unzipped executable is not a universal arm64+x86_64 Mach-O" >&2
      exit 1
    fi
    ;;
esac

python3 - "$stage/Cove.app/Contents/MacOS/Cove" "$platform" "$root" <<'PY'
import hashlib, os, plistlib, struct, sys

exe, platform, root = sys.argv[1:]
data = open(exe, "rb").read()

FAT_MAGIC = 0xCAFEBABE
MH_MAGIC_64 = 0xFEEDFACF
MH_EXECUTE = 0x2
CPU_ARM64 = 0x0100000C
CPU_X86_64 = 0x01000007
CPU_SUBTYPE_X86_64_H = 8
CPU_NAME = {CPU_ARM64: "arm64", CPU_X86_64: "x86_64"}
LC_BUILD_VERSION = 0x32
LC_CODE_SIGNATURE = 0x1D
CSMAGIC_EMBEDDED_SIGNATURE = 0xFADE0CC0
CSMAGIC_CODEDIRECTORY = 0xFADE0C02
CSSLOT_CODEDIRECTORY = 0
CS_ADHOC = 0x2
PLATFORM_MACOS = 1


def fail(msg):
    sys.exit(msg)


def verify_signature(blob, sig, sig_off, label):
    if len(sig) < 12:
        fail(f"{label} ad-hoc signature is truncated")
    magic, length, count = struct.unpack_from(">III", sig, 0)
    if magic != CSMAGIC_EMBEDDED_SIGNATURE or length != len(sig) or count < 1:
        fail(f"{label} ad-hoc signature superblob is invalid")
    cd = None
    for i in range(count):
        typ, blob_off = struct.unpack_from(">II", sig, 12 + 8 * i)
        if typ == CSSLOT_CODEDIRECTORY:
            cd = sig[blob_off:]
            break
    if cd is None or len(cd) < 44:
        fail(f"{label} ad-hoc signature has no code directory")
    cd_magic = struct.unpack_from(">I", cd, 0)[0]
    if cd_magic != CSMAGIC_CODEDIRECTORY:
        fail(f"{label} code directory magic is {cd_magic:#x}")
    flags, hash_off, _, nspec, ncode, code_limit = struct.unpack_from(">IIIIII", cd, 12)
    hash_size = cd[36]
    hash_type = cd[37]
    page_bits = cd[39]
    if hash_type != 2 or hash_size != 32 or page_bits != 12:
        fail(f"{label} code directory is not SHA-256 with 4KiB pages")
    if flags & CS_ADHOC == 0:
        fail(f"{label} code signature is not ad-hoc (flags {flags:#x})")
    if code_limit != sig_off:
        fail(f"{label} codeLimit {code_limit} does not match the signature offset {sig_off}")
    if nspec != 0:
        fail(f"{label} code directory has {nspec} special slots; the Go ad-hoc signature has none")
    page = 1 << page_bits
    hashes = cd[hash_off:hash_off + ncode * hash_size]
    if len(hashes) != ncode * hash_size:
        fail(f"{label} code directory hashes are truncated")
    for i in range(ncode):
        start = i * page
        end = min(start + page, code_limit)
        digest = hashlib.sha256(blob[start:end]).digest()
        if digest != hashes[i * hash_size:(i + 1) * hash_size]:
            fail(f"{label} ad-hoc signature does not cover page {i}")


def parse_thin(blob, label):
    if len(blob) < 32 or struct.unpack_from("<I", blob, 0)[0] != MH_MAGIC_64:
        fail(f"{label} is not a 64-bit Mach-O")
    cputype, cpusub, filetype, ncmds, sizeofcmds, _flags = struct.unpack_from("<IIIIII", blob, 4)
    arch = CPU_NAME.get(cputype)
    if arch is None:
        fail(f"{label} has cputype {cputype:#x}")
    if filetype != MH_EXECUTE:
        fail(f"{label} filetype {filetype:#x} is not MH_EXECUTE")
    if arch == "x86_64" and (cpusub & 0xFF) == CPU_SUBTYPE_X86_64_H:
        fail(f"{label} is x86_64h; that drops Intel Macs older than Haswell")
    if 32 + sizeofcmds > len(blob):
        fail(f"{label} load commands extend past the file")
    off = 32
    minos = None
    sig_off = None
    sig = None
    for _ in range(ncmds):
        if off + 8 > len(blob):
            fail(f"{label} load command is truncated")
        cmd, cmdsize = struct.unpack_from("<II", blob, off)
        if cmdsize < 8 or off + cmdsize > len(blob):
            fail(f"{label} load command is truncated")
        if cmd == LC_BUILD_VERSION and cmdsize >= 24:
            plat, ver, _sdk, _ntools = struct.unpack_from("<IIII", blob, off + 8)
            if plat != PLATFORM_MACOS:
                fail(f"{label} LC_BUILD_VERSION platform {plat} is not macOS")
            minos = f"{ver >> 16}.{(ver >> 8) & 0xFF}.{ver & 0xFF}"
        elif cmd == LC_CODE_SIGNATURE and cmdsize >= 16:
            dataoff, datasize = struct.unpack_from("<II", blob, off + 8)
            if dataoff + datasize > len(blob):
                fail(f"{label} code signature extends past the slice")
            sig_off = dataoff
            sig = blob[dataoff:dataoff + datasize]
        off += cmdsize
    if minos is None:
        fail(f"{label} has no LC_BUILD_VERSION")
    if sig is not None:
        verify_signature(blob, sig, sig_off, label)
    return arch, cpusub, minos, sig is not None


def fat_slices(blob):
    if len(blob) < 8 or struct.unpack_from(">I", blob, 0)[0] != FAT_MAGIC:
        return None
    nfat = struct.unpack_from(">I", blob, 4)[0]
    if nfat < 1 or 8 + 20 * nfat > len(blob):
        fail("universal header is truncated")
    slices = []
    off = 8
    for _ in range(nfat):
        cputype, _cpusub, offset, size, align = struct.unpack_from(">IIIII", blob, off)
        off += 20
        if align < 14:
            fail(f"slice alignment 2^{align} is below 16KiB")
        if offset % (1 << align) != 0:
            fail(f"slice offset {offset} is not {1 << align}-byte aligned")
        if offset + size > len(blob):
            fail("slice extends past the fat file")
        slices.append(blob[offset:offset + size])
    return slices


def bundle_files(app):
    out = {}
    for dirpath, dirnames, filenames in os.walk(app, followlinks=False):
        for name in list(dirnames):
            if os.path.islink(os.path.join(dirpath, name)):
                dirnames.remove(name)
        for name in filenames:
            path = os.path.join(dirpath, name)
            rel = os.path.relpath(path, app)
            if rel == os.path.join("Contents", "MacOS", "Cove"):
                continue
            if os.path.islink(path):
                out[rel] = ("link", os.readlink(path))
            elif os.path.isfile(path):
                digest = hashlib.sha256(open(path, "rb").read()).hexdigest()
                out[rel] = ("file", digest, os.stat(path).st_mode & 0o777)
    return out


def versions(app):
    plist = plistlib.loads(open(os.path.join(app, "Contents", "Info.plist"), "rb").read())
    return plist.get("LSMinimumSystemVersion"), plist.get("CFBundleShortVersionString"), plist.get("CFBundleVersion")


if platform == "darwin-arm64":
    if fat_slices(data) is not None:
        fail("darwin-arm64 executable is a fat Mach-O")
    arch, _cpusub, minos, signed = parse_thin(data, "executable")
    if arch != "arm64":
        fail(f"darwin-arm64 executable is {arch}")
    if not signed:
        fail("darwin-arm64 executable has no ad-hoc signature")
    print("lipo -info: Non-fat file: Cove is architecture: arm64")
    print(f"Mach-O minos arm64={minos}; ad-hoc signature arm64=ok")
elif platform == "darwin-universal":
    slices = fat_slices(data)
    if not slices:
        fail("universal executable is not a fat Mach-O")
    found = {}
    order = []
    for blob in slices:
        arch, cpusub, minos, signed = parse_thin(blob, "slice")
        if arch in found:
            fail(f"duplicate {arch} slice")
        found[arch] = (minos, signed, cpusub)
        order.append(arch)
    if set(found) != {"arm64", "x86_64"}:
        fail(f"universal slices are {sorted(found)}")
    if not found["arm64"][1]:
        fail("arm64 slice has no ad-hoc signature")
    if found["arm64"][0] != found["x86_64"][0]:
        fail(
            "slice deployment targets differ: "
            f"arm64={found['arm64'][0]} x86_64={found['x86_64'][0]}"
        )
    print("lipo -info: Architectures in the fat file: Cove are: " + " ".join(order))
    print(
        "Mach-O minos "
        + ", ".join(f"{arch}={found[arch][0]}" for arch in ("arm64", "x86_64"))
        + "; ad-hoc signature "
        + ", ".join(
            f"{arch}={'ok' if found[arch][1] else 'absent'}"
            for arch in ("arm64", "x86_64")
        )
    )
    uni_app = os.path.join(root, "build", "darwin-universal", "Cove.app")
    arm_app = os.path.join(root, "build", "darwin-arm64", "Cove.app")
    if os.path.isdir(arm_app):
        arm_files = bundle_files(arm_app)
        uni_files = bundle_files(uni_app)
        if arm_files != uni_files:
            only_arm = sorted(set(arm_files) - set(uni_files))
            only_uni = sorted(set(uni_files) - set(arm_files))
            changed = sorted(k for k in set(arm_files) & set(uni_files) if arm_files[k] != uni_files[k])
            fail(
                "universal bundle differs from the arm64 bundle outside the executable: "
                f"only arm64 {only_arm} only universal {only_uni} changed {changed}"
            )
        arm_ver = versions(arm_app)
        uni_ver = versions(uni_app)
        if arm_ver != uni_ver:
            fail(f"Info.plist mismatch arm64={arm_ver} universal={uni_ver}")
        print(
            "bundle matches darwin-arm64 except the executable; "
            f"LSMinimumSystemVersion {uni_ver[0]}; "
            f"CFBundleShortVersionString {uni_ver[1]}; CFBundleVersion {uni_ver[2]}"
        )
else:
    fail(f"unknown platform {platform}")
PY

(
  cd "$archive_dir"
  sha256sum "$archive_name" > "${archive_name}.sha256"
  sha256sum -c "${archive_name}.sha256"
)

echo "wrote $archive_name and ${archive_name}.sha256"
