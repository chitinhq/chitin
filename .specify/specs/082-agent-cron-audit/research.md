# Phase 0 Research: Agent-Runtime Cron Audit

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

This spec has no `NEEDS CLARIFICATION` items — the feature description supplied
the inventory and the defects. Phase 0 therefore fixes the **audit method**:
how each job is classified, how a registration source is found, how a registry
is mutated safely, and how a migration is gated. The full per-job decision log
is produced by US1 (Phase A); the buckets below are a preliminary pass.

## Decision 1 — The four-disposition classification scheme

Every job is assigned exactly one disposition. The decision rule:

- **keep** — the job's work is still wanted, has no orchestrator successor, and
  the job is healthy. (Personal career checkpoints; OpenClaw-core memory.)
- **disable** — the work is no longer wanted *or* the job is broken, but the
  registration is left in place (paused/disabled) rather than removed — used
  when a record is still useful or removal needs a follow-up. Reversible.
- **delete** — the job and its registration are removed outright; the work is
  dead (duplicates, jobs whose subject no longer exists).
- **migrate** — the work is still wanted but belongs in a spec-070/076
  orchestrator workflow; the cron is retired *after* that workflow is proven.

**Rationale**: four states are the minimum that separate "still wanted" from
"broken" from "dead" from "moving" — the distinctions that drive different
actions. `paused` is **not** a disposition (INV-001) — a paused job is
undecided and still gets one of the four.

**Alternatives considered**: a binary keep/kill — rejected: it cannot express
the gated "migrate" path and would force premature deletion into coverage gaps.

## Decision 2 — Registration-source discovery (durable deletion)

A deletion that only edits the registry row is not durable: the OpenClaw
registry already shows `clawta-stale-worker-watchdog` registered **three extra
times under `glm-agent`**, the signature of an agent re-registering its crons
on every start. For each job US1 must determine the source:

- **manual** — created by an operator `hermes cron create` (or equivalent); a
  registry-row delete is durable.
- **agent-managed** — (re)created by an agent startup / role / init path; the
  delete is durable only if that path is also corrected.

Method: inspect agent startup and role configuration for cron-registration
calls; correlate `created_at` clustering and duplicate `name`/`agentId` tuples.
The triplicated watchdog is the worked example — its source must be found and
corrected, not just its rows deleted (FR-005, FR-006, SC-004).

## Decision 3 — Safe-mutation protocol

Registries are live files edited while agents may run. Every mutation follows:

1. **Snapshot** the registry to a timestamped `.bak` before any edit (FR-011).
2. **Disable before delete** — set the job inactive, observe one scheduled
   cycle, then remove it. Never delete an enabled job in one step.
3. **Restart-durability check** — after a delete, restart the owning agent and
   re-list; a job that reappears means the registration source was missed
   (Decision 2). Loop until it stays gone.
4. **Rollback** is restoring the `.bak` snapshot.

See [quickstart.md](./quickstart.md) for the step-by-step runbook.

## Decision 4 — Orchestrator-mapping and the "proven" gate

A `migrate` job is retired only when its replacement is **proven**. "Proven"
borrows spec 081's definition: the replacement orchestrator workflow has run
beside the cron and demonstrably produced the same outcome — at which point the
cron is disabled in the *same* change that confirms the workflow owns the work
(no window where both or neither run; FR-009, INV-002). Mapping targets:

- Board / dispatch / watchdog crons → spec-070 Phase 1 (spec-076 scheduler)
  and Phase 3 (pollers/watchdogs as scheduled workflows).
- Periodic report / canary crons → the Temporal **Schedule-backed** pattern
  spec 081 US2 establishes for read-mostly jobs.

Until a target workflow exists, the job stays `migrate`-pending — never
deleted (FR-004, US3 AS2).

## Decision 5 — Reconciliation with spec 081

Spec 081 audits the ~15 **systemd** cron/timer jobs; this spec audits the
**agent-runtime** jobs. A few logical jobs surface in both layers
(`kanban-pull-loop` runs as a Hermes cron *and* an OpenClaw cron; `clawta-poller`
appears as a systemd job in 081 and as `clawta-kanban-poller` here). For every
such job the two specs MUST assign the same direction and never leave it
running from two layers at once (FR-012, SC-007). The audit cross-checks 081's
job inventory before finalizing any shared job's disposition.

## Preliminary classification buckets

Direction only — US1 confirms each job's registration source and finalizes the
per-job rationale.

**Hermes (30):**

| Bucket | Count | Jobs | Expected direction |
|--------|-------|------|--------------------|
| Board / dispatch orchestration | 14 | board-watchdog, board-audit, autonomous-board-engine, hermes-clawta-bridge, blocked-ticket-digest, readybench-poller, swarm-standup, swarm-controller-loop, swarm-invoker, icarus-watcher, board-grooming-loop, research-intake, kanban-pull-loop, chitin-watchdog | **migrate** (retire as spec-070/076 workflows prove out); `board-watchdog` **disable now** (failing every run) |
| Career checkpoints | 9 | anthropic-fde-checkpoint, glean-fde-checkpoint, glean-fde-cp2…cp8 | **keep** — personal, one-shot, self-expiring; out of swarm scope |
| Report generation | 3 | industry-scan, doc-sync, chain-summary | **migrate or delete** — paused; tie to spec-081 read-mostly pattern / board-retirement |
| Governance / health canaries | 3 | chain-governance-canary, chitin-canary, chitin-audit | **keep**, then **migrate** to a Schedule-backed workflow |
| Weekly retro | 1 | chitin-weekly-retro | **keep** or **migrate** |

**OpenClaw (9):**

| Bucket | Count | Jobs | Expected direction |
|--------|-------|------|--------------------|
| OpenClaw core | 1 | Memory Dreaming Promotion | **keep** — an OpenClaw memory feature, not swarm orchestration |
| Clawta dispatch | 5 | clawta-icarus-board-watcher, clawta-kanban-poller, clawta-blocked-escalator, clawta-stale-worker-watchdog (clawta), kanban-pull-loop (clawta) | **migrate** — spec-070 dispatch pipeline |
| Duplicate registrations | 3 | clawta-stale-worker-watchdog ×3 under `glm-agent` | **delete** — de-dup; correct the registration source |

## Open items for US1

- Confirm exact `hermes cron` and OpenClaw cron CLI subcommands for
  disable/delete (registry inspection vs CLI).
- Confirm the registration source of the `glm-agent` watchdog triplicate.
- Confirm which spec-070 workflows are already **proven** vs still Draft (only
  proven ones gate a Phase C deletion).
