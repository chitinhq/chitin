# Implementation Plan: Agent-Runtime Cron Audit

**Branch**: `082-agent-cron-audit` | **Date**: 2026-05-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/082-agent-cron-audit/spec.md`

## Summary

Audit and rationalize the 39 agent-runtime cron jobs — 30 in the Hermes
registry, 9 in the OpenClaw registry. The deliverable is a documented
**decision log** assigning every job exactly one disposition (keep / disable /
delete / migrate), followed by **staged execution**: the zero-regret cull
first (collapse the 4× `clawta-stale-worker-watchdog`, disable the
perpetually-failing `board-watchdog`), then the **gated retirement** of the
crons the spec-070 orchestrator replaces. No new service and no build logic —
this is registration-metadata cleanup, gated on behavior preservation.

## Technical Context

**Language/Version**: None — this feature ships no code. The work is JSON-registry edits plus the agents' existing cron CLIs.

**Primary Dependencies**: The Hermes cron subsystem (`hermes cron` CLI; `~/.hermes/cron/jobs.json`) and the OpenClaw cron subsystem (`~/.openclaw/cron/jobs.json`). The spec-070 orchestrator / spec-076 spec-DAG scheduler are the migration *target* — consumed as a gate, not built here.

**Storage**: Two JSON registries — `~/.hermes/cron/jobs.json` and `~/.openclaw/cron/jobs.json`. Run history under `~/.hermes/cron/output/` and `~/.openclaw/cron/runs/` is read-only audit evidence.

**Testing**: The spec's acceptance scenarios, verified against the live registries — `hermes cron list`, registry inspection, and an agent-restart durability check. No unit-test framework (no new code).

**Target Platform**: Linux, single box (chimera-ant), self-hosted.

**Project Type**: Operations / audit — a registration-metadata change, not a software artifact.

**Performance Goals**: N/A — 39 jobs, one operator, low cadence.

**Constraints**: Registries are edited while their agents may be running, so every mutation is backup-first and restart-safe. No logical job runs from both a cron and an orchestrator workflow at once. The behavior of any `keep` job is unchanged.

**Scale/Scope**: 39 jobs across 2 registries — ~17 orchestrator/dispatch-superseded, 9 personal career, ~13 other (reports, canaries, retro, OpenClaw core).

## Constitution Check

*GATE: must pass before Phase 0. Re-checked after Phase 1.*

| Principle | Assessment |
|-----------|------------|
| §1 Side-effect boundary | PASS — the audit reads cron/registry state and edits cron registrations through the agents' own cron subsystems; it never writes chain events or bypasses hermes/openclaw. |
| §2 Workers + worktrees | PASS — the spec artifacts ship via PR from a worktree; the *executable* work (registry edits) happens outside any git repo, in `~/.hermes`/`~/.openclaw`, so §2's git-checkout worktree rule is structurally inapplicable to it. This spec dispatches no agent code-work. |
| §3 Spec-kit gate | PASS — 082 has `spec.md` + this `plan.md`; `tasks.md` follows. |
| §4 Tracked installers | PASS — the audit removes and disables jobs; it adds no new operator script. The failing `board-watchdog` is dispositioned `disable` (it is orchestrator-superseded), incurring no §4 installer obligation; any future fix to a *kept* script carries its own installer in its own PR. |
| §5 Board-aware scripts | PASS — no board-touching script is added or modified. |
| §6 Swarm tooling exception | PASS — no new tooling; the audit only prunes existing registrations. |

No violations → Complexity Tracking is empty.

## Project Structure

### Documentation (this feature)

```text
specs/082-agent-cron-audit/
├── spec.md
├── plan.md                  # This file
├── research.md              # Phase 0 — audit method, classification scheme, preliminary buckets
├── data-model.md            # Phase 1 — entities, decision-record schema, job state machine
├── quickstart.md            # Phase 1 — the safe-mutation runbook
├── tasks.md                 # Phase 2 — /speckit-tasks output
├── decision-log.md          # US1 output — the 39-job classified decision log
└── checklists/requirements.md
```

`contracts/` is intentionally omitted — this feature exposes no external
interface; the one schema it defines (the decision-log record) is captured in
`data-model.md`.

### Affected surfaces (no repository source code)

This feature ships no code under the repo. It mutates two runtime registries
and produces spec-kit docs:

```text
~/.hermes/cron/jobs.json      # 30 jobs — the cull + retirements edit it
~/.openclaw/cron/jobs.json    # 9 jobs  — the de-dup edits it
<registration sources>        # agent startup/role configs — corrected so deletions are durable
specs/082-agent-cron-audit/decision-log.md   # the decision log — the audit's primary artifact (US1)
```

**Structure Decision**: An operations spec — no `src/` tree. The deliverables
are the decision log and the staged registry mutations; the only repository
footprint is `specs/082-agent-cron-audit/`.

## Execution Phases

The spec's three user stories map to staged, independently-shippable phases.

- **Phase A — Decision log (US1).** Read both registries; for each of the 39
  jobs record id / name / agent / schedule / state / last-status /
  description, its **registration source**, and one disposition + rationale.
  Map every `migrate` job to its spec-070/076 replacement workflow. Reconcile
  with spec 081. Output: the complete decision log in `research.md`. **Zero
  registry mutation.**
- **Phase B — Zero-regret cull (US2).** Back up both registries. Collapse the
  4× `clawta-stale-worker-watchdog` to one and correct the `glm-agent`
  registration source so the duplicates do not return. Disable the failing
  `board-watchdog`. Execute any other unconditionally-safe `delete`
  dispositions. Verify durability across an agent restart.
- **Phase C — Gated orchestrator retirement (US3).** For each `migrate` job,
  confirm its replacement workflow is proven, then disable the cron in the
  same change — never delete into a coverage gap. Honor the spec-081
  reconciliation so no job double-runs.

Phase order is strict (A → B → C). Each ships independently and is reversible:
`disable` precedes `delete`, and every registry is backed up before mutation.

## Complexity Tracking

None — no constitution violations to justify.
