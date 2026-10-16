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


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        global request_count
        request_count += 1

        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)

        project_key = self.headers.get("X-Seismo-Project-Key", "<missing>")

        if request_count <= FAIL_FIRST_N:
            print(f"[{request_count}] {self.path} project-key={project_key} -> 503 (forced failure)", flush=True)
            self.send_response(503)
            self.end_headers()
            return

        try:
            parsed = json.loads(body)
            title = parsed.get("title", "<no title>")
            description = parsed.get("description", "")
        except Exception:
            title, description = "<unparseable>", ""

        print(f"[{request_count}] {self.path} project-key={project_key} title={title!r} description={description!r}", flush=True)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')

    def log_message(self, format, *args):
        pass  # quiet; we print our own lines above


if __name__ == "__main__":
    port = 8787
    server = HTTPServer(("127.0.0.1", port), Handler)
    print(f"fake m-Seismo server listening on http://127.0.0.1:{port}", flush=True)
    server.serve_forever()
