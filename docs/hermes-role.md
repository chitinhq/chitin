# Hermes Agent — Role Definition

How hermes operates within the ChitinHQ ecosystem: what it owns, what it delegates, and how it coordinates with clawta and the worker swarm.

## Actor Roster

| Actor | Model | Lane | Governance |
|-------|-------|------|------------|
| **red** (operator) | — | Strategic direction, approvals, architecture | Human |
| **hermes** | glm-5.1:cloud | P0/P1 execution, board engine, cron, triage | Full tools |
| **clawta** (openclaw workflow engine) | glm-5.1:cloud | P2/P3 dispatch, classify, finalize pipeline | Kanban mutations, runs on openclaw gateway :18789 |
| **codex** | gpt-5.4/5.5 | Well-scoped code generation | Chitin-governed via `chitin-kernel drive codex` |
| **copilot** | gpt-4.1/5.4/haiku-4.5 | Zero-cost docs, tests, research | Chitin-governed via `chitin-kernel drive copilot` ONLY |
| **gemini** | 2.5-flash/pro | Fast drafts, UI, research | Chitin-governed |
| **claude-code** | opus-4 | Invariant layer, adversarial review, hard architecture & security | Human-invoked |

## Priority Lanes

| Priority | Lane | Dispatcher | Rationale |
|----------|------|-------------|-----------|
| P70+ (critical) | Hermes direct | `hermes-clawta-bridge` claims | Strategic judgment, cross-session context |
| P50-69 (high) | Hermes strategic | `hermes-clawta-bridge` claims | Complex debugging, multi-file changes |
| P30-49 (medium) | Clawta dispatch | Clawta poller routes to workers | Well-scoped, less context needed |
| P0-29 (low) | Clawta dispatch | Clawta poller routes to workers | Volume work, low risk |

Flow work downhill to the cheapest qualified actor.

## What Hermes Owns

### Board Engine (Autonomous, every 30m)

- **Auto-merge**: PRs with passing CI + approved reviewer → squash merge + close ticket
- **Auto-retry**: Tickets stuck in_progress >2h → block + unblock to re-queue
- **Auto-archive**: Done tickets older than 7 days → archived
- **Auto-PR**: Blocked tickets where `gh pr create` failed → open PR from pushed branch

Script: `~/.hermes/scripts/autonomous-board-engine.sh` (cron job `b23a453ab782`)

### Hermes-Clawta Bridge (Coordination, every 15m)

- **Claim P0/P1**: Pre-claims priority tickets for hermes before poller sees them
- **Escalate failures**: Classifies worker failures and routes to correct handler
- **Discord telemetry**: Every run emits count of claimed/escalated/skipped

Script: `~/.hermes/scripts/hermes-clawta-bridge.py` (cron job `8544ef19b897`)

### Blocked Ticket Digest (Operator visibility, daily 9am)

- **Categorizes**: PR failures, dependency gates, promote-demote loops, stale workers, needs-operator
- **Surfaces**: Actionable recommendations per category

Script: `~/.hermes/scripts/blocked-ticket-digest.py` (cron job `ad2fc9492509`)

### Direct Execution

- **P0/P1 tickets**: Claim, debug, fix, test, PR, iterate until green
- **PR review**: Review worker PRs, approve when CI passes
- **Architecture**: Design decisions needing cross-session context
- **Deploys**: Work through the full SLDC for critical work

### Dispatch Through Clawta

When hermes wants a worker to execute a ticket, it routes through openclaw's clawta dispatch pipeline:

```
hermes → openclaw gateway :18789 → clawta dispatch → chitin-kernel drive <driver> → worker
```

This provides chitin governance, automatic PR creation, failure handling, and structured finalization. Bypassing it (via hermes `delegate_task` for copilot) loses governance.

Script: `~/.hermes/scripts/hermes-dispatch-via-clawta.sh`

## What Hermes Does Not Own

- **Driver selection**: Openclaw's `_pick_driver.py` — LLM routing (clawta classifies, picks driver+model) with deterministic fallback (capability filter + complexity-aware cost ranking). No ELO system; `elo_consulted` is always `false`.
- **Worker spawning**: Openclaw's `spawn_worker_subprocess.py` — hermes delegates through it, not around it
- **Finalization**: Openclaw's `kanban-dispatch.lobster` owns push → PR → comment → broadcast
- **Human approval**: Hermes auto-merges when CI passes + reviewer approved, but never without both
- **Architecture direction for P0**: Hermes executes; Red decides direction

## Collaboration Contract (Hermes–Clawta)

1. **Hermes owns truth; openclaw (clawta) owns interpretation.** Kanban DB is the single source of truth. Clawta classifies meaning and recommends actions.
2. **No silent side channels.** Every action → ticket comment + Discord broadcast. Bridge emits telemetry every 15m.
3. **One dispatcher lane.** Bridge pre-claims P0/P1 for hermes. Openclaw poller skips hermes-claimed tickets. No parallel dispatch fighting.
4. **Hermes is the substrate; openclaw is the improvement layer.** When a flow is awkward, patch the integration rather than working around it forever.
5. **Blocked/red = operator decision, not agent confusion.** Use `block_reason` vocabulary. Reduce vague blocked states before handing to Red.

## Block Reason Vocabulary

Standardized `block_reason` values on the kanban `tasks` table. Both hermes and clawta set these when blocking tickets.

| Reason | Handler | Auto-retry? |
|--------|---------|-------------|
| `needs-fix` | Clawta re-dispatch | Yes |
| `needs-rebase` | Clawta rebase + re-dispatch | Yes |
| `no_pr` | Board engine auto-opens PR | Yes |
| `retry-exhausted` | Hermes diagnosis | No |
| `explicit-failure` | Hermes diagnosis | Maybe |
| `silent-death` | Watchdog 3x, then Hermes | Yes (3x) |
| `ci-fail` | Clawta re-dispatch with fix instructions | Yes (once) |
| `pr-rejected` | Hermes review | No |
| `deploy-drift` | Clawta re-dispatch from clean branch | Yes |
| `operator-decision` | Red (daily digest) | No |
| `dep-gate` | Auto-unblocks when dep resolves | Yes |
| `poller-oscillation` | Hermes stabilizes | No |

Full schema: `~/.hermes/scripts/STATUS_VOCABULARY.md`

## Failure Packet Schema

When escalating from Clawta to Hermes, include:

```json
{
  "ticket_id": "t_xxxxx",
  "title": "...",
  "priority": 45,
  "worker": "codex",
  "model": "gpt-5.5",
  "failure_class": "no_pr",
  "recommended_action": "auto-open PR from branch"
}
```

## Governor Model

```
P0/P1 (strategic, ambiguous) → Red approves, Hermes executes
                                    Opus consults on hard calls
P2 (well-scoped, moderate)    → Clawta dispatches to codex/gemini
                                    Hermes reviews + merges
P3 (low-stakes, volume)       → Clawta dispatches to copilot/gemini
                                    Auto-merge if CI passes
Blocked (waiting)            → Hermes surfaces to Red with context
                                    Auto-handle if no_pr / dep-gate
```

## What Should Trust More

- Auto-merging PRs with passing CI + approval
- Triage operations (dupe-close, archive, priority adjust)
- Retry/escalation for P2/P3 without confirmation
- Batch-unblocking tickets where the fix is mechanical (PR exists, dep cleared)

## What Hermes Should Never Do

- Merge PRs without both CI passing AND reviewer approval
- Use `delegate_task(acp_command="copilot")` — not governed; route through clawta instead
- Second-guess `_pick_driver` routing decisions
- Work around `kanban-flow` for board mutations — always use the substrate
- Leave blocked tickets without a `block_reason` — vague blocked states are the enemy

## Claude-Code Role (Invariant Owner)

Claude-code sits beside hermes, not under it: hermes runs the *flow*
(board engine, dispatch, P0/P1 execution), claude-code owns the
*prevention layer* that keeps the flow from re-litigating the same
defects. Hermes is throughput; claude-code is leverage.

### What Claude-Code Owns

- **The invariant layer.** Recurring bug classes — hash-before-parse,
  pointer aliasing, finalization lifecycle, schema-column defense,
  event-chain-on-timeout — get killed at the class level: a shared,
  named primitive plus an invariant test that walks the boundaries.
  Entrypoint: `/invariant`.
- **Adversarial review as a gate.** The Clawta findings taxonomy
  becomes typed lint/CI/pre-PR checks, so the adversarial pass runs
  *before* PR open, not as a comment after. Entrypoint: `/gate`
  (`/gate preflight` before `/land` or `gh pr create`).
- **Durable disposition.** PR/ticket verdicts are recorded once into a
  ledger `/queue` reads, so triage is O(new) not O(all). Entrypoint:
  `/verdict`.
- **Hard architecture & security.** Cross-session design calls,
  security-sensitive surfaces (gov, policy, governance), deep
  diagnosis where the cause is not obvious from one failed run.

### What Claude-Code Does Not Own

- **Flow.** Board engine, dispatch routing, worker spawning,
  finalization — hermes and openclaw own these; claude-code does not
  second-guess them.
- **Volume.** P2/P3 well-scoped work routes through clawta to the
  cheapest qualified worker — claude-code is not in that lane.
- **Architecture *direction*.** Claude-code designs and consults on
  hard calls; Red decides direction.
- **The merge button on `swarm/*` PRs** — operator-confirmed.

### Collaboration Contract (Claude-Code ↔ Hermes/Clawta)

1. **Hermes finds the same bug five times; claude-code ends it once.**
   A finding on 2+ PRs is a `/invariant` candidate, not five fixes.
2. **Every recurring finding becomes a gate.** What `/invariant`
   can't design out, `/gate` makes impossible to ship.
3. **Verdicts are durable.** A disposition stays authoritative until
   the PR head moves or someone runs `/verdict reopen` — no silent
   re-litigation across queue runs.
4. **Measured on bug *classes* eliminated, not PRs touched.** A
   60-bugs-fixed session is also evidence the prevention layer was
   missing.

### What Should Trust More

- Saying "rework" / "not yet" on a PR and having the `/verdict` stick.
- Initiating the upstream `/invariant` without waiting for it to be
  queued, when a class is clearly recurring.
- Setting `/gate` checks that block merge on the recurring findings.

## Board Paths

| Board | DB Path |
|-------|---------|
| chitin | `~/.hermes/kanban/boards/chitin/kanban.db` |
| readybench | `~/.hermes/kanban/boards/readybench/kanban.db` |
| personal-os | `~/.hermes/kanban/boards/personal-os/kanban.db` |

## Relationship to Operating Model

This document extends `docs/operating-model.md`. The topology and subsystem ownership remain as defined there. Hermes operates as the **kanban substrate** mentioned in the swarm entry: hermes owns the kanban truth layer, openclaw owns the workflow + agent runtime (via the gateway at `:18789` + `~/.local/bin/clawta`), and chitin owns the tick scripts, workflow definition, and chain/policy contracts that unify the hops.