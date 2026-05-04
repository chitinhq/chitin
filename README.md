# Chitin

**Execution kernel for AI coding agents.** Every tool call across Claude Code, Copilot CLI, and openclaw is gated by a single policy and recorded in a hash-linked event chain that also emits OTEL spans into your existing observability stack. Nx monorepo (Go + TypeScript + Python). MIT licensed.

> Principle: real execution before policy. Policy before automation. Automation gated by the same kernel.

## What chitin is today

The kernel and the contract it enforces:

- **Kernel + governance** — Go binary fires on every PreToolUse hook, evaluates `chitin.yaml`, writes a hash-linked JSONL chain. Gate decisions are auditable; chain integrity is SHA-256-verified.
- **OTEL emit (F4, 2026-05-02)** — kernel projects every chain event onto an OTLP/HTTP JSON span after the canonical write succeeds. One-way bridge: chain authoritative, OTEL non-authoritative. Opt-in via `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`.

Chitin is *not* a tick-loop, not an agent runner, not an OTEL ingest target, not a cloud product — see [`docs/thesis.md`](./docs/thesis.md) for the full set of refusals.

## Reference consumers (hosted in the monorepo for dogfooding)

These are downstream consumers of the kernel — they run *on* chitin, they aren't chitin. Hosted in the same repo so they share `libs/contracts/` schemas and the Nx project graph; gated from the outside via the openclaw plugin like any other agent surface.

- **Autonomous swarm runtime** (`apps/temporal-worker/`) — Temporal-backed dispatcher + worker that picks ready backlog entries, dispatches role-typed agents (programmer, reviewer, researcher, analyst, …), runs the §5 review-tier escalation chain (R1→R2→R3→operator), and (with `CHITIN_GATEKEEPER_AUTO_MERGE=1`) auto-merges PRs that pass the §6 gate matrix. Design: [`docs/design/2026-05-02-swarm-as-software-factory.md`](./docs/design/2026-05-02-swarm-as-software-factory.md).
- **Self-feeding telemetry loop** (cron scripts under `apps/temporal-worker/`) — periodic researcher / lessons / debt-curator / groomer / alarm-feeder / stale-doc detector keep the backlog hydrated from external signals + internal alarms; the analyst role runs deterministic Python recipes against the chain to investigate regressions.

## Quick start

```bash
pnpm install
pnpm exec nx run execution-kernel:build
pnpm exec nx run cli:build
./dist/apps/cli/main.js init claude-code
# Run a Claude Code session...
./dist/apps/cli/main.js events list
./dist/apps/cli/main.js replay <run_id>
./dist/apps/cli/main.js health
```

To run the autonomous swarm on your own rig, see [`infra/systemd/README.md`](./infra/systemd/README.md).

## Architecture

```
.
├── apps/
│   ├── cli/                          # operator CLI (`chitin`)
│   ├── temporal-worker/              # autonomous swarm runtime + cron-fired scripts
│   └── openclaw-plugin-governance/   # openclaw integration: chitin gates every openclaw tool call
├── libs/
│   ├── contracts/                    # canonical schemas (event chain, ExecutionRequest, envelope)
│   └── telemetry/                    # JSONL tailer + SQLite indexer + replay streamer
├── go/execution-kernel/              # the Go kernel — only layer allowed side effects
├── python/analysis/                  # decisions / debt / souls streams + daily rollup + analyst recipes
└── infra/systemd/                    # user-mode systemd units for the swarm
```

| Package | What it owns |
|---------|--------------|
| `apps/cli` | Operator CLI (`init`, `events list/tail/tree`, `replay`, `health`, `ledger`, `review`, `install`). [README](./apps/cli/README.md) |
| `apps/temporal-worker` | Dispatcher, review-graph workflow, gatekeeper, role-typed prompts, all cron scripts. [README](./apps/temporal-worker/README.md) |
| `apps/openclaw-plugin-governance` | openclaw plugin that wires chitin's policy gate into openclaw's tool-call lifecycle. [README](./apps/openclaw-plugin-governance/README.md) |
| `libs/contracts` | Canonical schemas every chitin component agrees on. [README](./libs/contracts/README.md) |
| `libs/telemetry` | Read-side of the event chain. [README](./libs/telemetry/README.md) |
| `go/execution-kernel` | The Go kernel binary — `canon`, `normalize`, `emit`, `gov`, `hook`, `health`. The only layer allowed side effects. |
| `python/analysis` | Decisions / debt / souls / swarm-runs / swarm-health / daily rollup + the analyst-role investigation recipe. |
| `infra/systemd` | User-mode systemd units (worker + 7 cron timers). [README](./infra/systemd/README.md) |

## Where chitin writes data

Chitin emits append-only JSONL and a SQLite materialized view to a `.chitin/` directory. The kernel resolves the path in this order: `--chitin-dir` flag → repo-local `.chitin/` (walking up from cwd) → fallback `$HOME/.chitin/`. **The Go kernel is the only writer.** TS adapters are read-only against the filesystem (see [architecture.md](./docs/architecture.md#hard-rule)).

```
$HOME/.chitin/                       # global state — used when no repo-local .chitin/ exists
├── flow_events.jsonl                # governance flow events (gov decisions, swarm ticks)
├── gov-decisions-YYYY-MM-DD.jsonl   # daily gov decision logs (one file per day)
├── gov.db                           # SQLite gov state
├── hook-capture/                    # raw Pre/PostToolUse JSON captures (one file per hook fire)
├── kernel-errors.log                # kernel-side errors (read by `chitin health`)
└── current-envelope                 # active cost envelope

<repo>/.chitin/                      # repo-local capture (created by `chitin-kernel init`)
├── events-<run_id>.jsonl            # canonical event stream — one file per run
└── session_state.json
```

For diagnostics, run `chitin health` — it reports on the resolved dir and exits non-zero on `[FAIL]` rows. The [health runbook](./docs/observations/runbooks/health.md) explains every row and what to do when it's red.

## Toolchain

- **Nx** — orchestrator (project graph, affected, module boundaries)
- **Vite+** (`vp`) — TypeScript test/lint/format/build (Vitest, Oxlint, Oxfmt, Rolldown, tsgo)
- **Go 1.22+** — execution kernel, run via `nx:run-commands`
- **uv + pytest** — Python analysis lib (`python/analysis/`)
- **Temporal** — workflow durability for the autonomous swarm

## Docs

Core:
- [`docs/thesis.md`](./docs/thesis.md) — what chitin is + isn't
- [`docs/operating-model.md`](./docs/operating-model.md) — how chitin runs against an agent surface
- [`docs/architecture.md`](./docs/architecture.md) — three-plane model (Temporal control / OpenClaw execution / Chitin enforcement)
- [`docs/event-model.md`](./docs/event-model.md) — canonical event chain + OTEL projection
- [`docs/toolchain.md`](./docs/toolchain.md)
- [`docs/roadmap.md`](./docs/roadmap.md)

Autonomous swarm (factory model):
- [`docs/design/2026-05-02-swarm-as-software-factory.md`](./docs/design/2026-05-02-swarm-as-software-factory.md) — the full §3-§9 station-taxonomy + review-tier escalation + auto-merge gates
- [`docs/swarm-backlog.md`](./docs/swarm-backlog.md) — what the swarm picks up next
- [`docs/swarm-lessons.md`](./docs/swarm-lessons.md) — auto-distilled lessons prepended to programmer prompts
- [`docs/debt-ledger.md`](./docs/debt-ledger.md) — operator-curated + auto-curated debt
- [`infra/systemd/README.md`](./infra/systemd/README.md) — install + operate the worker + 7 cron timers

Archive: [`docs/archive-map.md`](./docs/archive-map.md)

## License

[MIT](./LICENSE)
