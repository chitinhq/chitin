"""Post-amendment regression detection for applied proposals.

After an amendment is applied, observes N subsequent sentinel runs. If
the affected invariant's deny count increases vs the pre-amendment
baseline, flags the amendment for operator re-review.

Invariant 7: regression detection is observational only — NO auto-rollback.
"""
from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any

from analysis.proposals.models import ProposalStatus
from analysis.proposals.queue import ProposalQueue
from analysis.proposals.self_telemetry import AmendmentBaseline, SentinelObservation


@dataclass
class RegressionResult:
    """Result of a regression check for an applied amendment."""

    regression_detected: bool
    stability_observations: int
    details: str
    flag_for_re_review: bool


class RegressionDetector:
    """Detect post-amendment regressions by observing sentinel run outcomes.

    Compares deny counts from subsequent sentinel runs against the
    pre-amendment baseline. Observational only — does not auto-rollback.
    """

    def __init__(self, queue: ProposalQueue, *, n_observations: int = 3) -> None:
        self._queue = queue
        self._n_observations = n_observations
        # Initialize baselines storage on the queue if not present
        if not hasattr(queue, "_regression_baselines"):
            queue._regression_baselines: list[AmendmentBaseline] = []

    def record_baseline(
        self,
        proposal_id: str,
        baseline_deny_count: int,
        applied_at: datetime | None = None,
    ) -> AmendmentBaseline:
        """Record the pre-amendment baseline deny count for a proposal.

        Call this when an amendment is applied (before subsequent sentinel
        runs produce observations).
        """
        if applied_at is None:
            applied_at = datetime.now(tz=timezone.utc)

        baseline = AmendmentBaseline(
            proposal_id=proposal_id,
            baseline_deny_count=baseline_deny_count,
            applied_at=applied_at,
        )
        self._queue._regression_baselines.append(baseline)
        return baseline

    def record_observation(
        self,
        proposal_id: str,
        run_id: str,
        deny_count: int,
        timestamp: datetime | None = None,
    ) -> SentinelObservation:
        """Record a sentinel run observation for a proposal's baseline.

        Call this after each sentinel run that follows an amendment.
        """
        if timestamp is None:
            timestamp = datetime.now(tz=timezone.utc)

        baseline = self._find_baseline(proposal_id)
        obs = SentinelObservation(
            run_id=run_id,
            timestamp=timestamp,
            deny_count=deny_count,
        )
        baseline.observations.append(obs)
        return obs

    def check(self, proposal_id: str) -> RegressionResult:
        """Check whether a regression has been detected for an amendment.

        Examines the last N sentinel run observations (default 3) after
        the amendment was applied. If any observation shows the deny
        count increasing vs baseline, flags for re-review.

        Returns:
            RegressionResult with regression_detected, stability_observations,
            details, and flag_for_re_review.
        """
        baseline = self._find_baseline(proposal_id)
        observations = baseline.observations[-self._n_observations :]
        observed_count = len(observations)

        regressions: list[SentinelObservation] = []
        stable_count = 0

        for obs in observations:
            if obs.deny_count > baseline.baseline_deny_count:
                regressions.append(obs)
            else:
                stable_count += 1

        if regressions:
            worst = max(regressions, key=lambda o: o.deny_count)
            details = (
                f"Regression detected for proposal {proposal_id}: "
                f"deny count {worst.deny_count} exceeds baseline "
                f"{baseline.baseline_deny_count} "
                f"(baseline captured {baseline.applied_at.isoformat()}); "
                f"{len(regressions)} of {observed_count} observations showed increase"
            )
            return RegressionResult(
                regression_detected=True,
                stability_observations=stable_count,
                details=details,
                flag_for_re_review=True,
            )

        details = (
            f"No regression for proposal {proposal_id}: all {observed_count} "
            f"observations stable or improved vs baseline "
            f"{baseline.baseline_deny_count}"
        )
        return RegressionResult(
            regression_detected=False,
            stability_observations=observed_count,
            details=details,
            flag_for_re_review=False,
        )

    def _find_baseline(self, proposal_id: str) -> AmendmentBaseline:
        """Find the baseline for a proposal, raising if not found."""
        for baseline in self._queue._regression_baselines:
            if baseline.proposal_id == proposal_id:
                return baseline
        raise KeyError(f"No baseline recorded for proposal: {proposal_id}")