## Governance Bench

`bench/` is the repo-local harness for governance regression tasks that should
stay stable across policy and analyzer refactors.

Current scope:

- Runs `python -m chitin_telemetry.telemetry` end-to-end against fixed
  `gov-decisions-*.jsonl` fixtures.
- Verifies candidate-rule telemetry, empty-window behavior, top-N truncation,
  and error handling.
- Emits summary artifacts to `bench/out/`.

Run it from the repo root:

```bash
python3 bench/run.py
```

The GitHub workflow `.github/workflows/governance-bench.yml` runs this harness
on pull requests that touch `go/execution-kernel/internal/gov/**`.
