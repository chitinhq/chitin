# Phase 1 Data Model: Agent-Runtime Cron Audit

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

This feature has no runtime data model — it ships no service. The "data model"
here is the structure of the **audit's own artifact**: the entities the
decision log is built from, the schema of one decision record, and the
lifecycle a job moves through.

## Entities

### Cron Job
One scheduled routine in an agent's cron registry.
- **id** — registry-assigned identifier (Hermes: 12-hex; OpenClaw: UUID).
- **name** — human label (not unique — `clawta-stale-worker-watchdog` and
  `kanban-pull-loop` each recur).
- **owning agent** — `hermes`, `clawta`, `glm-agent`, or OpenClaw-core.
- **runtime** — which registry holds it: `hermes` or `openclaw`.
- **schedule** — cron expression or interval.
- **enabled state** — `enabled` or `paused` (the pre-audit states).
- **last status** — `ok`, `error`, or none (never run).
- **prompt / script** — what the job does.

### Cron Registry
The per-runtime JSON file holding an agent's job set.
- `~/.hermes/cron/jobs.json` — 30 jobs.
- `~/.openclaw/cron/jobs.json` — 9 jobs.
- Carries timestamped `.bak-*` snapshots (audit evidence, not live jobs).

### Registration Source
Where a job is created from — determines whether a delete is durable.
- `manual` — operator-created; a registry-row delete is durable.
- `agent-managed` — (re)created by an agent startup/role path; the delete is
  durable only if that path is corrected too.

### Disposition
The audit's decision for one job — exactly one of:
`keep` · `disable` · `delete` · `migrate`. (See research.md Decision 1.)

### Decision-Log Record
One row of the audit artifact — the normalized form every job gets.

| Field | Notes |
|-------|-------|
| `job_id` | Cron Job id |
| `name` | job name |
| `owning_agent` | hermes / clawta / glm-agent / openclaw-core |
| `runtime` | `hermes` or `openclaw` |
| `schedule` | cron/interval expression |
| `enabled_state` | `enabled` or `paused` at audit time |
| `last_status` | `ok` / `error` / `none` |
| `description` | plain-language: what the job does |
| `concern` | `swarm-infra` or `personal` — the FR-013 separation of concern |
| `registration_source` | `manual` or `agent-managed` (+ the path, if managed) |
| `disposition` | `keep` / `disable` / `delete` / `migrate` |
| `rationale` | why this disposition (required, non-empty) |
| `replacement_workflow` | required iff `disposition = migrate`; the spec-070/076 workflow |
| `spec_081_crossref` | set iff the job also appears in spec 081's layer |
| `execution_phase` | `A` (none) / `B` (zero-regret cull) / `C` (gated retirement) |

### Replacement-Workflow Mapping
For a `migrate` job — the spec-070/076 orchestrator workflow that subsumes it,
and its proven/unproven status (the Phase C gate).

## Job lifecycle state machine

Pre-audit states are `enabled` and `paused`. The audit drives every job to one
of three **terminal** states (INV-001):

```text
            ┌─────────┐  operator pause   ┌────────┐
            │ enabled │ ───────────────▶  │ paused │
            │         │ ◀───────────────  │        │
            └────┬────┘     resume        └───┬────┘
                 │                            │
   keep ─────────┘ (stays enabled)            │
                 │                            │
                 ▼          disposition: disable, or migrate step 1
            ┌──────────┐ ◀──────────────────────────────────────┐
            │ disabled │                                          │
            └────┬─────┘                                          │
                 │  disposition: delete (after disable),          │
                 │  or migrate step 2 (replacement proven)        │
                 ▼                                                │
            ┌─────────┐                                           │
            │ deleted │   registration removed + source corrected │
            └─────────┘                                           │
```

Disposition → terminal state:
- `keep` → **enabled** (unchanged; behavior preserved, INV-004).
- `disable` → **disabled**.
- `delete` → **deleted** (always via `disabled` first — FR-011).
- `migrate` → **disabled** when the replacement workflow is proven, then
  **deleted**; never deleted into a coverage gap (FR-004, INV-002).

## Validation rules

- Every record has a non-empty `rationale` (FR-003) and a `concern` (FR-013).
- `replacement_workflow` is present **iff** `disposition = migrate` (FR-004).
- A `delete` record's job must reach `deleted` only after passing the
  restart-durability check (FR-005, SC-004).
- Records for the same logical job across runtimes/layers
  (`kanban-pull-loop`, `clawta-poller`) must carry a consistent direction and a
  `spec_081_crossref` where applicable (FR-012, SC-007).
- The 39 records partition cleanly: every job appears exactly once; the four
  `clawta-stale-worker-watchdog` registrations are four distinct records that
  resolve to one `keep`/`migrate` + three `delete` (FR-006).
