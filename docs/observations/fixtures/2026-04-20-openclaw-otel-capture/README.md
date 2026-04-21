# SP-0 openclaw OTEL capture — fixtures

Files:

- `receiver.py` — the stdlib Python HTTP receiver used during SP-0,
  reproducible by running `python3 receiver.py 4318`.
- `v1-metrics-sample.pb` — one empty-batch OTLP/protobuf metric
  export POST body, captured at `POST /v1/metrics`
  `Content-Type: application/x-protobuf`. 0 bytes — see the
  observation doc for why. Retained as a wire-format sample.
- `v1-logs-sample.pb` — same for `POST /v1/logs`.

The full observation doc is at
`../2026-04-20-openclaw-otel-capture.md`.
