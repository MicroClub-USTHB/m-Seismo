#!/usr/bin/env python3
"""Throwaway local server for manually verifying the SDK's network layer
and offline queue against something real, instead of only mocks.
Not part of the SDK or its CI -- a dev-only tool.
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

FAIL_FIRST_N = int(sys.argv[1]) if len(sys.argv) > 1 else 0
request_count = 0


def parse_multipart(content_type, body):
    """Returns (report json or None, screenshot bytes or None)."""
    if "boundary=" not in content_type:
        return None, None
    boundary = content_type.split("boundary=", 1)[1].encode()
    report = screenshot = None
    for part in body.split(b"--" + boundary):
        head, sep, data = part.partition(b"\r\n\r\n")
        if not sep:
            continue
        data = data[:-2] if data.endswith(b"\r\n") else data
        if b'name="report"' in head:
            try:
                report = json.loads(data)
            except ValueError:
                pass
        elif b'name="screenshot"' in head:
            screenshot = data
    return report, screenshot


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        global request_count
        request_count += 1

        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)

        auth = self.headers.get("Authorization", "<missing>")
        report, screenshot = parse_multipart(self.headers.get("Content-Type", ""), body)

        if request_count <= FAIL_FIRST_N:
            print(f"[{request_count}] {self.path} auth={auth} -> 503 (forced failure)", flush=True)
            self.send_response(503)
            self.end_headers()
            return

        user_input = (report or {}).get("user_input", {})
        print(
            f"[{request_count}] {self.path} auth={auth} id={(report or {}).get('id')} "
            f"trigger={(report or {}).get('trigger')} title={user_input.get('title')!r} "
            f"screenshot={'yes' if screenshot else 'no'}",
            flush=True,
        )
        self.send_response(202)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({"id": (report or {}).get("id"), "status": "accepted"}).encode())

    def log_message(self, format, *args):
        pass  # quiet; we print our own lines above


if __name__ == "__main__":
    port = 8787
    server = HTTPServer(("127.0.0.1", port), Handler)
    print(f"fake m-Seismo server listening on http://127.0.0.1:{port}", flush=True)
    server.serve_forever()
