# Chitin

**Chitin is a local execution-governance runtime for AI coding agents.**

It sits between agent CLIs and your workstation, normalizes every tool call into one action vocabulary, evaluates that action against `chitin.yaml`, and writes the result to a hash-linked audit chain under `~/.chitin/`. The point is not to replace Claude Code, Codex, Gemini, Copilot, Hermes, or OpenClaw. The point is to make their local side effects deterministic, inspectable, and governed by one policy.

Apache-2.0. Local-first. Operator-owned data.

```
Specs / backlog / schedules                  GitHub webhooks (push, pull_request,
          │                                   pull_request_review)
          ▼                                                  │
┌─────────────────────────────┐                              ▼
│ Chitin Orchestrator         │  Temporal     ┌─────────────────────────────┐
│ go/orchestrator/            │◀──────────────│ factory-listen :8765        │
│ scheduler / work units      │  dispatch     │ /webhook/push  /webhook/pr  │
│ PRIterationWorkflow         │               └──────────────┬──────────────┘
│ SiblingRebaseWorkflow       │                              │ Discord escalation
└─────────────┬───────────────┘                              ▼
              │ attributed driver invocations            ┌──────────┐
              ▼                                          │ operator │
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

Two control flows close the loop:

1. **Forward path** (specs → PRs): a spec dispatch produces work units → drivers author code → PRs open.
2. **Feedback path** (review → fixups): factory-listen receives PR webhooks → on a Copilot review for a chitin-authored PR, `PRIterationWorkflow` re-invokes the authoring driver with the comment context and pushes a fixup commit. On a sibling-PR merge, `SiblingRebaseWorkflow` auto-rebases the other in-flight siblings. Escalations (cap hit, conflict the rebase can't resolve, low-confidence verdict) ping the operator's Discord with a clickable PR link.

## What Chitin owns

Chitin owns the orchestration, enforcement, and evidence layer:

1. **Orchestrator control plane** — `go/orchestrator/` is the Temporal-based control plane for spec/backlog/schedule driven work. It turns work into durable, attributed work units and owns scheduler, review, merge, ingest, and loop workflows as they migrate out of cron, boards, and shell sprawl.
2. **Kernel gate** — `chitin-kernel gate evaluate` is the one runtime enforcement point for local side effects. It receives normalized actions, evaluates policy, records decisions, increments agent severity counters, applies envelopes, and returns allow/deny/guide decisions.
3. **Driver normalization** — each supported surface maps its vendor-specific tool-call payload into the canonical action model in `go/execution-kernel/internal/driver/`.
4. **Tamper-evident chain** — events and governance decisions are appended as canonical JSON, linked by SHA-256, and materialized into SQLite read models for analysis and replay.
5. **Policy and bounds** — `chitin.yaml` defines action-level rules, path guards, branch guards, lines/files ceilings, denial escalation, and lockdown behavior.
6. **Spec-driven swarm infrastructure** — this repo currently also houses the transitional swarm tooling being absorbed by the Chitin Orchestrator.

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

There are two connected paths:

1. **Control path:** a spec, backlog item, schedule, review, or merge request becomes an orchestrator workflow in `go/orchestrator/`.
2. The orchestrator assigns an auditable work unit and invokes the appropriate driver surface with that attribution.
3. **Enforcement path:** the supported agent attempts a tool call.
4. The agent's hook, plugin, or wrapper calls `bin/chitin-router-hook` or `chitin-kernel` directly.
5. The driver normalizer converts the payload into a canonical action.
6. `gov.Gate.Evaluate` checks `chitin.yaml`, bounds, branch/worktree posture, escalation state, and cost envelope.
7. The kernel writes a decision row and returns the verdict.
8. Downstream tools can read the chain, replay sessions, emit OTEL, or mine policy improvements.

The orchestrator decides **what work runs and why**. The kernel decides **whether each local side effect is allowed**.

### Autonomous review-iteration loop

As of spec 113 (US1 MVP, 2026-05-25) the orchestrator closes the loop on Copilot review comments without operator intervention. The chain runs end-to-end on every chitin-authored PR:

1. **Initial review**: GitHub fires `pull_request_review.submitted` when Copilot reviews a `chitin/wu/*` branch. `factory-listen` (`/webhook/pr`) routes it through eligibility checks (Copilot allowlist + factory-authored branch).
2. **Iteration dispatch**: `dispatchPRIteration` starts `PRIterationWorkflow` with deterministic ID `iteration-pr-<N>-review-<M>` (Temporal `REJECT_DUPLICATE` dedups webhook redeliveries).
3. **Driver re-invocation**: `IteratePRReview` activity checks out the PR branch via `worktree.Manager.Checkout`, fetches the review body + line comments via `gh api`, builds an iteration prompt, and invokes the authoring driver in the worktree.
4. **Fixup push**: if the driver produces changes, the activity commits with conventional message (`review fix (round <N>): address review #<M>`) and `git push --force-with-lease`. Emits `pr_iteration_completed` chain event.
5. **Sibling cascade**: when a chitin PR merges to main, `SiblingRebaseWorkflow` (spec 112 US2) auto-rebases every other open PR carrying the same `sched/run/<id>` label. Clean rebases force-push; "both-added" conflicts fail-soft with `sibling_rebase_failed`.
6. **Escalation**: failed sibling rebases ping the operator's Discord webhook (`~/.chitin/discord-webhook.secret`) with a 🚨 marker, reason, and clickable PR link. Multi-driver re-review (spec 116) extends this with 🟢 ready-to-merge pings on autopilot-clean PRs.

The loop validated end-to-end in production on 2026-05-25 14:09 EDT — Copilot's review on PR #1057 produced a fixup commit (4 line comments addressed, including a real refactoring of the eligibility-field contract) with zero operator action between webhook delivery and `git push`.

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
│   ├── orchestrator/            # Temporal workflows, schedules, work units, loops
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
- **Factory loop closes on PR feedback.** Spec 098 ships the `factory-listen` webhook receiver; spec 099 handles GitHub-native (Copilot) dispatch. Spec 112 serializes parallel-merge work (US1 file-overlap edges + US2 sibling auto-rebase). Spec 113 is the PR-comment-respond loop that re-invokes the authoring driver on review and pushes fixup commits. Specs 114, 115, 116 are drafted (operator escalation queue, spec-PR review gate, multi-driver re-review) and queued for implementation.

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
- [docs/runbooks/spec-113-pr-comment-respond-loop.md](./docs/runbooks/spec-113-pr-comment-respond-loop.md) — how the autonomous review-iteration loop runs, how to trigger one manually, how to verify outcomes

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
