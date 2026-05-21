# 064 — Implementation Plan

## Status: COMPLETE (all slices merged)

All three slices have been implemented and merged to main:

1. **Slice 1** — `feat(analysis): add sentinel proposal review queue (#823)`
   - `proposals/models.py` — SpecAmendment, DispatchPolicyUpdate, Attribution, BuildEvidence, ThresholdStatus, ProposalStatus
   - `proposals/queue.py` — ProposalQueue with audited state machine, hard no-auto-apply gate
   - `proposals/review.py` — operator_approve() as the sole path to approved/applied
   - Sentinel `_promotion_metadata` gains `min_evidence_threshold` (default 5, floor 3)
   - `chitin.yaml` → `sentinel.promotion_threshold` config key
   - Tests: AC-S1-1 through AC-S1-7

2. **Slice 2** — `Implement versioned proposal policy log (#824)`
   - `proposals/versioned_policy.py` — immutable append-only policy version log
   - `queue.apply()` routes through versioned_policy, operator-gated
   - Rollback creates a new version restoring prior state
   - Tests: AC-S2-1 through AC-S2-5

3. **Slice 3** — `feat(proposals): spec 064 slice 3 — self-telemetry + regression detection (#825)`
   - `proposals/self_telemetry.py` — throughput, approval rate, time-to-review, stability ratio
   - `proposals/regression.py` — post-amendment regression detection (N-observation, baseline comparison)
   - Sentinel JSON output includes `self_telemetry` section
   - Tests: AC-S3-1 through AC-S3-4

## Dependency resolution

- Spec 062 (invariant attribution): done. Attribution fields use placeholder `"spec:062-attribution TBD"` until 062's types land.
- Spec 063 (replayable builds): done. Build grounding uses placeholder `"spec:063-build TBD"` until 063's types land.