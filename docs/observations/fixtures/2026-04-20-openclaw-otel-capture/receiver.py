#!/usr/bin/env python3
"""Tiny OTLP/HTTP receiver for SP-0. Writes each POST body to disk.

Usage:
    python3 receiver.py [port]  (default 4318)

Captures to /tmp/otel-capture/<signal>-<epoch_ms>.{json,pb}
"""
import sys
import json
import gzip
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

CAPTURE_DIR = "/tmp/otel-capture"


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length) if length else b""
        ce = (self.headers.get("Content-Encoding") or "").lower()
        if ce == "gzip":
            try:
                body = gzip.decompress(body)
            except Exception as e:
                print(f"[recv] gzip decode failed: {e}", flush=True)
        ct = (self.headers.get("Content-Type") or "").lower()
        signal = self.path.strip("/").replace("/", "-") or "unknown"
        ts_ms = int(time.time() * 1000)
        if "json" in ct:
            ext = "json"
        elif "protobuf" in ct or "x-protobuf" in ct:
            ext = "pb"
        else:
            ext = "bin"
        fname = f"{CAPTURE_DIR}/{signal}-{ts_ms}.{ext}"
        with open(fname, "wb") as f:
            f.write(body)
        print(
            f"[recv] {self.command} {self.path} ct={ct} ce={ce} bytes={len(body)} -> {fname}",
            flush=True,
        )
        if ext == "json":
            try:
                parsed = json.loads(body)
                top_keys = list(parsed.keys()) if isinstance(parsed, dict) else type(parsed).__name__
                print(f"[recv]   top-level: {top_keys}", flush=True)
            except Exception as e:
                print(f"[recv]   json parse failed: {e}", flush=True)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"partialSuccess":{}}')

    def log_message(self, fmt, *args):
        pass  # silence default stderr logging; we print our own lines


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 4318
    print(f"[recv] listening on 127.0.0.1:{port}, capturing to {CAPTURE_DIR}", flush=True)
    HTTPServer(("127.0.0.1", port), Handler).serve_forever()


if __name__ == "__main__":
    main()
