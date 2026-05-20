"""Proposal model types for telemetry-spec feedback."""
from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import StrEnum
from typing import Literal
from uuid import uuid4


class ProposalStatus(StrEnum):
    BELOW_THRESHOLD = "below-threshold"
    PROPOSED = "proposed"
    READY_FOR_REVIEW = "ready-for-review"
    APPROVED = "approved"
    REJECTED = "rejected"
    APPLIED = "applied"


class ThresholdStatus(StrEnum):
    ABOVE_THRESHOLD = "above-threshold"
    BELOW_THRESHOLD = "below-threshold"


@dataclass(frozen=True)
class Attribution:
    """Trace a proposal to the sentinel run and upstream spec provenance."""

    spec_provenance: str = "spec:062-attribution TBD"
    sentinel_source: str = "sentinel:unknown"


@dataclass(frozen=True)
class BuildEvidence:
    """Ground a proposal in replayable build evidence."""

    build_grounding: str = "spec:063-build TBD"


@dataclass
class ProposalBase:
    id: str
    attribution: Attribution
    evidence: BuildEvidence
    threshold_status: ThresholdStatus
    status: ProposalStatus
    priority: str = "normal"
    reviewer_notes: list[str] = field(default_factory=list)
    created_at: datetime = field(default_factory=lambda: datetime.now(tz=timezone.utc))

    def __post_init__(self) -> None:
        if self.threshold_status == ThresholdStatus.BELOW_THRESHOLD:
            self.status = ProposalStatus.BELOW_THRESHOLD


@dataclass
class SpecAmendment(ProposalBase):
    kind: Literal["spec-amendment"] = "spec-amendment"
    spec_id: str = ""
    amendment_summary: str = ""


@dataclass
class DispatchPolicyUpdate(ProposalBase):
    kind: Literal["dispatch-policy"] = "dispatch-policy"
    policy_path: str = "chitin.yaml"
    update_summary: str = ""


def new_proposal_id(prefix: str = "prop") -> str:
    return f"{prefix}_{uuid4().hex[:12]}"
