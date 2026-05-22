# Decision Log: Agent-Runtime Cron Audit

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Tasks**: [tasks.md](./tasks.md) (US1 — T006–T014)

**Audit snapshot**: 2026-05-21. Counts re-confirm at execution time before any
mutation (spec Assumptions). This is the audit's primary artifact (FR-010) —
**operator-reviewed**; US2/US3 execute only the dispositions the operator
approves.

## Summary

| Disposition | Count | Execution phase |
|-------------|-------|-----------------|
| keep        | 14    | A — no action |
| disable     | 4     | B — zero-regret cull |
| delete      | 9     | B — zero-regret cull |
| migrate     | 12    | C — gated orchestrator retirement |
| **Total**   | **39** | 30 Hermes + 9 OpenClaw |

Partition verified: every job appears in exactly one row below; 14+4+9+12 = 39
(INV-001).

> **Revised 2026-05-22** — `delete` 3→9, `migrate` 18→12: the six Hermes
> board-machinery crons are reclassified `migrate → delete` because the kanban
> board read-model they operate on is **already retired** (PR #903, merged).
> See Finding 5.

`registration_source` is a best determination from the snapshot; entries
marked *(confirm)* are verified during T009/T016 before any deletion.

---

## KEEP — 14

### Personal — career tracking (9, Hermes) · phase A

Out of swarm scope (FR-008). Self-limiting: `glean-fde-*` are one-shot
(`repeat.times: 1`); `anthropic-fde-checkpoint` is date-gated in its own prompt.
No migration — these are not orchestration.

| Job | ID | Schedule | State | Registration |
|-----|----|----------|-------|--------------|
| anthropic-fde-checkpoint | cc93a04f75e9 | `0 9 23,28,4,11 5,6 *` | paused | manual |
| glean-fde-checkpoint | 57e577f36f60 | `0 9 25 5 *` (one-shot) | paused | manual |
| glean-fde-cp2 | bd16bae65d91 | `0 9 1 6 *` (one-shot) | enabled | manual |
| glean-fde-cp3 | b80d3bd2ade4 | `0 9 8 6 *` (one-shot) | enabled | manual |
| glean-fde-cp4 | 9c85fa004bed | `0 9 15 6 *` (one-shot) | enabled | manual |
| glean-fde-cp5 | 695dee5e1c16 | `0 9 22 6 *` (one-shot) | enabled | manual |
| glean-fde-cp6 | a2f4d991ee12 | `0 9 29 6 *` (one-shot) | enabled | manual |
| glean-fde-cp7 | bf57bdffec7c | `0 9 6 7 *` (one-shot) | enabled | manual |
| glean-fde-cp8 | 2506ac291f7c | `0 9 13 7 *` (one-shot) | enabled | manual |

*Rationale*: personal career-checkpoint reminders on the operator's `career`
board; not swarm infrastructure, healthy, and self-expiring — keep, no action.
*Note*: `glean-fde-checkpoint` (the umbrella) is superseded by the `cp2..cp8`
cadence and is paused + one-shot → effectively already retired; an operator may
`delete` it, but it self-expires either way.

### Swarm-infra — governance / health checks (4, Hermes) · phase A

| Job | ID | Schedule | State·Last | Registration |
|-----|----|----------|-----------|--------------|
| chain-governance-canary | 3bed5c65952a | `0 9 * * *` | enabled · ok | manual |
| chitin-canary | 475e5a5fdb0a | `0 9 * * *` | enabled · ok | manual |
| chitin-audit | b6835a1bc3dd | `0 9 * * *` | enabled · ok | manual |
| chitin-weekly-retro | d42335a29c57 | `0 10 * * 1` | enabled · never run | manual |

*Rationale*: healthy, low-cost governance/health checks with no spec-070/076
orchestration successor — keep. **Future Schedule-backed migration candidates**
(spec-081 US2 read-mostly pattern), but not `migrate` today (no named
replacement workflow exists). See Finding 1 — these three chain canaries
overlap and should be consolidated to one before any Schedule migration.

### OpenClaw core (1) · phase A

| Job | ID | Schedule | State | Registration |
|-----|----|----------|-------|--------------|
| Memory Dreaming Promotion | 708b57d3-9e85-48f5-8602-f02ca3bd6a5b | `0 3 * * *` | enabled | agent-managed (`memory-core.short-term-promotion`) |

*Rationale*: an OpenClaw-core memory feature (`[managed-by=memory-core]`), not
swarm orchestration — keep, out of audit scope for migration.

---

## DISABLE — 4 (Hermes) · phase B · concern swarm-infra

| Job | ID | Schedule | State·Last | Registration | Rationale |
|-----|----|----------|-----------|--------------|-----------|
| board-watchdog | 388e38b20bd5 | every 60m | enabled · **error** | manual | Fails **every run** (`sqlite3: no such column block_reason`, 845 cycles). Orchestrator-superseded *and* locally superseded by the newer `chitin-watchdog` (created 2026-05-20). Disable now — not worth fixing (FR-007; plan.md §4). |
| industry-scan | 06430879caa7 | `0 9 * * 1` | paused | manual | Wiki/arXiv research report; operator-paused 2026-05-20. No 070/076 successor. Disable to formalize the pause; revisit as a Schedule-backed job if wanted. ⚠️ name/prompt swapped — see Finding 2. |
| doc-sync | 009d8a1dfa23 | `0 9 * * *` | paused · error | manual | Wiki doc-sync report; operator-paused, last run truncation-errored. Overlaps `industry-scan` (Finding 2). Disable; revisit/consolidate as a Schedule-backed job. |
| chain-summary | 65b3b4c43863 | `0 8,20 * * *` | paused | manual | Governance-chain HTML report; operator-paused 2026-05-20. No 070/076 successor. Disable; future Schedule-backed candidate. |

---

## DELETE — 9 · phase B · concern swarm-infra

### OpenClaw — duplicate registrations (3)

| Job | ID | Owning agent | Schedule | Registration | Rationale |
|-----|----|----|----------|--------------|-----------|
| clawta-stale-worker-watchdog | 3c0a957a-96a4-481d-b9c8-96103f3ca994 | glm-agent | every 600s | agent-managed — glm-agent re-registration *(confirm — T009)* | Duplicate of the `clawta`-owned watchdog (6a373b88). |
| clawta-stale-worker-watchdog | cff16422-79ff-422b-81f9-2e05da1162a1 | glm-agent | every 600s | agent-managed — glm-agent re-registration *(confirm)* | Duplicate (3rd copy). |
| clawta-stale-worker-watchdog | 0bca6534-7429-41fd-bbe7-2695919c1765 | glm-agent | every 600s | agent-managed — glm-agent re-registration *(confirm)* | Duplicate (4th copy). |

*Rationale*: `clawta-stale-worker-watchdog` is registered **4×** — once under
`clawta` (kept, see MIGRATE) and three times under `glm-agent`. The triplication
is the signature of an agent re-registering on start (research.md Decision 2).
Delete all three `glm-agent` copies **and correct the registration source** so
they do not return (FR-006, SC-004) — a registry-row delete alone is not
durable. Collapses 4 → 1. *(Executed 2026-05-21 — see tasks.md T015; the
registration source self-resolves: PR #908 `git rm`s `install-clawta-poller.sh`,
Finding 5.)*

### Hermes — board-machinery, subject retired (6)

These jobs operate on the Hermes kanban board read-model, which is **already
retired** (spec 081 US1, PR #903 merged). Their subject is gone — this is
dead-work `delete` (research.md Decision 1), not `migrate`. `autonomous-board-engine`
is additionally named in spec 070 FR-016 as explicitly "retired, not migrated".

| Job | ID | Schedule | State·Last | Rationale |
|-----|----|----------|-----------|-----------|
| board-audit | 84f4226bbdde | `0 9 * * *` | enabled · ok | Audits the kanban board read-model — retired by PR #903. |
| autonomous-board-engine | b23a453ab782 | every 30m | paused | Promotes/demotes board tickets; 070 FR-016 retires the board-engine outright. |
| board-grooming-loop | 00defe1cec84 | every 240m | enabled · ok | Grooms board tickets — board retired. |
| blocked-ticket-digest | ad2fc9492509 | `0 9 * * *` | enabled · ok | Reports blocked board tickets — board retired; the orchestrator surfaces stalled state natively (076 FR-016). |
| research-intake | dcc44c684fa2 | every 480m | enabled · ok | Files/grooms kanban tickets — board retired. |
| chitin-watchdog | 78009d89c78e | every 60m | enabled · ok | Reads the kanban DB for stuck tickets — board retired. |

---

## MIGRATE — 12 · phase C · concern swarm-infra

Retire each only once its replacement workflow is *proven* (research.md
Decision 4); never delete into a coverage gap (FR-004, US3 AS2). Most are
already paused — they are not running today, but the registration stays until
the replacement is proven.

### Hermes (7)

| Job | ID | Schedule | State·Last | Replacement workflow | spec 081 |
|-----|----|----------|-----------|----------------------|----------|
| kanban-pull-loop | f516ba8e2fd5 | every 10m | paused · error | spec-076 spec-DAG scheduler (replaces the pull-loop) | ✓ pull-loop |
| hermes-clawta-bridge | 8544ef19b897 | every 30m | paused | 070 Phase 2 dispatch pipeline | — |
| readybench-poller | ea11e28b814f | every 30m | paused | 070 Phase 2 dispatch / 076 multi-repo (076 US3) | ✓ clawta-poller — exec'd by PR #908 |
| swarm-invoker | 631b09c41925 | every 5m | paused | 070 Phase 2 dispatch | — |
| swarm-controller-loop | 39533a2ea1eb | every 1m | paused | 070 Phase 2 dispatch (controller) | — |
| icarus-watcher | 31673f20e7e0 | every 5m | paused | 070 Phase 3 poller (icarus lane) | — |
| swarm-standup | 4f55777ee280 | `0 9 * * 1-5` | paused · delivery-error | 070 telemetry / a Schedule-backed report | — |

### OpenClaw (5)

| Job | ID | Owning agent | Schedule | Replacement workflow | spec 081 |
|-----|----|----|----------|----------------------|----------|
| clawta-kanban-poller | c007f451-be69-45aa-aed1-b024e08d06e7 | clawta | every 120s | 070 Phase 2 dispatch pipeline | ✓ clawta-poller |
| clawta-blocked-escalator | 61048f9a-2516-4c09-a08f-fd8b8a3e2219 | clawta | every 600s | spec-076 stalled/blocked surfacing | — |
| clawta-stale-worker-watchdog | 6a373b88-3ca5-49d8-9e42-b4b3f2d7de5e | clawta | every 600s | 070 Phase 3 watchdog workflow | — |
| clawta-icarus-board-watcher | 4e4b58fe-fbb0-4314-b8de-740010424e54 | clawta | every 60s | 070 Phase 3 poller (icarus lane) | — |
| kanban-pull-loop | d6e8c612-10e8-4448-ab61-18591904fbfd | clawta | every 600s | spec-076 spec-DAG scheduler | ✓ pull-loop |

*Rationale (bucket)*: every job here is part of the human-managed kanban
pull-loop / dispatch machinery that spec 070 (and its P1 slice, spec 076) was
built to replace. The `clawta`-owned `clawta-stale-worker-watchdog` (6a373b88)
is the **one surviving instance** the DELETE bucket collapses the triplicate
down to. Registration source for the OpenClaw `clawta` jobs is *agent-managed
(suspected)* — confirmed in T009/T020 before retirement.

---

## Cross-cutting findings

**Finding 1 — three overlapping chain-health crons.** `chain-governance-canary`
is a standalone cron *and* the script `chitin-canary` runs *and* a step inside
`chitin-audit`. Three crons, one underlying check. Consolidate to one before any
Schedule-backed migration (kept as `keep` meanwhile).

**Finding 2 — `industry-scan` / `doc-sync` name↔prompt swap.** In the Hermes
registry, job `06430879caa7` is *named* `industry-scan` but its prompt is the
doc-sync wiki update; job `009d8a1dfa23` is *named* `doc-sync` but its prompt is
the industry-scan pipeline. The names are swapped relative to their work. Both
are being disabled, so this is recorded, not fixed — but any future re-enable
must correct the swap first.

**Finding 3 — `board-watchdog` superseded twice.** `board-watchdog` (388e…,
broken) is superseded both by the orchestrator and, locally, by the newer
`chitin-watchdog` (78009…, created 2026-05-20, healthy). Disabling it loses no
coverage.

**Finding 4 — `kanban-pull-loop` is dual-registered.** It exists as a Hermes
cron (Ares) and an OpenClaw cron (Clawta) — two registrations of one logical
pull-loop. Both are dispositioned `migrate` → spec-076; spec-081 cross-ref
applies. Reconcile at execution so neither double-runs against the scheduler
(FR-009, SC-007).

**Finding 5 — spec 081 + the orchestrator are already executing this layer.**
Re-checked 2026-05-22 against the merged/open PR set:

- The kanban **board read-model is already retired** (PR #903, merged) — which
  is why the six Hermes board-machinery crons above are `delete`, not `migrate`.
- `clawta-poller` retirement is in-flight as **PR #908**, which `git rm`s
  `swarm/bin/install-clawta-poller.sh`. The installer-idempotency bug behind the
  triplicated watchdog (research.md Decision 2) is therefore **moot** — that file
  is being deleted; it must not be separately "fixed". SC-004 durability for the
  watchdog dups is resolved by #908.
- spec-081 US3 systemd watchdog/mutation/ops crons are in-flight as **PR #907**.
- Both #907 and #908 were *delivered by the Chitin Orchestrator itself* — it is
  already consuming a task DAG, dispatching work units, and opening PRs.

**Scope consequence**: spec 081 owns the systemd layer + `clawta-poller`; spec
082's non-duplicated residue is the Hermes board/dispatch crons, the career
crons, the canaries, and the OpenClaw registration sweep. The poller `migrate`
jobs whose scripts #908 removes (`readybench-poller`, the clawta-poller family)
are *executed* by spec 081 — spec 082 only sweeps the orphaned agent-cron
*registrations* once #908 lands.

**Out of scope**: both registries carry many `jobs.json.bak-*` snapshots —
noted, not actioned (spec Out of Scope).

## Verification (T013 / T014)

- 39 records, each in exactly one disposition section — clean partition (INV-001). ✓
- Every record has id, name, owning agent, schedule, state, registration source, disposition, rationale, concern (section-level), execution phase. ✓
- Every `migrate` record names a replacement workflow (FR-004). ✓
- `concern`: 9 personal, 30 swarm-infra (incl. 1 OpenClaw-core). ✓
- Open for execution: confirm `registration_source` *(confirm)* entries (T009/T016); confirm which replacement workflows are *proven* (T020).
