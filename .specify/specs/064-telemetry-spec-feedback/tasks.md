# 064 — Tasks

## Status: ALL COMPLETE

### Slice 1: Attributed proposals + review queue + no-auto-apply guarantee ✅

| Task | Status | Notes |
|------|--------|-------|
| T-S1-01 models.py — SpecAmendment, DispatchPolicyUpdate, Attribution, BuildEvidence, ThresholdStatus, ProposalStatus | ✅ merged | PR #823 |
| T-S1-02 queue.py — ProposalQueue with audited state machine, agent/operator transition gates | ✅ merged | PR #823 |
| T-S1-03 review.py — operator_approve() sole path to approved/applied | ✅ merged | PR #823 |
| T-S1-04 sentinel._promotion_metadata — min_evidence_threshold (default 5, floor 3) | ✅ merged | PR #823 |
| T-S1-05 chitin.yaml — sentinel.promotion_threshold config key with clamping | ✅ merged | PR #823 |
| T-S1-06 Tests: AC-S1-1 through AC-S1-7 | ✅ merged | PR #823 |

### Slice 2: Approved amendments + versioned policy ✅

| Task | Status | Notes |
|------|--------|-------|
| T-S2-01 versioned_policy.py — immutable append-only policy version log | ✅ merged | PR #824 |
| T-S2-02 queue.apply() — routes through versioned_policy, operator-gated | ✅ merged | PR #824 |
| T-S2-03 versioned_policy.rollback() — creates new version restoring prior state | ✅ merged | PR #824 |
| T-S2-04 Tests: AC-S2-1 through AC-S2-5 | ✅ merged | PR #824 |

### Slice 3: Loop self-telemetry + post-amendment regression detection ✅

| Task | Status | Notes |
|------|--------|-------|
| T-S3-01 self_telemetry.py — compute() returns throughput, approval rate, time-to-review, stability | ✅ merged | PR #825 |
| T-S3-02 regression.py — post-amendment regression detection, N-observation window | ✅ merged | PR #825 |
| T-S3-03 Sentinel JSON output includes self_telemetry section | ✅ merged | PR #825 |
| T-S3-04 Tests: AC-S3-1 through AC-S3-4 | ✅ merged | PR #825 |