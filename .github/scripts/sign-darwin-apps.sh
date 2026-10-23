#!/usr/bin/env bash
# Ad-hoc sign the arm64 and universal Cove.app bundles on macOS, prove the
# signatures, launch both slices, and write the zips that ship.
#
# Usage:
#   sign-darwin-apps.sh <arm64-zip> <universal-zip> <arm64-out-zip> <universal-out-zip>
#
# The input zips are the unsigned (linker-signed arm64 slice only) archives
# from package-darwin-app.sh. The outputs are signed with
# `codesign --force --deep -s -`. Each output has a sibling .sha256 file in
# the same format as sha256sum.

set -euo pipefail

if [[ $# -ne 4 ]]; then
  echo "usage: sign-darwin-apps.sh <arm64-zip> <universal-zip> <arm64-out-zip> <universal-out-zip>" >&2
  exit 2
fi

if [[ $(uname -s) != Darwin ]]; then
  echo "sign-darwin-apps.sh needs macOS (codesign, lipo, and Rosetta)" >&2
  exit 1
fi

arm_zip=$1
uni_zip=$2
arm_out=$3
uni_out=$4

for cmd in codesign lipo file unzip zip python3 plutil xattr arch; do
  command -v "$cmd" >/dev/null 2>&1 || {
    echo "missing required command: $cmd" >&2
    exit 1
  }
done

repo=$(cd "$(dirname "$0")/../.." && pwd)
want_version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["version"])' "$repo/mygo.json")
want_min=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["macos"]["minimumSystemVersion"])' "$repo/mygo.json")

abspath() {
  python3 -c 'import os,sys; print(os.path.abspath(sys.argv[1]))' "$1"
}

arm_zip=$(abspath "$arm_zip")
uni_zip=$(abspath "$uni_zip")
arm_out=$(abspath "$arm_out")
uni_out=$(abspath "$uni_out")

# Still running after this many seconds counts as a successful launch.
hold_seconds=5

echo "===== host ====="
sw_vers
uname -m

unpack() {
  local zip=$1 dest=$2
  mkdir -p "$dest"
  unzip -q "$zip" -d "$dest"
  if [[ ! -d $dest/Cove.app ]]; then
    echo "unzipped $zip has no Cove.app" >&2
    exit 1
  fi
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
unpack "$arm_zip" "$work/arm64"
unpack "$uni_zip" "$work/universal"
arm_app=$work/arm64/Cove.app
uni_app=$work/universal/Cove.app

# A quarantined tree makes codesign and the kernel refuse the binary.
xattr -cr "$arm_app" || true
xattr -cr "$uni_app" || true

sign_app() {
  local label=$1 app=$2
  local exe=$app/Contents/MacOS/Cove
  # Sign the fat or thin executable first so every slice has an ad-hoc
  # signature, then seal the bundle. --deep repeats that inside-out order.
  echo "===== codesign --force --sign - ($label executable) ====="
  codesign --force --sign - "$exe"
  echo "===== codesign --force --deep --sign - ($label bundle) ====="
  codesign --force --deep --sign - "$app"
  echo "===== codesign --verify --deep --strict --verbose=2 ($label) ====="
  codesign --verify --deep --strict --verbose=2 "$app"
}

sign_app arm64 "$arm_app"
sign_app universal "$uni_app"

show_arch() {
  local app=$1 arch=$2
  echo "===== codesign -dv --arch $arch --verbose=2 ====="
  local out
  out=$(codesign -dv --arch "$arch" --verbose=2 "$app" 2>&1)
  printf '%s\n' "$out"
  if ! printf '%s\n' "$out" | grep -E -q 'Signature=adhoc|flags=0x2\(adhoc\)'; then
    echo "codesign -dv --arch $arch did not report an ad-hoc signature" >&2
    exit 1
  fi
}

show_arch "$arm_app" arm64
show_arch "$uni_app" arm64
show_arch "$uni_app" x86_64

echo "===== lipo -info (universal) ====="
uni_exe=$uni_app/Contents/MacOS/Cove
arm_exe=$arm_app/Contents/MacOS/Cove
lipo_info=$(lipo -info "$uni_exe")
printf '%s\n' "$lipo_info"
file "$uni_exe"
file "$arm_exe"
case $lipo_info in
  *"are:"*arm64*x86_64*|*"are:"*x86_64*arm64*) ;;
  *)
    echo "lipo -info did not report both arm64 and x86_64" >&2
    exit 1
    ;;
esac
arm_lipo=$(lipo -info "$arm_exe")
echo "===== lipo -info (arm64) ====="
printf '%s\n' "$arm_lipo"
case $arm_lipo in
  *"architecture: arm64"*) ;;
  *)
    echo "arm64 app is not a thin arm64 binary: $arm_lipo" >&2
    exit 1
    ;;
esac

check_plist() {
  local app=$1
  local short bundle minimum
  short=$(plutil -extract CFBundleShortVersionString raw "$app/Contents/Info.plist")
  bundle=$(plutil -extract CFBundleVersion raw "$app/Contents/Info.plist")
  minimum=$(plutil -extract LSMinimumSystemVersion raw "$app/Contents/Info.plist")
  echo "CFBundleShortVersionString=$short CFBundleVersion=$bundle LSMinimumSystemVersion=$minimum"
  if [[ $short != "$want_version" || $bundle != "$want_version" ]]; then
    echo "Info.plist version does not match mygo.json ($want_version)" >&2
    exit 1
  fi
  if [[ $minimum != "$want_min" ]]; then
    echo "LSMinimumSystemVersion $minimum does not match mygo.json ($want_min)" >&2
    exit 1
  fi
}

echo "===== Info.plist ====="
check_plist "$arm_app"
check_plist "$uni_app"

if ! arch -x86_64 /usr/bin/true >/dev/null 2>&1; then
  echo "===== install Rosetta ====="
  sudo softwareupdate --install-rosetta --agree-to-license
fi
arch -x86_64 /usr/bin/true

# $1 is the log label. The remaining arguments are the command.
hold_launch() {
  local label=$1
  shift
  local log pid slept code
  log=$(mktemp)
  echo "===== launch $label ====="
  echo "command: $*"
  python3 -c 'import os, sys; os.setpgrp(); os.execvp(sys.argv[1], sys.argv[1:])' "$@" >"$log" 2>&1 &
  pid=$!
  slept=0
  while [[ $slept -lt $hold_seconds ]]; do
    if ! kill -0 "$pid" 2>/dev/null; then
      break
    fi
    sleep 1
    slept=$((slept + 1))
  done
  if kill -0 "$pid" 2>/dev/null; then
    echo "still running after ${slept} seconds"
    kill -TERM -- "-$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
    sleep 1
    kill -KILL -- "-$pid" 2>/dev/null || kill -KILL "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    return 0
  fi
  set +e
  wait "$pid"
  code=$?
  set -e
  echo "exited after ${slept} seconds with status ${code}"
  cat "$log" || true
  return 1
}

hold_launch "arm64" "$arm_exe"
# The first process owns a unix socket lock. Kill anything left, then start
# the other slice.
killall -9 Cove >/dev/null 2>&1 || true
sleep 1
hold_launch "x86_64 under Rosetta" arch -x86_64 "$uni_exe"
killall -9 Cove >/dev/null 2>&1 || true

write_zip() {
  local app=$1 dest=$2
  local parent name
  parent=$(dirname "$app")
  name=$(basename "$app")
  mkdir -p "$(dirname "$dest")"
  rm -f "$dest" "${dest}.sha256"
  (
    cd "$parent"
    zip -ry "$dest" "$name"
  )
  python3 - "$dest" <<'PY'
import hashlib, os, stat, sys, zipfile
dest = sys.argv[1]
name = os.path.basename(dest)
exe = "Cove.app/Contents/MacOS/Cove"
with zipfile.ZipFile(dest) as z:
    info = z.getinfo(exe)
    mode = (info.external_attr >> 16) & 0xFFFF
    if mode & 0o111 == 0:
        sys.exit(f"{exe} is mode {mode:o} inside {name}, want an executable")
digest = hashlib.sha256(open(dest, "rb").read()).hexdigest()
with open(dest + ".sha256", "w", encoding="utf-8") as out:
    out.write(f"{digest}  {name}\n")
print(f"wrote {name} and {name}.sha256")
PY
  local again=$work/roundtrip
  rm -rf "$again"
  mkdir -p "$again"
  unzip -q "$dest" -d "$again"
  echo "===== codesign --verify after unzip ($name) ====="
  codesign --verify --deep --strict --verbose=2 "$again/Cove.app"
}

write_zip "$arm_app" "$arm_out"
write_zip "$uni_app" "$uni_out"

echo "signed zips:"
echo "  $arm_out"
echo "  $uni_out"
