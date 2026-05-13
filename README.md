# Chitin

**Execution governance runtime for heterogeneous AI coding agents.**

Every tool call across Claude Code, Codex CLI, Gemini CLI, Copilot CLI, and OpenClaw passes through one declarative policy and lands in a hash-linked audit chain that emits OTEL spans into your existing observability stack. Apache 2.0 licensed.

> Stable primitives (tool invocation, execution authority, policy enforcement, observability, heuristic signals) wrapped around an unstable substrate (drivers, models, benchmarks). Composes with whatever orchestrator you already run.

## What chitin IS

The kernel and the contract it enforces — exhaustively three things:

1. **The kernel** — `chitin-kernel` Go binary. Gate, severity counter, lockdown, envelope, audit, router signals. Single side-effect authority.
2. **Driver plugins** — `go/execution-kernel/internal/driver/{claudecode,codex,gemini,hermes,copilot}/normalize.go`. Adapters between vendor tool vocabularies and the kernel's canonical action enum.
3. **The data** — `~/.chitin/{gov-decisions-*.jsonl, events-*.jsonl, gov.db, chain_index.sqlite}`. Tamper-evident chain + the read-side analysis surface (`python/analysis/`).

Plus the scaffolding to keep those healthy: systemd timers for kernel redeploy, agent-unlock, chain-watch, envelope-rotate. Internal, not orchestration.

## What chitin is NOT

- Not an agent framework. The agent runs in Claude Code / Codex / Gemini / Copilot / OpenClaw / Hermes. Chitin gates each one's tool calls; it doesn't host a session.
- Not an orchestrator. Work tracking, dispatch, scheduling, workflows, kanban — that's hermes (or whatever orchestrator you run). Chitin is agnostic to it.
- Not a model router. The driver picks the model; chitin observes the decision via fingerprint dimensions in the chain.
- Not an approval system. Hermes' `tools/approval.py` provides operator-prompt + reply-parse + persistent allowlist natively. Chitin's gate denies; hermes prompts. See `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`.
- Not a SaaS. Local-only. Operator's box, operator's data.

## The moat

Asymmetric strengths nothing else in the ecosystem provides:

1. **Cross-driver canonical action vocabulary.** Hermes' `pre_tool_call` fires only inside Hermes; OpenClaw's `before_tool_call` only inside its pi-runtime. Chitin alone gates Claude Code, Codex, Gemini, Copilot, OpenClaw, and Hermes against one shared enum (`internal/gov/action.go`).
2. **Typed-action policy** with `path_under` and `bounds`. `chitin.yaml` evaluates a structured action against typed predicates, not regex-on-shell-string.
3. **Tamper-evident chain across all drivers + sessions.** SHA-256-linked JSONL with SQLite materialized index. Replay-able. Cross-driver, cross-session, cross-day.
4. **Per-agent severity ladder + lockdown counter.** `agent_state` in `gov.db` tracks behavior across all tasks and drivers. Hermes has per-task retry budgets; chitin's counter spans the agent's lifetime.
5. **Bounds enforcement on push-shaped actions** (lines/files changed). No equivalent in any substrate.
6. **Heuristic signals stamped onto the chain** (`internal/router/`). Blast-radius, floundering, and drift are pure-Go signals emitted as advisory decision rows. LLM consultation lives downstream in Hermes `approvals.mode: smart` or operator-wired chain consumers, not in the kernel hot path.

## How chitin composes with what you already run

```
        Claude Code   Codex CLI   Gemini CLI   Copilot CLI   OpenClaw   Hermes
              │           │            │            │            │         │
              └─────┬─────┴─────┬──────┴──────┬─────┴─────┬──────┴────┬────┘
                    │ tool calls (PreToolUse / SDK / hook / plugin)
                    ▼
            ┌──────────────────────────────────────────┐
            │ chitin-kernel gate                       │  ◄── chitin.yaml (declarative policy)
            │   normalize → policy → bounds → counter  │
            │   → envelope → audit → OTEL              │
            └──────────────────────────────────────────┘
                    │
                    ▼
            ~/.chitin/{events-*.jsonl, gov-decisions-*.jsonl,
                       gov.db, chain_index.sqlite}
```

Hermes runs the orchestration substrate (kanban, cron, approvals). OpenClaw runs the personal-AI gateway. Chitin gates every tool call from both, plus the standalone CLI drivers, against one policy.

## Quick start

```bash
# Build the kernel
go build -o ~/.local/bin/chitin-kernel ./go/execution-kernel/cmd/chitin-kernel/

# Install hooks for your driver(s)
chitin-kernel install-hook            # default: claude-code
chitin-kernel install-hook --agent codex
chitin-kernel install-hook --agent gemini

# Inspect activity
chitin-kernel chain-info --chain-id <id>
chitin-kernel envelope tail
chitin-kernel health
```

## Repo layout

```
.
├── go/execution-kernel/         # Go kernel — only layer with side effects
│   ├── cmd/chitin-kernel/       #   binary + subcommands
│   └── internal/
│       ├── gov/                 #   gate, policy, bounds, severity, chain
│       ├── driver/              #   per-driver normalize.go (claudecode, codex,
│       │                        #   gemini, hermes, copilot)
│       ├── router/              #   pure-Go heuristic signals + plugin checks
│       ├── chain/               #   audit chain + SQLite index
│       └── canon/               #   canonical-JSON SHA-256
├── libs/
│   ├── contracts/               # canonical wire schemas (TS)
│   ├── telemetry/               # read/query side over the event chain
│   ├── router-plugin-api/       # typed API for external router plugins
│   └── adapters/                # operator-installed driver-side adapters
├── apps/cli/                    # operator CLI (`chitin` — events, replay, health, ledger)
├── python/analysis/             # gate-derived analyzers (decisions, debt, predict, detect)
├── infra/systemd/               # user-mode timers (redeploy, agent-unlock, chain-watch,
│                                # envelope-rotate, codex-chain-ingest, codex-usage-feed)
├── docs/decisions/              # durable boundary docs (positioning, scope, culls)
├── chitin.yaml                  # policy
└── bin/chitin-router-hook       # PreToolUse shim
```

## Where chitin writes data

The kernel resolves the chitin dir via `CHITIN_HOME` env → fallback `$HOME/.chitin/`. Every chitin process writes only there.

```
$HOME/.chitin/
├── events-<run_id>.jsonl              # canonical event chain (one file per run)
├── gov-decisions-YYYY-MM-DD.jsonl     # daily gate decisions
├── gov.db                             # SQLite: envelope state + agent_state (severity counter)
├── chain_index.sqlite                 # materialized view of events for fast lookup
├── current-envelope                   # active cost envelope
└── kernel-errors.log                  # kernel-side errors (read by `chitin health`)
```

## Documentation

- [`docs/decisions/2026-05-06-execution-governance-runtime-positioning.md`](./docs/decisions/2026-05-06-execution-governance-runtime-positioning.md) — the moat
- [`docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`](./docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md) — the boundary
- [`docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`](./docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md) — why operator-approval lives in hermes, not chitin
- [`docs/architecture.md`](./docs/architecture.md) — kernel internals
- [`docs/roadmap.md`](./docs/roadmap.md) — strategic arc + what's in flight
- [`docs/driver-conformance.md`](./docs/driver-conformance.md) — current driver hook matrix and normalizer gaps
- [`docs/governance-setup-extras/README.md`](./docs/governance-setup-extras/README.md) — mirrored workflow: `kanban-dispatch.lobster` (see sync policy)

## License

Apache 2.0.
