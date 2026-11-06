#!/usr/bin/env bash
# Linux Xvfb end-to-end: mock OpenAI SSE stream, Chinese prompt, two
# screenshots (mid-stream and after completion). Fails if the mock never
# sees the request, never sends [DONE], or Cove records an API error.
#
# Usage: e2e-linux.sh <Cove-<ver>-linux-amd64.tar.gz> [streaming.png] [complete.png]

set -euo pipefail

if [[ $# -lt 1 || $# -gt 3 ]]; then
  echo "usage: e2e-linux.sh <linux-amd64-tar.gz> [streaming.png] [complete.png]" >&2
  exit 2
fi

archive=$1
streaming=${2:-linux-e2e-streaming.png}
complete=${3:-linux-e2e-complete.png}
if [[ $archive != /* ]]; then
  archive=$(pwd)/$archive
fi
if [[ $streaming != /* ]]; then
  streaming=$(pwd)/$streaming
fi
if [[ $complete != /* ]]; then
  complete=$(pwd)/$complete
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
need python3
need xvfb-run
need xdotool
need xwininfo
need scrot

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
mock_dir=$work/mock
home=$work/home
mkdir -p "$mock_dir" "$home"
trap 'rm -rf "$work"' EXIT

tar -xzf "$archive" -C "$work"
exe=$work/cove
if [[ ! -x $exe ]]; then
  echo "archive has no executable cove" >&2
  ls -la "$work" >&2
  exit 1
fi

python3 "$here/mock-openai.py" --state "$mock_dir" --host 127.0.0.1 --port 0 --delay 0.11 \
  >"$mock_dir/stdout.log" 2>"$mock_dir/stderr.log" &
mock_pid=$!
cleanup_mock() {
  if kill -0 "$mock_pid" 2>/dev/null; then
    kill "$mock_pid" 2>/dev/null || true
    wait "$mock_pid" 2>/dev/null || true
  fi
}
trap 'cleanup_mock; rm -rf "$work"' EXIT

port=""
for _ in $(seq 1 50); do
  if [[ -f $mock_dir/port ]]; then
    port=$(tr -d '[:space:]' <"$mock_dir/port")
    break
  fi
  sleep 0.1
done
if [[ -z $port ]]; then
  echo "mock server did not publish a port" >&2
  cat "$mock_dir/stdout.log" "$mock_dir/stderr.log" "$mock_dir/mock.log" >&2 || true
  exit 1
fi
base="http://127.0.0.1:${port}/v1"
echo "mock listening at $base"

# PathUserData is $XDG_CONFIG_HOME/Cove. The 0600 fallback keyring is
# conversations.json.keys next to the store. Version 7 so adoptProviders
# does not prune the custom connection.
export HOME=$home
export XDG_CONFIG_HOME=$home/.config
export XDG_CACHE_HOME=$home/.cache
export XDG_RUNTIME_DIR=$work/runtime
export LANG=${LANG:-C.UTF-8}
export LC_ALL=${LC_ALL:-C.UTF-8}
mkdir -p "$XDG_CONFIG_HOME/Cove" "$XDG_CACHE_HOME" "$XDG_RUNTIME_DIR"
python3 - "$XDG_CONFIG_HOME/Cove" "$base" <<'PY'
import json, os, sys

root, base = sys.argv[1], sys.argv[2]
prompt = "请用表格比较两种方案"
conn = {
    "id": "mock",
    "name": "Mock",
    "provider": "Custom",
    "baseUrl": base,
    "custom": True,
    "modelsSet": True,
    "models": [{"id": "mock-model", "enabled": True}],
    "probe": "ok",
}
cfg = {
    "provider": "Custom",
    "model": "mock-model",
    "baseUrl": base,
    "connectionId": "mock",
    "systemPrompt": "You are a helpful assistant. Be clear and concise.",
}
now = "2026-11-06T05:00:00Z"
store = {
    "version": 7,
    "connections": [conn],
    "defaults": cfg,
    "conversations": [
        {
            "id": "e2e",
            "title": prompt,
            "draft": prompt,
            "config": cfg,
            "messages": [],
            "createdAt": now,
            "updatedAt": now,
        }
    ],
    "activeId": "e2e",
    "appearance": "light",
    "theme": "warm",
    "showReasoning": True,
    "restoreLast": True,
    "keepLastRequest": True,
    "autoTitles": False,
}
path = os.path.join(root, "conversations.json")
with open(path, "w", encoding="utf-8") as f:
    json.dump(store, f, ensure_ascii=False, indent=2)
    f.write("\n")
keys = os.path.join(root, "conversations.json.keys")
fd = os.open(keys, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
with os.fdopen(fd, "w", encoding="utf-8") as f:
    json.dump({"mock": "sk-e2e-dummy"}, f)
    f.write("\n")
mode = os.stat(keys).st_mode & 0o777
if mode != 0o600:
    sys.exit(f"keys file mode {oct(mode)}, want 0600")
print(f"seeded {path} -> {base}")
PY

cat >"$work/run.sh" <<'INNER'
#!/usr/bin/env bash
set -euo pipefail
work=$1
exe=$2
streaming=$3
complete=$4
mock_dir=$5
if command -v openbox >/dev/null 2>&1; then
  openbox >/tmp/cove-e2e-openbox.log 2>&1 &
  sleep 0.4
fi
# Force the 0600 file keyring: a session bus with libsecret would hide
# conversations.json.keys.
env -u DBUS_SESSION_BUS_ADDRESS -u DBUS_SESSION_BUS_PID \
  "$exe" >"$work/cove.log" 2>&1 &
pid=$!
cleanup() {
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
}
trap cleanup EXIT

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

wait_file() {
  local path=$1
  local seconds=$2
  local i
  for i in $(seq 1 "$seconds"); do
    if [[ -f $path ]]; then
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      echo "Cove exited while waiting for $(basename "$path")" >&2
      cat "$work/cove.log" >&2 || true
      cat "$mock_dir/mock.log" >&2 || true
      exit 1
    fi
    sleep 1
  done
  echo "timed out waiting for $path" >&2
  cat "$work/cove.log" >&2 || true
  cat "$mock_dir/mock.log" >&2 || true
  xwininfo -root -tree >&2 || true
  return 1
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
xdotool windowactivate --sync "$wid" || xdotool windowactivate "$wid" || true
sleep 1.5
# The composer sits on the content card: ~720px wide, ~14px above the
# bottom, send control on its trailing edge. Click the field, Return,
# then the Send button if the mock has not seen the request.
geom=$(xdotool getwindowgeometry --shell "$wid")
eval "$geom"
xdotool mousemove --window "$wid" $((WIDTH * 40 / 100)) $((HEIGHT - 70)) click 1 || true
sleep 0.4
xdotool key --clearmodifiers Return || true
sleep 0.8
if [[ ! -f $mock_dir/got-request ]]; then
  xdotool windowactivate "$wid" || true
  xdotool mousemove --window "$wid" $((WIDTH * 90 / 100)) $((HEIGHT - 38)) click 1 || true
fi

if ! wait_file "$mock_dir/got-request" 20; then
  scrot -z "$streaming" || true
  exit 1
fi
wait_file "$mock_dir/mid-stream" 20
sleep 0.4
scrot -z "$streaming"
echo "wrote $streaming"

wait_file "$mock_dir/done" 30
# Reveal catch-up is 280ms; give the box table a frame to lay out.
sleep 1.2
scrot -z "$complete"
echo "wrote $complete"

if ! grep -q '\[DONE\] sent' "$mock_dir/mock.log"; then
  echo "mock did not log [DONE] sent" >&2
  cat "$mock_dir/mock.log" >&2
  exit 1
fi
if ! grep -q '请用表格比较两种方案' "$mock_dir/request.json"; then
  echo "mock request did not include the Chinese prompt" >&2
  cat "$mock_dir/request.json" >&2
  exit 1
fi

chat=""
chat=$(find "$HOME/.config/Cove" -name 'e2e.jsonl' -o -name '*.jsonl' 2>/dev/null | head -n 1 || true)
if [[ -z $chat ]]; then
  echo "no chat jsonl under $HOME/.config/Cove" >&2
  find "$HOME/.config/Cove" -print >&2 || true
  exit 1
fi
if grep -q '"error":' "$chat" && grep '"error":' "$chat" | grep -vq '"error":""'; then
  echo "Cove recorded an API error in $chat" >&2
  cat "$chat" >&2
  exit 1
fi
if ! grep -q '方案 A' "$chat" || ! grep -q 'Plan B' "$chat" || ! grep -q '┌' "$chat"; then
  echo "completed chat is missing the mocked Chinese/box table" >&2
  cat "$chat" >&2
  exit 1
fi
if grep -Eiq 'Could not reach the API|Add an API key|The API returned HTTP' "$work/cove.log"; then
  echo "Cove log shows an API error" >&2
  cat "$work/cove.log" >&2
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
    echo "linux e2e ok; streaming=$streaming complete=$complete"
    exit 0
  fi
  sleep 1
done
echo "Cove did not exit after windowclose" >&2
cat "$work/cove.log" >&2 || true
exit 1
INNER
chmod +x "$work/run.sh"

xvfb-run -a -s '-screen 0 1400x900x24' "$work/run.sh" "$work" "$exe" "$streaming" "$complete" "$mock_dir"
echo "linux e2e artifacts: $streaming $complete"
