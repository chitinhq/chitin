# Chitin

**Chitin is a local execution-governance runtime for AI coding agents.**

It sits between agent CLIs and your workstation, normalizes every tool call into one action vocabulary, evaluates that action against `chitin.yaml`, and writes the result to a hash-linked audit chain under `~/.chitin/`. The point is not to replace Claude Code, Codex, Gemini, Copilot, Hermes, or OpenClaw. The point is to make their local side effects deterministic, inspectable, and governed by one policy.

Apache-2.0. Local-first. Operator-owned data.

```
Claude Code   Codex CLI   Gemini CLI   Copilot CLI   Hermes   OpenClaw
     │            │            │             │           │         │
     └────────────┴────────────┴──────┬──────┴───────────┴─────────┘
                                      │ tool calls
                                      ▼
                         ┌──────────────────────────┐
                         │ chitin-kernel gate       │  ← chitin.yaml
                         │ normalize → policy       │
                         │ bounds → counter         │
                         │ envelope → audit → OTEL  │
                         └────────────┬─────────────┘
                                      ▼
                 ~/.chitin/{gov-decisions-*.jsonl, events-*.jsonl,
                            gov.db, chain_index.sqlite}
```

## What Chitin owns

Chitin owns the enforcement and evidence layer:

1. **Kernel gate** — `chitin-kernel gate evaluate` is the one runtime enforcement point. It receives normalized actions, evaluates policy, records decisions, increments agent severity counters, applies envelopes, and returns allow/deny/guide decisions.
2. **Driver normalization** — each supported surface maps its vendor-specific tool-call payload into the canonical action model in `go/execution-kernel/internal/driver/`.
3. **Tamper-evident chain** — events and governance decisions are appended as canonical JSON, linked by SHA-256, and materialized into SQLite read models for analysis and replay.
4. **Policy and bounds** — `chitin.yaml` defines action-level rules, path guards, branch guards, lines/files ceilings, denial escalation, and lockdown behavior.
5. **Spec-driven swarm infrastructure** — this repo currently also houses the transitional swarm tooling and the emerging Temporal-based Chitin Orchestrator that moves agent work from cron/script sprawl into durable workflows.

## What Chitin does not own

These are boundaries, not missing features:

- **Not an agent framework.** Agents still run in their native CLIs and runtimes.
- **Not a model router.** Model choice belongs to the driver or orchestrator. Chitin records and governs the resulting local effects.
- **Not an approval UX.** Chitin denies or guides. Operator prompting and allowlists live in the surrounding substrate, especially Hermes.
- **Not a SaaS.** The product boundary is the operator's box. OTEL export is optional and non-authoritative.
- **Not a second kanban or chat system.** Legacy board/bus tooling exists because it is being migrated. New executable swarm work should flow through the Chitin Orchestrator design, not direct driver dispatch.

## Why it exists

AI coding agents are powerful but unstable as execution substrates. Their vendors, tool schemas, prompts, and model behavior change constantly. Chitin puts stable primitives around that unstable layer:

- one action vocabulary across drivers
- typed policy over structured actions, not regex-only shell matching
- one local audit chain across sessions and tools
- replayable evidence for what happened and why a gate fired
- per-agent severity counters that survive across tasks
- bounds checks on push-shaped actions
- heuristic signals like blast radius, floundering, and drift stamped onto the chain

The long-term thesis is spec-driven development with telemetry: specs define intent, the orchestrator executes work units, the kernel gates every side effect, and the chain teaches the next build.

## Current architecture

### Runtime path

1. A supported agent attempts a tool call.
2. The agent's hook, plugin, or wrapper calls `bin/chitin-router-hook` or `chitin-kernel` directly.
3. The driver normalizer converts the payload into a canonical action.
4. `gov.Gate.Evaluate` checks `chitin.yaml`, bounds, branch/worktree posture, escalation state, and cost envelope.
5. The kernel writes a decision row and returns the verdict.
6. Downstream tools can read the chain, replay sessions, emit OTEL, or mine policy improvements.

### Supported driver surfaces

| Surface | Integration shape | Runtime path |
|---|---|---|
| Claude Code | PreToolUse hook | `go/execution-kernel/internal/driver/claudecode/` |
| Codex CLI | PreToolUse hook | `go/execution-kernel/internal/driver/codex/` + `scripts/install-codex-hook.sh` |
| Gemini CLI | BeforeTool hook | `go/execution-kernel/internal/driver/gemini/` + `scripts/install-gemini-hook.sh` |
| Hermes | `pre_tool_call` hook | `go/execution-kernel/internal/driver/hermes/` + `scripts/install-hermes-hook.sh` |
| Copilot CLI | kernel wrapper | `chitin-kernel drive copilot` |
| OpenClaw | `before_tool_call` plugin | `apps/openclaw-plugin-governance/` |

Codex, Gemini, and Hermes intentionally share the Claude-style hook pipeline where possible. The shim stamps `CHITIN_DRIVER`; the vendor-specific normalizer handles tool names and payload details.

### On-disk state

By default Chitin writes to `$HOME/.chitin/`. Override with `CHITIN_HOME` or a command-specific `--chitin-dir` where supported.

```
~/.chitin/
├── events-<run_id>.jsonl              # canonical event chain, one file per run
├── gov-decisions-YYYY-MM-DD.jsonl     # daily gate decisions
├── gov.db                             # SQLite: envelope state, agent_state, counters
├── chain_index.sqlite                 # materialized chain index
├── usage/<driver>.json                # usage feeds for budget/status tooling
├── current-envelope                   # active cross-process cost envelope marker
└── kernel-errors.log                  # kernel-side errors read by health checks
```

The JSONL chain is the source of truth. SQLite and OTEL are projections.

## Quick start for a local checkout

```bash
# install JS/Nx dependencies
pnpm install

# build the Go kernel through Nx
pnpm exec nx run execution-kernel:build

# or install the kernel binary into ~/.local/bin
pnpm run install-kernel

# install supported hooks
chitin-kernel install --surface claude-code --global
bash scripts/install-codex-hook.sh
bash scripts/install-gemini-hook.sh
bash scripts/install-hermes-hook.sh

# inspect the current local state
chitin-kernel health
chitin-kernel gate status
chitin-kernel envelope status
```

For a fresh worktree, run:

```bash
scripts/bootstrap-worktree.sh
```

It prepares local dependencies for Nx without reusing the primary checkout as a work surface.

## Common commands

```bash
# Go kernel
pnpm exec nx run execution-kernel:build
pnpm exec nx run execution-kernel:lint
(cd go/execution-kernel && go test ./...)

# TypeScript / Nx projects
pnpm exec vitest run
pnpm exec nx run @chitin/cli:test
pnpm exec nx run @chitin/contracts:test
pnpm exec nx run @chitin/telemetry:test
pnpm exec oxlint .
pnpm exec eslint .

# Operator CLI
pnpm exec nx run @chitin/cli:run -- --help
pnpm exec tsx apps/cli/src/main.ts --help

# Console dogfood stack
pnpm run console
```

CI and local agents use Node 22 and pnpm 10. If `better-sqlite3` did not build during install, run `pnpm rebuild better-sqlite3`.

## Repo layout

```
.
├── go/
│   ├── execution-kernel/        # chitin-kernel, router-hook, gov gate, drivers, chain
│   ├── chainhash/               # shared hash/canonicalization helper
│   └── run-sdk/                 # Go run-event SDK
├── apps/
│   ├── cli/                     # operator CLI surface
│   ├── chitin-console/          # Angular operator console
│   ├── chitin-console-api/      # API backing the console
│   └── openclaw-plugin-governance/
├── libs/
│   ├── contracts/               # canonical schemas and shared TS contracts
│   ├── telemetry/               # read/query side over event chains
│   ├── run-sdk/                 # TypeScript run-event SDK
│   ├── adapters/                # thin driver-side adapters
│   └── router-plugin-api/       # external router plugin API bindings
├── python/
│   ├── analysis/                # chain-derived analysis tools
│   └── argus/                   # local research / analysis service experiments
├── swarm/                       # transitional swarm tooling and orchestrator migration code
│   ├── bin/                     # bounded scripts, installers, audits, bench runners
│   ├── workflows/               # legacy Lobster / dispatch workflows
│   ├── roles/                   # role prompts for worker lanes
│   ├── systemd/                 # one-box services for orchestrator/console/bench
│   └── tests/                   # regression tests for swarm governance behavior
├── scripts/                     # install scripts, hooks, guards, status tools
├── tools/                       # Nx generators and repo lints
├── docs/                        # architecture, decisions, runbooks, strategy
├── examples/                    # policy packs and plugin examples
├── souls/                       # cognitive-lens reference material
├── .specify/                    # repo constitution and active spec corpus
├── chitin.yaml                  # signed baseline governance policy
└── AGENTS.md                    # agent handoff and repository boundary rules
```

## Spec-driven work

Chitin is run as a spec-first repo.

- Repo constitution: [`.specify/constitution.md`](./.specify/constitution.md)
- Spec index: [`.specify/specs/INDEX.md`](./.specify/specs/INDEX.md)
- Specs: [`.specify/specs/`](./.specify/specs/)
- Build/test conventions: [`.github/copilot-instructions.md`](./.github/copilot-instructions.md)

Important rules:

1. **Worktrees are mandatory.** The primary checkout stays on `main` and is not a work surface. Use a sibling worktree for every branch.
2. **Spec before implementation.** Implementation work needs a reviewed spec binding unless it is an operator-approved emergency hotfix.
3. **Tests bind to specs.** Specs include test coverage. Test files carry `spec: NNN-<slug>` references where the gate expects them.
4. **Author and reviewer are different.** Governance, specs, and implementation PRs need cross-review.
5. **The orchestrator is the swarm.** New executable swarm work should be represented as orchestrator-managed work units, not ad-hoc direct calls to drivers.

Create a worktree with the repo script when possible:

```bash
pnpm run worktree -- --branch <branch-name>
# or
bash scripts/create-worktree.sh --agent <agent> --task <slug>
```

## Swarm and orchestrator posture

The repo still contains legacy swarm infrastructure because it is being migrated, not because every script is a product surface.

Current direction:

- **Kernel remains the enforcement point.** Nothing bypasses `gov.Gate` for local side effects.
- **Temporal-based Chitin Orchestrator is the target control plane.** Specs 070 and 075-081 define durable workflows, driver contracts, spec-DAG scheduling, spec adapters, telemetry feedback, and cron/board retirement.
- **No driver bypass.** Spec 092 tracks the invariant that implementation-producing driver invocations carry orchestrator work-unit attribution.
- **Merge and review become workflows.** Specs 093 and 094 move PR merge and review policy into orchestrated, audited flows.
- **Continue Checks is a narrow pilot.** Spec 095 evaluates PR-governance checks. It is not a Hermes replacement and not an inbound webhook surface.

Read [docs/strategy/chitin-orchestrator-options-2026-05-20.md](./docs/strategy/chitin-orchestrator-options-2026-05-20.md) for the engine decision and [docs/strategy/chitin-spec-driven-platform.md](./docs/strategy/chitin-spec-driven-platform.md) for the broader spec-driven platform thesis.

## Documentation map

Start here:

- [AGENTS.md](./AGENTS.md) — repository boundaries for AI agents
- [docs/architecture.md](./docs/architecture.md) — kernel and driver architecture
- [docs/architecture/layer-contracts.md](./docs/architecture/layer-contracts.md) — locked layer invariants
- [docs/event-model.md](./docs/event-model.md) — event envelope and chain model
- [docs/operating-model.md](./docs/operating-model.md) — how the system runs on the operator box
- [docs/governance-setup.md](./docs/governance-setup.md) — hook installation, policy schema, and kill switches
- [docs/driver-conformance.md](./docs/driver-conformance.md) — driver hook matrix and normalizer expectations
- [docs/roadmap.md](./docs/roadmap.md) — historical roadmap and shipped/in-flight context
- [docs/runbooks/](./docs/runbooks/) — operational runbooks

Key decision records:

- [2026-05-06 execution governance runtime positioning](./docs/decisions/2026-05-06-execution-governance-runtime-positioning.md)
- [2026-05-06 scope narrow to kernel](./docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md)
- [2026-05-08 defer approvals to Hermes](./docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md)
- [2026-05-08 remove advisor from kernel hot path](./docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md)
- [2026-05-08 remove MCP hosting from Chitin](./docs/decisions/2026-05-08-cull-mcp-server-tools-as-subcommands.md)
- [2026-05-13 compose with Hermes and OpenClaw substrates](./docs/decisions/2026-05-13-swarm-readopted-composing-substrates.md)

## Development notes

- The Go kernel is the only layer that writes chain/governance state.
- TypeScript contracts and telemetry are read-side or adapter layers.
- When changing the event model, update TS schemas, Go structs, adapters, and telemetry together.
- Nx layer tags in `eslint.config.mjs` are intentional. Do not paper over dependency-boundary failures.
- `chitin.yaml.sig` signs `chitin.yaml`. If policy content changes, the operator must regenerate the signature through the approved signing path.
- `scripts/MANIFEST.yaml` and `scripts/check-scripts-manifest.sh` track operator-box scripts. Runtime scripts need tracked source and installer/verify coverage.
- Direct edits to the primary checkout are governance violations. If you are an agent, announce the worktree path before the first write.

## License

Apache-2.0. See [LICENSE](./LICENSE).
