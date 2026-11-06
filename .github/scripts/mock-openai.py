#!/usr/bin/env python3
"""OpenAI-compatible Chat Completions mock with slow SSE chunks.

Used by the Linux Xvfb end-to-end check. Writes status files under
COVE_MOCK_STATE (or --state):

  port          listening port
  got-request   first POST arrived
  request.json  raw JSON body
  mid-stream    prose chunks flushed; table still in flight
  done          data: [DONE] flushed
  mock.log      human-readable log
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

PROSE = "下面比较两种方案。方案 A 落地更快，Plan B 更利于长期维护。\n\n"
TABLE = "\n".join(
    [
        "┌────────┬──────────────────┬──────────┐",
        "│ 方案   │ 优点             │ Tradeoff │",
        "├────────┼──────────────────┼──────────┤",
        "│ 方案 A │ 实现快、改动少   │ 债较多   │",
        "│ Plan B │ 结构清晰、易测   │ 成本高   │",
        "└────────┴──────────────────┴──────────┘",
        "",
    ]
)
REPLY = PROSE + TABLE

# ~2–4 runes per event so 110ms pacing is visibly in progress.
CHUNK = 3
DELAY = 0.11  # 80–150ms


def chunks(text: str, n: int) -> list[str]:
    runes = list(text)
    return ["".join(runes[i : i + n]) for i in range(0, len(runes), n)]


class State:
    def __init__(self, root: Path) -> None:
        self.root = root
        self.root.mkdir(parents=True, exist_ok=True)
        self.log_path = self.root / "mock.log"
        self.log_path.write_text("", encoding="utf-8")

    def write(self, name: str, data: str = "") -> None:
        (self.root / name).write_text(data, encoding="utf-8")

    def log(self, line: str) -> None:
        with self.log_path.open("a", encoding="utf-8") as f:
            f.write(line.rstrip() + "\n")
        print(line, flush=True)


def handler(state: State, delay: float):
    class H(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt: str, *args) -> None:
            state.log("http: " + (fmt % args))

        def do_POST(self) -> None:
            length = int(self.headers.get("Content-Length") or "0")
            body = self.rfile.read(length)
            state.write("request.json", body.decode("utf-8", "replace"))
            state.write("got-request")
            state.log(f"got POST {self.path} ({length} bytes)")
            try:
                payload = json.loads(body)
            except json.JSONDecodeError:
                payload = {}
            msgs = payload.get("messages") or []
            last_user = ""
            for m in msgs:
                if m.get("role") == "user":
                    last_user = str(m.get("content") or "")
            state.log(f"prompt: {last_user}")

            stream = bool(payload.get("stream", True))
            if not stream:
                self._json_reply()
                return
            self._sse_reply()

        def do_GET(self) -> None:
            if self.path in ("/health", "/"):
                raw = b"ok\n"
                self.send_response(200)
                self.send_header("Content-Type", "text/plain")
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)
                return
            self.send_error(404)

        def _json_reply(self) -> None:
            blob = json.dumps(
                {"choices": [{"message": {"role": "assistant", "content": REPLY}}]},
                ensure_ascii=False,
            ).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(blob)))
            self.end_headers()
            self.wfile.write(blob)
            state.write("mid-stream")
            state.write("done")
            state.log("[DONE] sent (json)")

        def _sse_reply(self) -> None:
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "keep-alive")
            self.end_headers()
            parts = chunks(REPLY, CHUNK)
            mid_at = max(1, len(chunks(PROSE, CHUNK)))
            for i, part in enumerate(parts, start=1):
                event = {
                    "choices": [{"delta": {"content": part}, "index": 0}]
                }
                line = "data: " + json.dumps(event, ensure_ascii=False) + "\n\n"
                self.wfile.write(line.encode("utf-8"))
                self.wfile.flush()
                if i == mid_at:
                    state.write("mid-stream")
                    state.log("mid-stream")
                time.sleep(delay)
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
            state.write("done")
            state.log("[DONE] sent")

    return H


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--state", default=os.environ.get("COVE_MOCK_STATE", "."))
    p.add_argument("--host", default="127.0.0.1")
    p.add_argument("--port", type=int, default=int(os.environ.get("COVE_MOCK_PORT", "0")))
    p.add_argument("--delay", type=float, default=float(os.environ.get("COVE_MOCK_DELAY", str(DELAY))))
    args = p.parse_args()
    state = State(Path(args.state))
    httpd = ThreadingHTTPServer((args.host, args.port), handler(state, args.delay))
    host, port = httpd.server_address
    state.write("port", str(port))
    state.log(f"listening on http://{host}:{port}")
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
