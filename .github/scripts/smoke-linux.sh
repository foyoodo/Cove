#!/usr/bin/env bash
# Launch the Linux preview binary under Xvfb, wait for the main window,
# take a screenshot, and close the app. A missing window, a hang, or a
# non-zero exit fails the job.
#
# Usage: smoke-linux.sh <Cove-<ver>-linux-amd64.tar.gz> [screenshot.png]

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: smoke-linux.sh <linux-amd64-tar.gz> [screenshot.png]" >&2
  exit 2
fi

archive=$1
screenshot=${2:-linux-smoke.png}
if [[ $archive != /* ]]; then
  archive=$(pwd)/$archive
fi
if [[ $screenshot != /* ]]; then
  screenshot=$(pwd)/$screenshot
fi
if [[ ! -f $archive ]]; then
  echo "missing $archive" >&2
  exit 1
fi

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}
need tar
need xvfb-run
need xdotool
need xwininfo
need scrot

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
tar -xzf "$archive" -C "$work"
exe=$work/cove
if [[ ! -x $exe ]]; then
  echo "archive has no executable cove" >&2
  ls -la "$work" >&2
  exit 1
fi

cat >"$work/run.sh" <<'INNER'
#!/usr/bin/env bash
set -euo pipefail
work=$1
exe=$2
screenshot=$3
if command -v openbox >/dev/null 2>&1; then
  openbox >/tmp/cove-openbox.log 2>&1 &
  sleep 0.4
fi
"$exe" >"$work/cove.log" 2>&1 &
pid=$!
cleanup() {
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# Title can be a chat name. Prefer the process, then the WM class.
# Ignore helper surfaces smaller than the main window.
find_main_window() {
  local id geom w h area best="" best_area=0
  local ids
  ids=$(xdotool search --onlyvisible --pid "$pid" 2>/dev/null || true)
  if [[ -z $ids ]]; then
    ids=$(xdotool search --onlyvisible --class Cove 2>/dev/null || true)
  fi
  for id in $ids; do
    geom=$(xdotool getwindowgeometry "$id" 2>/dev/null || true)
    w=${geom##*Geometry: }
    w=${w%%x*}
    h=${geom##*x}
    h=${h%%[^0-9]*}
    if [[ $w != [0-9]* || $h != [0-9]* ]]; then
      continue
    fi
    area=$((w * h))
    if [[ $w -ge 400 && $h -ge 300 && $area -gt $best_area ]]; then
      best=$id
      best_area=$area
    fi
  done
  printf '%s' "$best"
}

wid=""
for _ in $(seq 1 45); do
  if ! kill -0 "$pid" 2>/dev/null; then
    echo "Cove exited before a window appeared" >&2
    cat "$work/cove.log" >&2 || true
    exit 1
  fi
  wid=$(find_main_window)
  if [[ -n $wid ]]; then
    break
  fi
  sleep 1
done
if [[ -z $wid ]]; then
  echo "no Cove window after 45s" >&2
  cat "$work/cove.log" >&2 || true
  xwininfo -root -tree >&2 || true
  exit 1
fi
xdotool windowactivate "$wid" || true
sleep 1
scrot -z "$screenshot"

root_info=$(xwininfo -root)
win_info=$(xwininfo -id "$wid")
parse_geom() {
  awk '
    /Absolute upper-left X:/ { x=$4 }
    /Absolute upper-left Y:/ { y=$4 }
    /^  Width:/ { w=$2 }
    /^  Height:/ { h=$2 }
    END { printf "%s %s %s %s", x, y, w, h }
  '
}
read -r sx sy sw sh <<<"$(printf '%s\n' "$root_info" | parse_geom)"
read -r wx wy ww wh <<<"$(printf '%s\n' "$win_info" | parse_geom)"
echo "linux window rect ${wx},${wy} ${ww}x${wh} vs Xvfb screen ${sx},${sy} ${sw}x${sh}"
xdotool getwindowgeometry "$wid" || true
if [[ -z $sx || -z $sy || -z $sw || -z $sh || -z $wx || -z $wy || -z $ww || -z $wh ]]; then
  echo "could not parse window/screen geometry" >&2
  printf '%s\n' "$root_info" "$win_info" >&2
  exit 1
fi
if (( wx < sx || wy < sy || wx + ww > sx + sw || wy + wh > sy + sh )); then
  echo "window rect is outside the Xvfb screen" >&2
  exit 1
fi

xdotool windowclose "$wid" || true
for _ in $(seq 1 20); do
  if ! kill -0 "$pid" 2>/dev/null; then
    wait "$pid"
    code=$?
    trap - EXIT
    if [[ $code -ne 0 ]]; then
      echo "Cove exited $code" >&2
      cat "$work/cove.log" >&2 || true
      exit "$code"
    fi
    echo "linux smoke ok; screenshot $screenshot"
    exit 0
  fi
  sleep 1
done
echo "Cove did not exit after windowclose" >&2
cat "$work/cove.log" >&2 || true
exit 1
INNER
chmod +x "$work/run.sh"

export COVE_SMOKE=1
# xvfb-run picks a free display. openbox maps the window so xdotool can find it.
xvfb-run -a -s '-screen 0 1400x900x24' "$work/run.sh" "$work" "$exe" "$screenshot"
