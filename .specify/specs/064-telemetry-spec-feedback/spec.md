# 064 — Telemetry-spec feedback loop (L6)

> Layer 6 of the chitin SDD platform. Sentinel-mined invariants loop back
> into spec governance as typed proposals. Hard operator review gate — no
> proposal is ever auto-applied (R3, absolute).

## Ticket refs

- Parent: chitin spec 060 (SDD platform charter)
- Blocked on: spec 062 (invariant attribution — per-spec provenance for
  sentinel findings), spec 063 (replayable builds — reproducible build
  evidence grounding)
- Slice implementation: t_612b362e (S1), t_dcdbd97d (S2), t_18ba5a7a (S3)

## File-system scope

Worker MAY write under:

- `python/analysis/proposals/` (new — proposal types, queue, review logic)
- `python/analysis/proposals/models.py` (SpecAmendment, DispatchPolicyUpdate)
- `python/analysis/proposals/queue.py` (proposal queue, state machine)
- `python/analysis/proposals/review.py` (operator review gate, audit log)
- `python/analysis/proposals/versioned_policy.py` (immutable policy versions)
- `python/analysis/proposals/regression.py` (post-amendment regression detection)
- `python/analysis/proposals/self_telemetry.py` (loop self-telemetry)
- `python/analysis/tests/test_proposals_*.py` (test files)
- `python/analysis/sentinel.py` (amend promotion_metadata to carry attribution)
- `.specify/specs/064-telemetry-spec-feedback/**`

Worker MUST NOT write under:

- `go/**` (kernel — not a kernel feature)
- `swarm/**` (dispatch — not a dispatch feature)
- Any path not listed above without a spec amendment.

## Goal

Close the learning flywheel: sentinel-mined invariants that pass an
evidence threshold become typed `SpecAmendment` or
`DispatchPolicyUpdate` proposals attributed to the spec and build that
produced them. Proposals land in an operator review queue. No proposal
is ever auto-applied. After operator approval, amendments produce new
immutable policy versions. The loop self-monitors: throughput, review
latency, approval rates, post-amendment regression detection.

## Dependency on specs 062 and 063

This spec IS blocked on specs 062 and 063 landing first:

- **Spec 062 (invariant attribution)** defines per-spec provenance for
  sentinel findings. The `SpecAmendment` type in this spec (064) carries
  an `attribution.spec_provenance` field referencing 062's attribution
  shape. Until 062 lands, the field is typed as a forward-reference
  placeholder (string — `spec:062-attribution TBD`).
- **Spec 063 (replayable builds)** defines reproducible build evidence
  that grounds each proposal. The `evidence.build_grounding` field in
  this spec references 063's build-identity shape. Until 063 lands, the
  field is typed as a forward-reference placeholder.

Both dependencies will be resolved when the respective specs ship. The
064 spec is written to be implementable against the placeholder types
now, with a straightforward replacement when 062/063 land.

## Resolved open questions

### Q1: Minimum evidence threshold for invariant promotion

**Decision: configurable, default 5, with a minimum floor of 3.**

Rationale:

1. A fixed threshold of 5 is reasonable for a mature operator box
   accumulating 7+ days of decisions (the sentinel's default `--window
   7d`). The sentinel's `detect_patterns` already groups by
   `(rule_id, action_type, agent_id)` and ranks by count — a pattern
   that appears 5 times in 7 days of denies is genuinely recurring.
2. However, a hard-coded threshold of 5 is too aggressive for new
   installations or narrow windows (e.g., `--window 24h`). On a fresh
   box with only hours of history, requiring 5 repetitions could
   suppress legitimate findings that only appeared 2-3 times.
3. Conversely, a floor of 3 prevents noise from one-off deny spikes
   (a single bad session should not produce a proposal) while still
   allowing promotion on tighter windows.
4. The threshold is therefore **configurable** via
   `chitin.yaml` → `sentinel.promotion_threshold` (default: 5,
   minimum enforced: 3). Values below 3 are clamped with a warning.

Implementation: sentinel's `_promotion_metadata` gains a
`min_evidence_threshold` parameter (default 5). Proposals with
`pattern.count < min_evidence_threshold` are emitted but flagged
`status: "below-threshold"`. Only proposals at or above threshold
enter the review queue.

### Q2: Whether a delegated agent may triage the proposal queue

**Decision: triage yes, approval absolutely no. The boundary is the
`proposed → approved` state transition.**

Rationale:

1. R3 is absolute: no proposal is ever auto-applied. "Auto-applied"
   includes delegation to an agent — the spirit of R3 is that a human
   operator must make the final call on every state change that moves a
   proposal toward active policy.
2. However, triage (sorting, prioritizing, tagging, adding reviewer
   notes, marking "ready-for-review", grouping by spec, deduplicating)
   does not advance a proposal toward application. It is
   observation-only bookkeeping, which agents already do in the
   analysis/ layer.
3. The boundary is therefore the `proposed → approved` transition:
   — **Agent may**: change `priority` field, add `reviewer_notes`,
     merge duplicates into a single proposal, mark `status: ready-for-
     review`, attach `attribution` metadata, suggest `kind`
     classification (SpecAmendment vs DispatchPolicyUpdate).
   — **Agent MUST NOT**: transition `status` to `approved` or
     `applied`. These transitions require an explicit human operator
     action (keyboard input from a terminal, or explicit `chitin
     proposal approve <id>` command).
4. Enforcement: the state machine in `queue.py` encodes this as a hard
   constraint. Any code path that transitions a proposal to `approved`
   or `applied` MUST pass through `review.py:operator_approve()`,
   which requires a human-verified action token. No agent, cron, or
   sentinel call path may reach `approved` without this token.

## Architectural overview

```
  Sentinel (analysis/sentinel.py)
      │
      │  detect_patterns → draft_for_pattern → _promotion_metadata
      │  (existing; emits candidate invariants)
      ▼
  ┌─────────────────────────────────────────────────────────────────┐
  │  Proposal queue (python/analysis/proposals/)                    │
  │                                                                  │
  │  SpecAmendment                   DispatchPolicyUpdate           │
  │  ├─ attribution                  ├─ attribution                 │
  │  │  ├─ spec_provenance (062)     │  ├─ spec_provenance (062)    │
  │  │  └─ sentinel_source          │  └─ sentinel_source          │
  │  ├─ evidence                     ├─ evidence                    │
  │  │  └─ build_grounding (063)     │  └─ build_grounding (063)    │
  │  ├─ kind: "spec-amendment"        ├─ kind: "dispatch-policy"     │
  │  ├─ threshold_status             ├─ threshold_status            │
  │  ├─ priority          ⟵ agent   ├─ priority        ⟵ agent     │
  │  ├─ reviewer_notes    ⟵ agent   ├─ reviewer_notes  ⟵ agent     │
  │  └─ status                       └─ status                      │
  │     ├─ proposed  (initial)          ├─ proposed  (initial)        │
  │     ├─ ready-for-review ⟵ agent     ├─ ready-for-review ⟵ agent │
  │     ├─ approved  ⟵ OPERATOR ONLY    ├─ approved ⟵ OPERATOR ONLY │
  │     ├─ rejected  ⟵ OPERATOR ONLY     ├─ rejected ⟵ OPERATOR ONLY│
  │     └─ applied  ⟵ OPERATOR ONLY      └─ applied  ⟵ OPERATOR ONLY│
  │                                                                  │
  │  State machine enforces: proposed → ready-for-review → approved  │
  │                                    → approved → applied          │
  │  Skip transitions are forbidden.                                 │
  │  No agent path reaches approved/applied.                       │
  │                                                                  │
  │  Audit log: every state transition recorded with actor + ts.    │
  └─────────────────────────────────────────────────────────────────┘
      │
      │  operator approve → versioned_policy.apply()
      ▼
  ┌─────────────────────────────────────────────────────────────────┐
  │  Versioned policy (versioned_policy.py)                          │
  │                                                                  │
  │  - Immutable policy versions (append-only log)                  │
  │  - Each version references the approved proposal id             │
  │  - Rollback = creating a new version that restores old state    │
  │  - Policy versions are never mutated in-place                   │
  └─────────────────────────────────────────────────────────────────┘
      │
      │  after application, regression monitor observes
      ▼
  ┌─────────────────────────────────────────────────────────────────┐
  │  Regression detection (regression.py)                            │
  │                                                                  │
  │  - Post-amendment: observe N subsequent sentinel runs           │
  │  - If affected invariant's deny count INCREASES → flag for      │
  │    operator re-review                                            │
  │  - Stability metric: "invariant held steady for K runs"        │
  └─────────────────────────────────────────────────────────────────┘
      │
      │  self-telemetry
      ▼
  ┌─────────────────────────────────────────────────────────────────┐
  │  Loop self-telemetry (self_telemetry.py)                         │
  │                                                                  │
  │  - Proposal throughput (proposals / window)                     │
  │  - Approval rate (% approved of total)                          │
  │  - Time-to-review distribution                                  │
  │  - Invariant stability metric (K/total stable post-amendment)   │
  └─────────────────────────────────────────────────────────────────┘
```

## Slice plan

### Slice 1: Attributed proposals + review queue + no-auto-apply guarantee

Bound ticket: t_612b362e

**What ships:**

- `proposals/models.py` — `SpecAmendment` and `DispatchPolicyUpdate`
  dataclasses with full attribution fields (spec_provenance,
  sentinel_source, build_grounding, kind, threshold_status, priority,
  reviewer_notes, status).
- `proposals/queue.py` — proposal queue with state machine. All state
  transitions are logged with actor + timestamp. The `proposed →
  approved` and `proposed → applied` transitions are **hard-gated** to
  require `operator_approve()` with a human-verified action token.
- `proposals/review.py` — operator review gate and audit log.
  `operator_approve(proposal_id, operator_id, action_token)` is the
  ONLY path to transition a proposal to `approved` or `applied`.
- Amendment to `sentinel.py:_promotion_metadata` — add
  `min_evidence_threshold` parameter (default 5, floor 3). Proposals
  below threshold are emitted with `status: "below-threshold"` and do
  not enter the review queue.
- Amendment to `chitin.yaml` — new key
  `sentinel.promotion_threshold` (default 5, min 3, clamped with
  warning).

**Acceptance criteria (Slice 1):**

- **AC-S1-1**: Running `python -m analysis.sentinel` against a fixture
  of 7 identical deny patterns produces a proposal with
  `threshold_status: "above-threshold"` and `status: "proposed"`.
- **AC-S1-2**: Running the same with only 2 deny patterns produces a
  proposal with `threshold_status: "below-threshold"` and `status:
  "below-threshold"` — it does NOT enter the review queue.
- **AC-S1-3**: Setting `sentinel.promotion_threshold: 2` in
  `chitin.yaml` promotes the 2-occurrence proposal to
  `above-threshold`. Setting `sentinel.promotion_threshold: 1` clamps
  to 3 with a warning emitted to stderr.
- **AC-S1-4**: An agent-authored call to
  `queue.transition(proposal_id, "approved")` without an
  operator action token raises `PermissionError: only operator
  approve() may transition to approved`.
- **AC-S1-5**: `review.operator_approve(proposal_id, operator_id,
  action_token)` transitions a `proposed` or `ready-for-review`
  proposal to `approved` and records `{actor: operator_id, ts: <now>,
  transition: "proposed→approved"}` in the audit log.
- **AC-S1-6**: Every state transition (by any actor) appends an audit
  log entry with `{proposal_id, from_status, to_status, actor,
  actor_kind: "operator"|"agent"|"sentinel", timestamp}`.
- **AC-S1-7**: A `SpecAmendment` proposal carries `attribution`
  with `spec_provenance` and `sentinel_source` fields (placeholder
  strings `"spec:062-attribution TBD"` until spec 062 lands) and
  `evidence.build_grounding` (placeholder `"spec:063-build TBD"`).

### Slice 2: Approved amendments + versioned policy

Bound ticket: t_dcdbd97d

**What ships:**

- `proposals/versioned_policy.py` — immutable policy version log. An
  `apply()` call creates a new version; existing versions are never
  mutated. Each version records the `proposal_id` that produced it, the
  previous version hash, and the applied diff.
- `proposals/queue.py` extension — `applied` state transition routes
  through `versioned_policy.apply()`, which returns the new version
  number. Only proposals in `approved` state may transition to
  `applied`.
- Rollback: `versioned_policy.rollback(proposal_id, operator_id,
  action_token)` creates a new version that restores the state before
  the given proposal was applied. Rollback is also operator-gated.

**Acceptance criteria (Slice 2):**

- **AC-S2-1**: After `operator_approve()` transitions a proposal to
  `approved`, calling `queue.apply(proposal_id, operator_id,
  action_token)` creates a new policy version and transitions the
  proposal to `applied`.
- **AC-S2-2**: Attempting to apply a proposal that is in `proposed`
  state (not yet approved) raises `InvalidTransition: only approved
  proposals may be applied`.
- **AC-S2-3**: Policy versions are append-only: calling
  `versioned_policy.get_version(N)` returns the same content
  regardless of subsequent applies or rollbacks.
- **AC-S2-4**: Rolling back an applied amendment creates a NEW policy
  version whose content matches the state before that amendment. The
  original applied version remains unchanged.
- **AC-S2-5**: Each policy version records
  `{version, proposal_id, previous_version_hash, applied_diff, ts}`.

### Slice 3: Loop self-telemetry + post-amendment regression detection

Bound ticket: t_18ba5a7a

**What ships:**

- `proposals/self_telemetry.py` — tracks proposal throughput (count
  per window), approval rate (approved / total), time-to-review
  distribution (median, p95), and invariant stability metric (fraction
  of post-amendment sentinel runs where the invariant's deny count
  remained stable or decreased).
- `proposals/regression.py` — post-amendment regression detector. After
  an amendment is applied, observes N subsequent sentinel runs (default
  3). If the affected invariant's deny count increases vs pre-amendment
  baseline, flags the amendment for operator re-review with
  `regression_detected: true`.
- Integration: sentinel output now includes a `self_telemetry` section
  alongside `promotion`.

**Acceptance criteria (Slice 3):**

- **AC-S3-1**: `self_telemetry.compute(queue, window)` returns a
  dict with keys `proposal_throughput`, `approval_rate`,
  `time_to_review_median`, `time_to_review_p95`,
  `invariant_stability_ratio`.
- **AC-S3-2**: After 3 post-amendment sentinel runs where the
  invariant's deny count is ≤ pre-amendment baseline,
  `regression.check(proposal_id)` returns `{regression_detected:
  False, stability_observations: 3}`.
- **AC-S3-3**: After 3 post-amendment sentinel runs where the deny
  count increases in any run,
  `regression.check(proposal_id)` returns `{regression_detected:
  True, details: "...", flag_for_re_review: True}`.
- **AC-S3-4**: The sentinel JSON output includes a `self_telemetry`
  section populated from `self_telemetry.compute()`.

## Invariants

- **inv-1: R3 absolute — no auto-apply.** No code path (agent, cron,
  sentinel, or internal function) may transition a proposal to
  `approved` or `applied` without passing through
  `review.operator_approve()` with a human-verified action token.
  `proposed → approved` is a hard gate, not a soft recommendation.
- **inv-2: evidence threshold is configurable with a floor.** The
  minimum evidence threshold for entering the review queue defaults
  to 5 and is configurable via `chitin.yaml` →
  `sentinel.promotion_threshold`. Values below 3 are clamped with a
  warning. Proposals below threshold are emitted but flagged
  `below-threshold` and excluded from the review queue.
- **inv-3: triage / approval boundary is the state transition.** Agents
  may triage (change `priority`, add `reviewer_notes`, set
  `status: ready-for-review`, merge duplicates). Agents MUST NOT
  transition status to `approved` or `applied`. The state machine
  enforces this — only `operator_approve()` reaches those states.
- **inv-4: policy versions are immutable.** `versioned_policy.apply()`
  always creates a new version. Existing versions are never mutated.
  Rollback creates a new version that restores prior state.
- **inv-5: full attribution chain.** Every proposal carries
  `attribution.spec_provenance` (which spec the invariant traces to)
  and `attribution.sentinel_source` (which sentinel run mined it), plus
  `evidence.build_grounding` (which replayable build provides
  reproducible evidence). Placeholder strings are used until specs 062
  and 063 land.
- **inv-6: audit log is append-only.** Every state transition appends to
  the audit log. No log entry is ever mutated or deleted. Entries record
  `{proposal_id, from_status, to_status, actor, actor_kind,
  timestamp}`.
- **inv-7: regression detection is observational, not interventional.**
  The regression detector flags amendments for re-review; it does NOT
  auto-rollback. Auto-rollback would violate inv-1 (no auto-apply, and
  the reverse operation is still a state change).

## Out of scope

- **Spec 062 content** — this spec references 062's attribution shape
  but does not define it. 062 is a separate spec.
- **Spec 063 content** — same for replayable builds.
- **LLM-assisted proposal drafting** — the sentinel's `--llm-draft`
  flag exists already; this spec does not extend it. A future spec may
  add LLM summarization of proposals.
- **Cross-operator proposal sharing** — proposals are local to one
  operator box. No network uplink for proposals.
- **Web UI for the review queue** — the review queue is a local
  JSONL + SQLite surface. CLI only (`chitin proposal list`,
  `chitin proposal approve <id>`). A UI is out of scope.
- **Automated rollback** — inv-1 prohibits auto-apply; the symmetric
  operation (auto-rollback) is equally prohibited. Regression flags for
  re-review are observational.