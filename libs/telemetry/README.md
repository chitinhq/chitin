# `@chitin/telemetry`

The query-side of chitin's event chain. Reads the canonical JSONL
log + the SQLite chain index to answer questions about what happened
across runs.

If `@chitin/contracts` defines the chain's *shape*, this lib defines
how to *consume* it.

## Modules

| File | Purpose |
|------|---------|
| `src/sqlite-indexer.ts` | The SQLite-backed index that mirrors the JSONL chain. Built lazily; the kernel writes events to JSONL and updates the index in the same transaction (chain.go). This module is the read-side. |
| `src/ensure-indexed.ts` | Idempotent "make sure the index is current" — call before any query. Cheap when up-to-date. |
| `src/jsonl-tailer.ts` | Streaming reader for `events-*.jsonl`. Used by the CLI's `events tail` command. |
| `src/replay.ts` | Replay a captured run from the chain — produces the same execution-request that originally fired. Underpins `chitin replay <run-id>`. |
| `src/schema.sql` | Schema for the SQLite index. Bundled at build; the indexer applies it on first read. |
| `src/index.ts` | Public exports. |

## Chain semantics (read side)

- The JSONL log is **canonical**. Every consumer must tolerate
  reading raw JSONL even if the SQLite index is missing.
- The index is a **derived view** — building it is fast (re-scans
  JSONL); losing it is a recoverable corruption, not data loss.
- `replay` is **deterministic**: same chain → same emitted
  execution-request. The chain's hash linkage is the audit-grade
  guarantee.

## Test suite

```bash
pnpm exec vitest run libs/telemetry
```

Coverage focuses on round-trips between JSONL and the SQLite index
+ replay determinism.

## Related

- `go/execution-kernel/internal/chain/` — the Go-side write path
  (kernel emits events; this lib reads them).
- `python/analysis/loaders.py` — the Python-side reader for the
  same JSONL chain. Two languages, one canonical format.
- `apps/cli/src/commands/events-list.ts` / `events-tail.ts` — CLI
  consumers of this lib.
