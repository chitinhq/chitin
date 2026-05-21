"""Tests for Slice 3: self-telemetry and regression detection.

AC-S3-1: self_telemetry.compute(queue, window) returns a dict with keys
    proposal_throughput, approval_rate, time_to_review_median,
    time_to_review_p95, invariant_stability_ratio.
AC-S3-2: After 3 post-amendment sentinel runs where deny count <= baseline,
    regression.check(proposal_id) returns {regression_detected: False,
    stability_observations: 3}.
AC-S3-3: After 3 post-amendment runs where deny count increases in any run,
    regression.check(proposal_id) returns {regression_detected: True,
    details: "...", flag_for_re_review: True}.
AC-S3-4: self_telemetry.compute() output has the right shape for a sentinel
    JSON self_telemetry section.
"""
from datetime import datetime, timedelta, timezone

import pytest

from analysis.proposals import (
    AmendmentBaseline,
    Attribution,
    BuildEvidence,
    DispatchPolicyUpdate,
    ProposalQueue,
    ProposalStatus,
    RegressionDetector,
    RegressionResult,
    SelfTelemetry,
    SentinelObservation,
    SpecAmendment,
    ThresholdStatus,
    operator_approve,
)


def _proposal(
    status=ProposalStatus.PROPOSED,
    proposal_id="prop_1",
    created_at: datetime | None = None,
):
    """Helper to create a DispatchPolicyUpdate for testing."""
    return DispatchPolicyUpdate(
        id=proposal_id,
        attribution=Attribution(
            spec_provenance="spec:062-attribution TBD",
            sentinel_source="sentinel:test",
        ),
        evidence=BuildEvidence(build_grounding="spec:063-build TBD"),
        threshold_status=ThresholdStatus.ABOVE_THRESHOLD,
        status=status,
        update_summary="test",
        created_at=created_at or datetime.now(tz=timezone.utc),
    )


def _spec_amendment(
    status=ProposalStatus.PROPOSED,
    proposal_id="prop_spec_1",
    created_at: datetime | None = None,
):
    """Helper to create a SpecAmendment for testing."""
    return SpecAmendment(
        id=proposal_id,
        attribution=Attribution(
            spec_provenance="spec:062-attribution TBD",
            sentinel_source="sentinel:test",
        ),
        evidence=BuildEvidence(build_grounding="spec:063-build TBD"),
        threshold_status=ThresholdStatus.ABOVE_THRESHOLD,
        status=status,
        spec_id="spec-invariant-7",
        amendment_summary="Update threshold from 5 to 3",
        created_at=created_at or datetime.now(tz=timezone.utc),
    )


# ---------------------------------------------------------------------------
# AC-S3-1: self_telemetry.compute returns correct keys and values
# ---------------------------------------------------------------------------


class TestSelfTelemetryCompute:
    """Test SelfTelemetry.compute(queue, window) output shape and values."""

    def test_returns_all_required_keys(self):
        """AC-S3-1: compute returns dict with all 5 required keys."""
        queue = ProposalQueue()
        window = timedelta(days=7)

        result = SelfTelemetry.compute(queue, window)

        assert isinstance(result, dict)
        assert "proposal_throughput" in result
        assert "approval_rate" in result
        assert "time_to_review_median" in result
        assert "time_to_review_p95" in result
        assert "invariant_stability_ratio" in result

    def test_empty_queue_returns_zeros_and_nones(self):
        """AC-S3-1: empty queue returns throughput=0, rate=0.0, medians=None."""
        queue = ProposalQueue()
        result = SelfTelemetry.compute(queue, timedelta(days=1))

        assert result["proposal_throughput"] == 0
        assert result["approval_rate"] == 0.0
        assert result["time_to_review_median"] is None
        assert result["time_to_review_p95"] is None

    def test_proposal_throughput_counts_proposals_in_window(self):
        """AC-S3-1: proposal_throughput counts proposals created in window."""
        now = datetime.now(tz=timezone.utc)
        queue = ProposalQueue()

        # Two proposals within window
        queue.add(_proposal(proposal_id="p1", created_at=now - timedelta(hours=1)))
        queue.add(_proposal(proposal_id="p2", created_at=now - timedelta(hours=2)))

        # One proposal outside window
        queue.add(_proposal(proposal_id="p3", created_at=now - timedelta(days=30)))

        window = (now - timedelta(days=7), now)
        result = SelfTelemetry.compute(queue, window)

        assert result["proposal_throughput"] == 2

    def test_approval_rate_computes_fraction(self):
        """AC-S3-1: approval_rate is approved / total as a fraction."""
        now = datetime.now(tz=timezone.utc)
        queue = ProposalQueue()
        p1 = _proposal(proposal_id="p1", status=ProposalStatus.READY_FOR_REVIEW)
        queue.add(p1)

        result = SelfTelemetry.compute(queue, timedelta(days=7))
        assert result["approval_rate"] == 0.0

        # Approve the proposal
        operator_approve(queue, "p1", "operator1", "token1")
        result = SelfTelemetry.compute(queue, timedelta(days=7))
        assert result["proposal_throughput"] == 1
        assert result["approval_rate"] == 1.0

    def test_approval_rate_with_mixed_statuses(self):
        """AC-S3-1: approval rate with some approved, some not."""
        queue = ProposalQueue()
        p1 = _proposal(proposal_id="p1")
        p2 = _proposal(proposal_id="p2")
        queue.add(p1)
        queue.add(p2)

        # Approve p1 only
        queue.transition("p1", ProposalStatus.READY_FOR_REVIEW, actor="agent1")
        operator_approve(queue, "p1", "op1", "token1")

        result = SelfTelemetry.compute(queue, timedelta(days=7))
        assert result["proposal_throughput"] == 2
        assert result["approval_rate"] == 0.5

    def test_time_to_review_median_and_p95(self):
        """AC-S3-1: time_to_review_median and p95 from audit log."""
        now = datetime.now(tz=timezone.utc)
        queue = ProposalQueue()

        # Create two proposals with specific timestamps
        p1 = _proposal(proposal_id="p1", created_at=now - timedelta(hours=10))
        p2 = _proposal(proposal_id="p2", created_at=now - timedelta(hours=5))
        queue.add(p1)
        queue.add(p2)

        # Transition p1 to ready-for-review; p2 to approved
        queue.transition("p1", ProposalStatus.READY_FOR_REVIEW, actor="agent1")
        queue.transition("p2", ProposalStatus.READY_FOR_REVIEW, actor="agent1")
        operator_approve(queue, "p2", "op1", "token1")

        result = SelfTelemetry.compute(queue, timedelta(days=1))

        # Both should have time_to_review values
        assert result["time_to_review_median"] is not None
        assert result["time_to_review_p95"] is not None
        # Times should be non-negative
        assert result["time_to_review_median"] >= 0
        assert result["time_to_review_p95"] >= 0

    def test_time_to_review_p95_none_with_single_observation(self):
        """AC-S3-1: p95 is None when fewer than 2 data points."""
        queue = ProposalQueue()
        p1 = _proposal(proposal_id="p1")
        queue.add(p1)
        queue.transition("p1", ProposalStatus.READY_FOR_REVIEW, actor="agent1")

        result = SelfTelemetry.compute(queue, timedelta(days=1))
        assert result["time_to_review_median"] is not None
        assert result["time_to_review_p95"] is None

    def test_timedelta_window(self):
        """AC-S3-1: window as timedelta works (from now backward)."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="p1"))
        result = SelfTelemetry.compute(queue, timedelta(days=7))
        assert result["proposal_throughput"] == 1

    def test_datetime_range_window(self):
        """AC-S3-1: window as (start, end) datetime tuple works."""
        now = datetime.now(tz=timezone.utc)
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="p1", created_at=now - timedelta(hours=1)))
        result = SelfTelemetry.compute(
            queue, (now - timedelta(days=7), now + timedelta(days=1))
        )
        assert result["proposal_throughput"] == 1


# ---------------------------------------------------------------------------
# AC-S3-2: Stable observations -> no regression
# ---------------------------------------------------------------------------


class TestRegressionStable:
    """AC-S3-2: After 3 stable post-amendment runs, no regression."""

    def test_stable_observations_no_regression(self):
        """After 3 runs with deny_count <= baseline, regression_detected is False."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="prop_1"))
        detector = RegressionDetector(queue, n_observations=3)

        # Record baseline at time of amendment application
        detector.record_baseline("prop_1", baseline_deny_count=5)

        # Three sentinel runs, all stable or improving
        detector.record_observation("prop_1", "run_1", deny_count=5)
        detector.record_observation("prop_1", "run_2", deny_count=4)
        detector.record_observation("prop_1", "run_3", deny_count=3)

        result = detector.check("prop_1")

        assert result.regression_detected is False
        assert result.stability_observations == 3
        assert result.flag_for_re_review is False

    def test_stable_with_exact_baseline(self):
        """Deny count exactly at baseline is stable (not regressing)."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="prop_1"))
        detector = RegressionDetector(queue, n_observations=3)

        detector.record_baseline("prop_1", baseline_deny_count=10)
        detector.record_observation("prop_1", "run_1", deny_count=10)
        detector.record_observation("prop_1", "run_2", deny_count=10)
        detector.record_observation("prop_1", "run_3", deny_count=10)

        result = detector.check("prop_1")

        assert result.regression_detected is False
        assert result.stability_observations == 3

    def test_fewer_than_n_observations(self):
        """If fewer than N observations, all that exist are checked."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="prop_1"))
        detector = RegressionDetector(queue, n_observations=3)

        detector.record_baseline("prop_1", baseline_deny_count=5)
        detector.record_observation("prop_1", "run_1", deny_count=5)

        result = detector.check("prop_1")

        assert result.regression_detected is False
        assert result.stability_observations == 1


# ---------------------------------------------------------------------------
# AC-S3-3: Regression detection when deny count increases
# ---------------------------------------------------------------------------


class TestRegressionDetected:
    """AC-S3-3: After runs with increased deny count, regression detected."""

    def test_regression_detected_with_increase(self):
        """AC-S3-3: deny count increase flags regression and re-review."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="prop_1"))
        detector = RegressionDetector(queue, n_observations=3)

        detector.record_baseline("prop_1", baseline_deny_count=5)
        detector.record_observation("prop_1", "run_1", deny_count=5)
        detector.record_observation("prop_1", "run_2", deny_count=8)  # increase!
        detector.record_observation("prop_1", "run_3", deny_count=4)

        result = detector.check("prop_1")

        assert result.regression_detected is True
        assert result.flag_for_re_review is True
        assert "deny count" in result.details.lower() or "Regression" in result.details

    def test_regression_all_runs_increase(self):
        """AC-S3-3: all runs showing increase is still regression_detected."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="prop_1"))
        detector = RegressionDetector(queue, n_observations=3)

        detector.record_baseline("prop_1", baseline_deny_count=5)
        detector.record_observation("prop_1", "run_1", deny_count=7)
        detector.record_observation("prop_1", "run_2", deny_count=9)
        detector.record_observation("prop_1", "run_3", deny_count=12)

        result = detector.check("prop_1")

        assert result.regression_detected is True
        assert result.flag_for_re_review is True
        assert result.stability_observations == 0

    def test_regression_with_single_increase(self):
        """AC-S3-3: even one run with increase triggers regression."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="prop_1"))
        detector = RegressionDetector(queue, n_observations=3)

        detector.record_baseline("prop_1", baseline_deny_count=3)
        detector.record_observation("prop_1", "run_1", deny_count=3)
        detector.record_observation("prop_1", "run_2", deny_count=3)
        detector.record_observation("prop_1", "run_3", deny_count=10)

        result = detector.check("prop_1")

        assert result.regression_detected is True
        assert result.flag_for_re_review is True

    def test_regression_result_type(self):
        """RegressionResult is a dataclass with expected fields."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="prop_1"))
        detector = RegressionDetector(queue, n_observations=3)

        detector.record_baseline("prop_1", baseline_deny_count=5)
        detector.record_observation("prop_1", "run_1", deny_count=6)

        result = detector.check("prop_1")

        assert isinstance(result, RegressionResult)
        assert hasattr(result, "regression_detected")
        assert hasattr(result, "stability_observations")
        assert hasattr(result, "details")
        assert hasattr(result, "flag_for_re_review")

    def test_missing_baseline_raises(self):
        """Checking regression for unknown proposal raises KeyError."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="prop_1"))
        detector = RegressionDetector(queue, n_observations=3)

        with pytest.raises(KeyError, match="No baseline recorded"):
            detector.check("prop_1")


# ---------------------------------------------------------------------------
# AC-S3-4: self_telemetry output shape for sentinel JSON
# ---------------------------------------------------------------------------


class TestSelfTelemetrySentinelShape:
    """AC-S3-4: verify compute() output can populate a sentinel self_telemetry section."""

    def test_output_is_json_serializable(self):
        """AC-S3-4: compute() result values are JSON-serializable types."""
        import json

        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="p1"))
        queue.transition("p1", ProposalStatus.READY_FOR_REVIEW, actor="agent1")
        operator_approve(queue, "p1", "op1", "token1")

        result = SelfTelemetry.compute(queue, timedelta(days=7))

        # Should not raise
        serialized = json.dumps(result)
        assert isinstance(serialized, str)

    def test_sentinel_json_includes_self_telemetry_section(self):
        """AC-S3-4: sentinel JSON output shape includes self_telemetry key."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="p1"))

        telemetry = SelfTelemetry.compute(queue, timedelta(days=7))

        # Simulating sentinel JSON structure
        sentinel_output = {
            "promotion": {"proposal_id": "p1"},
            "self_telemetry": telemetry,
        }

        assert "self_telemetry" in sentinel_output
        st = sentinel_output["self_telemetry"]
        assert "proposal_throughput" in st
        assert "approval_rate" in st
        assert "time_to_review_median" in st
        assert "time_to_review_p95" in st
        assert "invariant_stability_ratio" in st

    def test_stability_ratio_reflects_regression_observations(self):
        """AC-S3-4: invariant_stability_ratio incorporates regression data."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="p1"))
        detector = RegressionDetector(queue, n_observations=3)

        detector.record_baseline("p1", baseline_deny_count=5)
        detector.record_observation("p1", "run_1", deny_count=5)
        detector.record_observation("p1", "run_2", deny_count=4)
        detector.record_observation("p1", "run_3", deny_count=6)  # one regression

        result = SelfTelemetry.compute(queue, timedelta(days=7))

        # 2 out of 3 stable
        assert result["invariant_stability_ratio"] == pytest.approx(2.0 / 3.0)

    def test_stability_ratio_all_stable(self):
        """AC-S3-4: all stable observations gives ratio 1.0."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="p1"))
        detector = RegressionDetector(queue, n_observations=3)

        detector.record_baseline("p1", baseline_deny_count=5)
        detector.record_observation("p1", "run_1", deny_count=5)
        detector.record_observation("p1", "run_2", deny_count=4)
        detector.record_observation("p1", "run_3", deny_count=3)

        result = SelfTelemetry.compute(queue, timedelta(days=7))

        assert result["invariant_stability_ratio"] == 1.0

    def test_stability_ratio_no_observations(self):
        """AC-S3-4: no observations gives ratio 0.0 (no data)."""
        queue = ProposalQueue()
        queue.add(_proposal(proposal_id="p1"))

        result = SelfTelemetry.compute(queue, timedelta(days=7))
        assert result["invariant_stability_ratio"] == 0.0