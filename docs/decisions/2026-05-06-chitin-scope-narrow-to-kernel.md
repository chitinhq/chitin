# Chitin owns kernel + plugins + data — nothing else

Status: durable boundary. Tightens the positioning in
`2026-05-06-execution-governance-runtime-positioning.md` from
"removed Temporal" to "remove all orchestration."

Date: 2026-05-06 (PM, post-positioning doc)

## The boundary

Chitin owns three things, exhaustively:

1. **Kernel** — the Go binary at `chitin-kernel`: gate, escalation
   counter, lockdown, envelope, audit. Single side-effect authority.
2. **Driver plugins** — `internal/driver/{claudecode,codex,gemini,
   hermes,copilot}`: hook payload normalization + decision formatting.
   Adapters between vendor-specific tool vocabularies and the
   kernel's canonical action enum.
3. **The data** — `~/.chitin/{gov-decisions-*.jsonl, events-*.jsonl,
   gov.db, chain_index.sqlite}`: the chain itself + the analysis
   surface that reads it (`python/analysis/*`).

And the operational scaffolding required to keep those three healthy
(systemd timers for kernel redeploy, agent-unlock, chain-watch,
envelope-rotate). These are operationally chitin-internal; they're
not "orchestration."

## The boundary, restated as exclusions

Chitin does NOT own:

- Work tracking (backlog, kanban, board state, statuses)
- Dispatch (picking what to run next, spawning runners)
- Scheduling (when to run, how often, in what order)
- Workflow definitions (DAGs, retries, durable state)
- PR-merge → status-flip pipelines
- Mirroring between any two work-tracking surfaces
- Linting work-tracking schemas
- The work-tracking source-of-truth file itself

Whatever orchestrator the operator runs (today: hermes kanban;
tomorrow: maybe something else) is *agnostic* — chitin doesn't know
or care. The orchestrator dispatches, agents do work, every tool
call passes through chitin's gate, every decision lands in chitin's
chain. The orchestrator doesn't need to integrate with chitin
beyond installing the gate hook in its agent CLIs.

## Why this matters more than yesterday

The positioning doc said "Hermes Kanban absorbs the orchestration
role." The unstated assumption was that all orchestration code in
chitin's repo would migrate out. That migration didn't happen —
~1700 LOC of orchestration code (dispatcher, kanban mirror, kanban
bridge, groomer, status flippers) still lives in
`apps/runner/src/`, plus a 6700-line markdown work-tracking file
(`docs/swarm-backlog.md`) chitin still owns and maintains.

Symptoms today (2026-05-06 PM):
- Operator nudged a kanban card; nothing happened. Because chitin's
  dispatcher reads `swarm-backlog.md`, not kanban.
- 23 ready tasks in kanban sitting 29h, no daemon dispatching them.
- 5 downstream entries blocked-by chains where the root blockers
  are stuck in `partial` status because chitin's `partial → done`
  flipper either doesn't exist or isn't running.
- Dispatcher fires every 5min and finds mostly skipped entries
  because the dependency model lives in markdown frontmatter that
  goes stale.

These aren't bugs to fix individually. They're symptoms of chitin
owning code that shouldn't be in chitin's repo at all.

## What stays vs what goes (concrete inventory)

### Stays (kernel + plugins + data, all currently correct)

- `go/execution-kernel/cmd/chitin-kernel/*` — the binary
- `go/execution-kernel/internal/gov/*` — gate + policy + escalation
- `go/execution-kernel/internal/driver/*` — driver normalize/format
- `go/execution-kernel/internal/router/*` — router/advisor/peer-spawn
  (lives between gate decisions and outcomes; serves the kernel's
  escalation contract)
- `go/execution-kernel/internal/cost/*`, `tier/*`, `ingest/*` —
  kernel-internal classification and chain ingestion
- `python/analysis/*` — reads chitin's chain, produces digests
- `chitin.yaml` policy schema + reference policies
- `~/.chitin/` data layout
- Operational timers: `chitin-kernel-redeploy`, `chitin-agent-unlock`,
  `chitin-envelope-rotate`, `chitin-chain-watch`, `chitin-alarm-feeder`
  (if it sticks to chain-derived alerts)
- `scripts/chitin-{agent-unlock,envelope-rotate,chain-watch,
  status}.sh` and `scripts/install-{kernel,*-hook,systemd-units}.sh`
  — kernel deployment + lifecycle, not orchestration

### Goes (orchestration code crossing the boundary)

| File / surface | LOC | Why it goes |
|---|---|---|
| `apps/runner/src/dispatcher.ts` | 965 | Picks next entry, submits Temporal workflows. Pure orchestration. |
| `apps/runner/src/kanban-mirror.ts` | 321 | Two-way sync with hermes kanban. Coupling chitin owns. |
| `apps/runner/src/kanban-bridge.ts` | 91 | Same. |
| `apps/runner/src/kanban-card-to-request.ts` | 158 | Translation layer. |
| `apps/runner/src/kanban-pr-mirror.ts` | 225 | PR → kanban status sync. Belongs in the orchestrator. |
| `apps/runner/src/groomer.ts` + `grooming/*` | unknown | Edits the backlog file. Dies with the backlog file. |
| `apps/runner/src/pr-event-ingester.ts` | unknown | Half-on-each-side; needs split (chain-relevant pieces stay; dispatch-side goes). |
| `apps/runner/src/alarm-feeder.ts` | unknown | If it reads chain → alerts, stays. If it reads backlog, the read goes. |
| `docs/swarm-backlog.md` | 6686 lines | The work-tracking source. Lives in the orchestrator. |
| `infra/systemd/chitin-dispatcher.{service,timer}` | small | Runs the deleted dispatcher. |
| `infra/systemd/chitin-shipped-entry-flipper.{service,timer}` | small | Flips backlog statuses. |
| `tools/lint/backlog-entry-shape.ts` | unknown | Lints the deleted file. |
| `apps/runner/test/{dispatcher,grooming-*,groomer,kanban-*,review-graph}.test.ts` | unknown | Tests for the deleted code. |

Net: 1700+ LOC of TypeScript + 6700 lines of work-tracking markdown
+ several systemd units + an unknown LOC of tests removed from chitin.

### `apps/runner/` goes wholesale — no incremental migration

Initial draft of this doc tried to split `activity.ts` and
`review-graph*.ts` into "chain-side stays / dispatch-side goes."
Operator decision: don't bother. Rip the entire `apps/runner/`
tree out. Anything chain-side that turns out to be load-bearing
gets re-implemented natively in the Go kernel (where it belongs
anyway — chain emission is a kernel concern, not a TypeScript app
concern).

Justification:
- Incremental splits leave the boundary fuzzy. A clean cut forces
  the right architectural placement on what survives.
- The TypeScript runner is parallel to the Go kernel — duplicate
  surface area. Whatever logic is worth keeping is worth keeping
  in Go.
- Hermes + openclaw absorb the orchestration role. Chitin doesn't
  need a runner at all; agents are dispatched by the orchestrator
  and call the kernel via the gate hook.

## What replaces the deleted surface

- **Work tracking + dispatch:** hermes kanban + `hermes kanban
  daemon`. Operator (or a hermes-side promotion job) keeps the
  spawnable lanes (`chitin-runner`, `default`) populated with ready
  tasks. Chitin doesn't see this surface at all.
- **Status reflection:** hermes ingests PR-merged events and updates
  its own kanban. If chitin's chain is the source of "PR merged,"
  hermes pulls from chitin's chain (chitin exposes; hermes consumes).
- **The 23 currently-stuck ready tasks:** orchestrator concern.
  Triage in hermes (assign to a spawnable lane or remove).
- **The 4 partial-stuck root blockers:** if the underlying PRs are
  merged, mark them done in hermes kanban. Chitin doesn't need a
  flipper.

## Things to NOT do (durable warnings, addendum to positioning doc)

- **Don't add code to `apps/runner/`.** It's all heading to delete.
  New work that touches dispatch belongs in hermes (or wherever
  orchestration lives), not chitin.
- **Don't grow `docs/swarm-backlog.md`.** It's a deletion target.
- **Don't refactor "kanban-as-source" inside chitin.** Cuts the
  wrong way. The whole consumer surface goes; refactoring it more
  tightly to kanban makes the deletion harder.
- **Don't add hermes-aware features to chitin.** Chitin should not
  know hermes exists. Hermes can know about chitin (via gate hook +
  chain reads).

## Migration sequence

See `docs/superpowers/plans/2026-05-06-orchestration-code-deletion.md`.
