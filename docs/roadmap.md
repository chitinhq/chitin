# Roadmap

## Phase 0 — Archive old org *(complete)*

Renamed `chitinhq/chitin` → `chitinhq/chitin-archive` at `v1.0.0`; archived every other repo in the org; created new public MIT `chitinhq/chitin` monorepo.

## Phase 1 — Claude Code capture→replay

- Nx + Vite+ scaffold
- Go execution kernel (canon, normalize, emit, hook)
- libs/contracts (zod schema, generated Go types)
- libs/telemetry (JSONL tailer, SQLite indexer, replay)
- libs/adapters/claude-code (monitor-only PreToolUse hook)
- apps/cli (init, events list/tail, replay)

**Done when:** a Claude Code session on the 3090 box is captured, normalized, persisted, and replayable — fully offline.

## Phase 1.5 — OpenClaw + local Ollama

Extend the canonical contract to a second and third surface. Validate that the event model is genuinely surface-neutral by comparing traces across Claude Code, OpenClaw, and Ollama on the same task.

## Phase 2 — Governance

Policy engine, invariants, drift detection, research-phase manifests. Enforcement becomes possible once observability is trustworthy across surfaces.

## Phase 3 — Automation + shared dashboard

GitHub Actions / Copilot Enterprise as unattended surfaces. Postgres/Neon for cross-machine aggregation. Web UI.
