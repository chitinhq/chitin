# Architecture — Phase 1

```
Claude Code
   │ PreToolUse hook (JSON on stdin)
   ▼
libs/adapters/claude-code (TS)
   │ exec() with stdin forwarded
   ▼
go/execution-kernel (Go binary)
   │ canon (canonicalize shell) → normalize (action type) → emit (JSONL append)
   ▼
.chitin/events-<run_id>.jsonl  ◄── ground truth, immutable
   │ tail
   ▼
libs/telemetry (TS)
   │ SQLite indexer
   ▼
.chitin/events.db  ◄── queryable materialized view
   │ read
   ▼
apps/cli (TS commander)
   events list | events tail | replay <run_id>
```

## Layers

- **`libs/contracts`** — zod event schema; source of truth for TS + generated Go.
- **`go/execution-kernel`** — canon, normalize, emit, hook. Only layer allowed to perform side effects.
- **`libs/telemetry`** — SQLite indexer, JSONL tailer, replay streamer.
- **`libs/adapters/*`** — per-surface hook receivers. One per execution surface. Thin — forwards to kernel.
- **`apps/cli`** — operator commands.

## Hard rule

> Only the Go execution kernel may perform side effects. TypeScript is read-only against the filesystem except via the kernel binary.
