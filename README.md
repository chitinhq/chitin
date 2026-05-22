# Chitin

**Execution governance runtime for heterogeneous AI coding agents.**

Every tool call across Claude Code, Codex CLI, Gemini CLI, Hermes, Copilot CLI, and OpenClaw passes through one declarative policy and lands in a hash-linked audit chain that emits OTEL spans into your existing observability stack. Apache 2.0 licensed.

> Stable primitives (tool invocation, execution authority, policy enforcement, observability, heuristic signals) wrapped around an unstable substrate (drivers, models, benchmarks). Composes with whatever orchestrator you already run.

## What chitin IS

The kernel and the contract it enforces — exhaustively three things:

1. **The kernel** — `chitin-kernel` Go binary. Gate, severity counter, lockdown, envelope, audit, router signals. Single side-effect authority.
2. **Driver plugins** — `go/execution-kernel/internal/driver/{claudecode,codex,gemini,hermes,copilot}/normalize.go` + the OpenClaw `before_tool_call` plugin. Adapters between each vendor's tool vocabulary and the kernel's canonical action enum.
3. **The data** — `~/.chitin/{gov-decisions-*.jsonl, events-*.jsonl, gov.db, chain_index.sqlite}`. Tamper-evident chain + the read-side analysis surface (`python/analysis/`).

Plus the scaffolding to keep those healthy: systemd timers for kernel redeploy, agent-unlock, chain-watch, envelope-rotate, codex-chain-ingest, codex-usage-feed, and judge-backfill. Internal, not orchestration.

## What chitin is NOT

- Not an agent framework. The agent runs in Claude Code / Codex / Gemini / Copilot / OpenClaw / Hermes. Chitin gates each one's tool calls; it doesn't host a session.
- Not a re-implementation of the substrates it composes. Chitin's swarm (`swarm/`) reads kanban from **hermes** and dispatches via **openclaw**'s Lobster workflow runtime; it does not ship a parallel kanban DB or workflow engine. See `docs/decisions/2026-05-13-swarm-readopted-composing-substrates.md`.
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

Hermes is the kanban substrate; OpenClaw is the agent-runtime substrate (Lobster workflows + acpx gateway). Chitin gates every tool call against one policy and owns the four-hop dispatch pipeline (`hermes → clawta → openclaw/Lobster → frontier-coder CLI`) that composes those substrates. The chain is the unifying contract; `gov.Gate` is the unifying enforcement point.

## Constitution + spec-kit

The kernel above gates each tool call. The **constitution + spec-kit layer** governs how the swarm dispatches and coordinates around the kernel:

- **`.specify/constitution.md`** — repo-level overlay; extends the workspace constitution at `~/workspace/.specify/constitution.md`. Both ratified via agent-bus standup threads.
- **`.specify/specs/NNN-<slug>/spec.md`** — every ticket promoted `triage → ready` MUST have a matching spec-kit entry. Forward binding (ticket body references spec path) and reverse binding (spec mentions ticket id) are both accepted.
- **Empty-branch gate** — `swarm/workflows/kanban-dispatch.lobster` refuses to push if `git rev-list --count "origin/$DEFAULT_BRANCH..HEAD" == 0`. Prevents zombie branches when workers exit without committing.
- **PR lifecycle close-intent** — `swarm/bin/clawta-pr-lifecycle` closes a ticket on PR merge when either (a) the branch name matches a trusted prefix (default `DEFAULT_PREFIXES = ("swarm/", "clawta/")`; pass `--prefix agent/` to extend) AND maps to a ticket suffix, or (b) the body contains an explicit `Closes/Fixes/Resolves t_xxx`. `Refs t_xxx` is traceability only — it never closes.
- **Loop-detected auto-unblock skip** — tickets carrying `loop_detected=true` stay with the watchdog/manual-repair path until the flag is cleared.
- **Tracked source over local patches** — shared swarm runtime scripts (bridge, controller, lifecycle, dispatch workflow, watchdog prompt) ship with tracked sources + idempotent installers at `swarm/bin/install-*.sh`. Drift between repo source and deployed runtime is a bug.
- **Worktree discipline** — `~/workspace/chitin/` is the primary checkout and is read-only for branch work; sibling worktrees (`~/workspace/chitin-*`) are the dispatch targets.

76 specs (001–730) live in [`.specify/specs/`](./.specify/specs/). See [`INDEX.md`](./.specify/specs/INDEX.md) for current status. Spec 004 (driver-allowlist) is explicitly **blocked** on `chitin-kernel drivers list --json` (tracked by `t_7c9d02b7`).

## Quick start

```bash
# Build the kernel
go build -o ~/.local/bin/chitin-kernel ./go/execution-kernel/cmd/chitin-kernel/

# Install per-driver hooks (each is idempotent)
chitin-kernel install --surface claude-code --global
bash scripts/install-codex-hook.sh
bash scripts/install-gemini-hook.sh
bash scripts/install-hermes-hook.sh
# Copilot CLI runs via the in-kernel wrapping driver:
#   chitin-kernel drive copilot "<prompt>"
# openclaw loads chitin-governance as a before_tool_call plugin.

# Inspect activity
chitin-kernel chain-info --chain-id <id>
chitin-kernel envelope status
chitin-kernel health
```

See [`docs/governance-setup.md`](./docs/governance-setup.md) for per-driver install paths, the policy schema, kill switches, and the escalation ladder.

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
│   ├── run-sdk/                 # standalone TS run-event SDK for third-party tools
│   ├── telemetry/               # read/query side over the event chain
│   ├── router-plugin-api/      # typed API for external router plugins
│   ├── adapters/                # operator-installed driver-side adapters
│   ├── governance/              # governance policy utilities
│   └── scheduler/               # scheduling primitives for swarm workflows
├── go/run-sdk/                  # standalone Go run-event SDK (no kernel dependency)
├── apps/
│   ├── cli/                     # operator CLI (`chitin` — events, replay, health,
│   │                            #   ledger, wiki ingest/compile/ask/lint)
│   ├── chitin-console/          # operator-facing Angular UI (mobile-first)
│   ├── chitin-console-api/      # Node API for chitin-console (kanban + bus + suggestions)
│   ├── chitin-dashboard/        # web dashboard for chain visualization
│   ├── agentguard-vscode/       # VS Code extension for agent guardrails
│   ├── mcp-server/              # MCP server exposing chain queries
│   ├── runner/                  # kernel runner / process manager
│   ├── slack-app/               # Slack integration for chain alerts
│   └── openclaw-plugin-governance/ # openclaw before_tool_call plugin
├── python/analysis/             # gate-derived analyzers (decisions, debt, predict, detect,
│                                #   analyzer cron per spec 007)
├── swarm/                       # swarm tooling — kept here until 3+ active dispatch repos
│   ├── workflows/               #   kanban-dispatch.lobster, hermes-clawta-bridge.py,
│   │                            #   _pick_driver.py, analyzer-cron.lobster
│   ├── bin/                     #   swarm-controller, clawta-pr-lifecycle, install-*.sh,
│   │                            #   board_resolver, board-aware audit scripts
│   ├── prompts/                 #   board-watchdog.md (tracked cron prompt source)
│   └── tests/                   #   regression tests for poller/bridge/lifecycle gates
├── .specify/                    # spec-kit layer (per Constitution §5)
│   ├── constitution.md          #   chitin repo overlay; extends workspace constitution
│   └── specs/NNN-<slug>/spec.md #   76 specs (001–730) — the dispatch gate
├── infra/systemd/               # 7 user-mode timers (redeploy, unlock, chain-watch,
│                                #   envelope-rotate, codex-ingest, codex-usage, judge-backfill)
├── examples/                    # copyable examples: policy packs and router plugins
├── docs/decisions/              # 8 durable boundary docs (positioning, scope, culls)
├── docs/runbooks/               # 17 operator runbooks (health, router, dispatch-pipeline…)
├── docs/superpowers/specs/      # auto-generated INDEX.md from spec-kit
├── chitin.yaml                  # policy (signed)
├── scripts/                     # installers, MANIFEST.yaml + check-scripts-manifest.sh
│                                #   (per spec 002), regen-spec-index, lint helpers
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
├── usage/<driver>.json                # universal usage feed (codex 5h/weekly, etc.)
├── current-envelope                   # active cost envelope
└── kernel-errors.log                  # kernel-side errors (read by `chitin health`)
```

## Documentation

**Positioning & scope**

- [`docs/thesis.md`](./docs/thesis.md) — what chitin is in one paragraph
- [`docs/decisions/2026-05-06-execution-governance-runtime-positioning.md`](./docs/decisions/2026-05-06-execution-governance-runtime-positioning.md) — the moat
- [`docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`](./docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md) — the boundary
- [`docs/decisions/2026-05-13-swarm-readopted-composing-substrates.md`](./docs/decisions/2026-05-13-swarm-readopted-composing-substrates.md) — composing hermes + openclaw substrates
- [`docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`](./docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md) — operator approvals live in hermes

**Architecture**

- [`docs/architecture.md`](./docs/architecture.md) — kernel internals + system diagram
- [`docs/architecture/layer-contracts.md`](./docs/architecture/layer-contracts.md) — the four locked invariants
- [`docs/event-model.md`](./docs/event-model.md) — canonical envelope + chain shape
- [`docs/operating-model.md`](./docs/operating-model.md) — topology, subsystem ownership, what's live
- [`libs/run-sdk/README.md`](./libs/run-sdk/README.md) and [`go/run-sdk/README.md`](./go/run-sdk/README.md) — third-party SDK quickstarts

**Operate**

- [`docs/governance-setup.md`](./docs/governance-setup.md) — per-driver install paths, policy schema, kill switches
- [`docs/driver-conformance.md`](./docs/driver-conformance.md) — driver hook matrix and normalizer gaps
- [`docs/roadmap.md`](./docs/roadmap.md) — strategic arc + what's in flight
- [`examples/README.md`](./examples/README.md) — copyable examples, including stack-specific policy packs
- [`docs/runbooks/`](./docs/runbooks/) — health, router, sandbox, regression-gate, spec lifecycle, swarm SDLC
- [`.specify/specs/INDEX.md`](./.specify/specs/INDEX.md) — spec index (76 entries, 001–730)

**Mirrored from substrate**

- [`docs/governance-setup-extras/README.md`](./docs/governance-setup-extras/README.md) — `kanban-dispatch.lobster` mirror + sync policy

**Archived history**

- [`docs/archive/`](./docs/archive/) — observation logs and dated audits preserved out of the main path

## Recent posture (2026-05-08 cull pass)

Three lenses (Knuth correctness, da Vinci architecture, Sun Tzu positioning) audited the repo. Convergent finding: chitin drifted into building parallel-to-substrate features (operator approvals, in-gate LLM advisor, MCP server hosting, TS governance substrate, half-finished orchestration). All culled. Read the decisions in order:

1. `docs/decisions/2026-05-06-execution-governance-runtime-positioning.md` — the moat narrative
2. `docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md` — the boundary
3. `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md` — operator approvals → hermes
4. `docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md` — in-gate LLM removed; signals stay
5. `docs/decisions/2026-05-08-cull-libs-governance-ts-substrate.md` — parallel TS adjudicator deleted
6. `docs/decisions/2026-05-08-cull-mcp-server-tools-as-subcommands.md` — MCP hosting deleted; tools → kernel subcommands

## License

Apache 2.0.