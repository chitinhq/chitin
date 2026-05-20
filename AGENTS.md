# AGENTS.md — Picking up chitin work

This file is the universal pickup point for any AI coding agent
(Codex CLI, Copilot CLI, Cursor, OpenHands, Claude Code, future
substrates). Read it once before touching code.

For build/test/lint commands, see `.github/copilot-instructions.md`.
This file is about **what chitin is, what it isn't, and what's
allowed to ship**.

## What chitin IS

**Execution governance runtime for heterogeneous AI coding agents.**
A Go kernel that sits in the PreToolUse path of every supported
driver and writes a tamper-evident chain. That's the whole product.

The kernel does three things:

1. **Gate.** `chitin-kernel gate evaluate` consumes a tool-call
   payload, normalizes it via the per-driver
   `internal/driver/{claudecode,codex,gemini,hermes,copilot}/normalize.go`,
   evaluates it against `chitin.yaml`, and emits a deny/allow.
2. **Chain.** Every decision lands in
   `~/.chitin/gov-decisions-YYYY-MM-DD.jsonl` and the materialized
   `chain_index.sqlite`. SHA-256 linked. Replay-able. Cross-driver,
   cross-session.
3. **Signals.** `internal/router/{blast_radius,floundering,drift}.go`
   compute pure-Go heuristics from the chain tail. They're
   stamped onto a second `gov.Decision` row with `omitempty` fields
   (`PredictedBlast`, `FlounderingScore`, `DriftScore`,
   `RoutingDecision`). **The kernel never spawns an LLM in-line**
   (cull 2026-05-08, see decision doc below).

## What chitin is NOT

If you find yourself building any of these, stop and re-read this
section. They live in the substrate, not in chitin:

- **Not an orchestrator.** Work tracking, kanban, dispatch, cron,
  workflows, retries — that's **hermes**.
- **Not an approval system.** `tools/approval.py` in hermes
  provides `manual|smart|off`, 4-way `[once|session|always|deny]`
  reply parsing, and persistent `command_allowlist`. Chitin's
  gate denies; hermes prompts the operator. See
  `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`.
- **Not an LLM-runner.** No `claude -p` shellouts from inside the
  hot path. The asymmetric value is the chain + signals; LLM
  consultation happens in consumers (hermes' `approvals.mode:
  smart`, operator-wired chain readers). See
  `docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md`.
- **Not an MCP server host.** Both hermes and openclaw ship MCP
  natively. Chitin exposes its tool surface as
  `chitin-kernel <subcmd>` calls; substrates wire their own MCP
  servers to those. See
  `docs/decisions/2026-05-08-cull-mcp-server-tools-as-subcommands.md`.
- **Not an agent framework.** Agents run in their respective
  CLIs/runtimes. Chitin gates each one's tool calls; it doesn't
  host a session.
- **Not a model router.** Drivers pick the model; chitin observes
  via fingerprint dimensions in the chain.
- **Not a SaaS.** Local-only. Operator's box, operator's data.

## The four allowed buckets in this monorepo

Anything you ship here must fit one of these:

1. **The kernel** — `go/execution-kernel/` (`chitin-kernel`
   binary, `internal/gov/*`, `internal/router/*`,
   `internal/chain/*`, `internal/driver/*/`, `cmd/chitin-kernel/`).
2. **Analytics libs** — `python/analysis/` for chain-derived
   readers, `libs/contracts/` for the canonical schemas,
   `libs/telemetry/` for the read-side index/replay.
3. **Plugins** — operator-installed driver-side adapters
   (`libs/adapters/{claude-code,ollama-local,openclaw}`) and
   downstream-substrate plugins
   (`libs/openclaw-plugins/*`, future `libs/hermes-plugins/*`).
4. **Apps built off the kernel** — `apps/cli/` (the operator's
   `chitin` CLI). The bar for adding a new app: it must
   non-trivially consume kernel state in a way no substrate
   already does.

If your work doesn't fit a bucket, it doesn't ship here. Push back
or surface the misfit to the operator.

## The moat (what chitin alone provides)

Asymmetric strengths nothing else in the ecosystem ships:

1. **Cross-driver canonical action vocabulary.** Hermes'
   `pre_tool_call` is hermes-only. OpenClaw's `before_tool_call`
   is pi-runtime-only. Chitin alone gates Claude Code, Codex,
   Gemini, Copilot, OpenClaw, and Hermes against one shared
   action enum (`internal/gov/action.go`).
2. **Typed-action policy.** `chitin.yaml` evaluates structured
   actions against `path_under` / `bounds` / `branches`
   predicates. Not regex-on-shell-string.
3. **Tamper-evident chain across all drivers + sessions.**
   SHA-256-linked JSONL with SQLite index. Replay-able.
4. **Per-agent severity ladder + lockdown counter.** `agent_state`
   in `gov.db` tracks behavior across all tasks and drivers.
   Lifetime-spanning, not per-task.
5. **Bounds enforcement** on push-shaped actions (lines/files
   changed). No substrate equivalent.
6. **Heuristic signals** stamped on the chain — blast-radius,
   floundering, drift — pure Go, no LLM cost.

When proposing new work, the question is always: **does this
deepen one of these asymmetries?** If yes, ship. If no, the
substrate already does it (or should).

## Recent posture (2026-05-08 cull pass)

Three lenses (Knuth correctness, da Vinci architecture, Sun Tzu
positioning) audited the repo. Convergent finding: chitin had
drifted into building parallel-to-substrate features (operator
approvals, in-gate LLM advisor, MCP server hosting, TS
governance substrate, half-finished orchestration). All culled.

Decisions to read in order:

- `docs/decisions/2026-05-06-execution-governance-runtime-positioning.md`
  — the moat narrative
- `docs/decisions/2026-05-06-chitin-scope-narrow-to-kernel.md`
  — the boundary (what chitin won't do)
- `docs/decisions/2026-05-08-cull-escalate-defer-to-hermes.md`
  — operator approvals → hermes
- `docs/decisions/2026-05-08-cull-advisor-out-of-kernel-hot-path.md`
  — in-gate LLM consultation removed; signals stay
- `docs/decisions/2026-05-08-cull-libs-governance-ts-substrate.md`
  — parallel TS adjudicator deleted; Go kernel is canonical
- `docs/decisions/2026-05-08-cull-mcp-server-tools-as-subcommands.md`
  — MCP hosting deleted; tools re-exposed as kernel subcommands

When in doubt, the decision doc is the truth. README + roadmap
narrate; decisions adjudicate.

## How to compose with what the operator already runs

```
        Claude Code   Codex CLI   Gemini CLI   Copilot CLI   OpenClaw   Hermes
              │           │            │            │            │         │
              └─────┬─────┴─────┬──────┴──────┬─────┴─────┬──────┴────┬────┘
                    │ tool calls (PreToolUse / SDK / hook / plugin)
                    ▼
            ┌──────────────────────────────────────────┐
            │ chitin-kernel gate                       │  ◄── chitin.yaml
            │   normalize → policy → bounds → counter  │
            │   → envelope → audit → OTEL              │
            └──────────────────────────────────────────┘
                    │
                    ▼
            ~/.chitin/{events-*.jsonl, gov-decisions-*.jsonl,
                       gov.db, chain_index.sqlite}
```

Hermes runs orchestration (kanban, cron, approvals). OpenClaw
runs the personal-AI gateway. Chitin gates every tool call from
both, plus the standalone CLI drivers, against one policy.

## Swarm and dispatch posture (2026-05-17)

The Chitin swarm is transitional runtime/tooling that still lives in
this repo. Treat it as operator infrastructure, not kernel product
surface.

- **Branch convention:** current worker branches use `agent/*`.
  Older `swarm/*` and `clawta/*` branches are legacy/controller
  conventions. Reviewers and lifecycle tooling must include `agent/*`
  when scanning worker PRs.
- **No primary-checkout edits:** branch work happens in sibling
  worktrees under `~/workspace/chitin-<slug>`. The primary checkout at
  `~/workspace/chitin` stays on `main` for read/controller operations.
- **Spec before dispatch:** triage → ready promotion requires a
  merged/tracked spec-kit entry at `.specify/specs/NNN-<slug>/spec.md`
  for Chitin-owned work. Untracked local spec files do not count.
- **Spec gate is bidirectional:** dispatch gates accept either an
  explicit ticket-body spec path or a spec file that references the
  ticket id. Keep ticket/spec reverse bindings exact (`t_1234abcd`).
- **Empty-branch gate:** worker finalize must prove
  `git rev-list --count origin/$DEFAULT_BRANCH..HEAD > 0` before push.
  Empty branches fail closed with `failure-kind=empty_branch`.
- **Board-aware scripts:** swarm scripts must resolve board/repo/default
  branch from board config or explicit flags (`--board`, `KANBAN_BOARD`),
  not hard-coded Chitin paths.
- **Watchdog prompt is tracked:** board-watchdog prompt/runtime config is
  installed from tracked source via `swarm/bin/install-*.sh`; deployed
  prompt drift is a bug, not an operator memory exercise.
- **Lifecycle closure is exact-match:** PR lifecycle closes tickets only
  on branch-derived close intent or explicit `Closes/Fixes/Resolves
  t_...`; `Refs t_...` only links.

## When you're stuck

- Check `docs/decisions/` for the most recent dated decision —
  it usually answers boundary questions.
- Check `docs/roadmap.md` for shipped vs in-flight vs deferred.
- Read `chitin.yaml` and `internal/gov/action.go` to see the
  policy surface.
- For build/test/lint, `.github/copilot-instructions.md`.
- For swarm dispatch/lifecycle behavior, read
  `docs/runbooks/dispatch-pipeline.md` and the current
  `.specify/specs/*/spec.md` entry for the ticket.

If a feature seems missing, the substrate probably has it. Look
in hermes (`tools/approval.py`, `kanban_*`, plugin hooks) and
openclaw (`before_tool_call`, exec-approvals.json) before
proposing a chitin-side build.

<!-- SPECKIT START -->
This repo is a spec-kit project. For spec-driven work, use the Codex
skills under `.agents/skills/speckit-*`:

- `$speckit-specify` for a new or revised spec in `.specify/specs/`.
- `$speckit-plan` and `$speckit-tasks` before implementation work.
- `$speckit-implement` only after a spec, plan, and tasks exist.
- `$speckit-analyze` or `$speckit-checklist` when consistency or
  requirements quality is uncertain.

For kanban-driven work, read the bound `.specify/specs/*/spec.md`,
`plan.md`, and `tasks.md` before editing. If a ticket lacks a reviewed
spec-kit binding, do not implement it; route it back through grooming.
<!-- SPECKIT END -->
