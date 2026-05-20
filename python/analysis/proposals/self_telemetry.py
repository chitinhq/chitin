"""Loop self-telemetry for the proposal review cycle.

Tracks proposal throughput, approval rate, time-to-review distribution,
and invariant stability ratio across sentinel runs after amendments are
applied.
"""
from __future__ import annotations

import statistics
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from typing import Union

from analysis.proposals.models import ProposalStatus
from analysis.proposals.queue import AuditEntry, ProposalQueue


Window = Union[timedelta, tuple[datetime, datetime]]


@dataclass
class SentinelObservation:
    """A single sentinel run observation after an amendment was applied."""

    run_id: str
    timestamp: datetime
    deny_count: int


@dataclass
class AmendmentBaseline:
    """Baseline deny count captured at the time an amendment is applied."""

    proposal_id: str
    baseline_deny_count: int
    applied_at: datetime
    observations: list[SentinelObservation] = field(default_factory=list)


class SelfTelemetry:
    """Compute self-telemetry metrics for the proposal review loop."""

    @staticmethod
    def compute(queue: ProposalQueue, window: Window) -> dict:
        """Compute loop self-telemetry metrics over a time window.

        Args:
            queue: The proposal queue with audit log.
            window: Either a timedelta (lookback from now) or a
                     (start, end) datetime tuple filtering proposals
                     by creation time.

        Returns:
            Dict with keys:
                proposal_throughput: count of proposals in the window
                approval_rate: approved / total (0.0 if no proposals)
                time_to_review_median: median seconds from creation to
                    first review-ready or approved transition (None if
                    no data)
                time_to_review_p95: p95 of same distribution (None if
                    fewer than 2 data points)
                invariant_stability_ratio: fraction of post-amendment
                    sentinel observations where deny count stayed stable
                    or decreased vs baseline (0.0 if no observations)
        """
        if isinstance(window, timedelta):
            window_end = datetime.now(tz=timezone.utc)
            window_start = window_end - window
        else:
            window_start, window_end = window

        # Gather proposals created within the window
        proposals_in_window = [
            p
            for p in queue._proposals.values()
            if window_start <= p.created_at <= window_end
        ]

        total = len(proposals_in_window)
        approved_count = sum(
            1
            for p in proposals_in_window
            if p.status == ProposalStatus.APPROVED
            or p.status == ProposalStatus.APPLIED
        )
        proposal_throughput = total
        approval_rate = approved_count / total if total > 0 else 0.0

        # Compute time-to-review from audit log
        audit_log = queue.audit_log
        time_to_review: list[float] = []
        for proposal in proposals_in_window:
            created_at = proposal.created_at
            # Find first transition to ready-for-review or approved
            first_review_ts: datetime | None = None
            for entry in audit_log:
                if entry.proposal_id != proposal.id:
                    continue
                if entry.to_status in (
                    str(ProposalStatus.READY_FOR_REVIEW),
                    str(ProposalStatus.APPROVED),
                ):
                    first_review_ts = entry.timestamp
                    break
            if first_review_ts is not None:
                delta = (first_review_ts - created_at).total_seconds()
                if delta >= 0:
                    time_to_review.append(delta)

        time_to_review_median: float | None = None
        time_to_review_p95: float | None = None
        if time_to_review:
            time_to_review_median = statistics.median(time_to_review)
        if len(time_to_review) >= 2:
            sorted_times = sorted(time_to_review)
            # p95 using nearest-rank method
            rank = max(1, int(0.95 * len(sorted_times)))
            # Clamp to valid index
            idx = min(rank, len(sorted_times)) - 1
            time_to_review_p95 = sorted_times[idx]

        # Invariant stability ratio: from regression baselines stored on queue
        stability_ratio = SelfTelemetry._compute_stability_ratio(queue)

        return {
            "proposal_throughput": proposal_throughput,
            "approval_rate": approval_rate,
            "time_to_review_median": time_to_review_median,
            "time_to_review_p95": time_to_review_p95,
            "invariant_stability_ratio": stability_ratio,
        }

    @staticmethod
    def _compute_stability_ratio(queue: ProposalQueue) -> float:
        """Compute fraction of post-amendment observations where deny
        count stayed stable or decreased vs the pre-amendment baseline.

        Looks for AmendmentBaseline objects stored on the queue.
        Returns 0.0 if there are no observations at all.
        """
        baselines: list[AmendmentBaseline] = getattr(
            queue, "_regression_baselines", []
        )
        total_observations = 0
        stable_observations = 0
        for baseline in baselines:
            for obs in baseline.observations:
                total_observations += 1
                if obs.deny_count <= baseline.baseline_deny_count:
                    stable_observations += 1

        return stable_observations / total_observations if total_observations > 0 else 0.0