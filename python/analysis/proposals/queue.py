"""Proposal queue and audited state machine."""
from __future__ import annotations

from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from typing import Literal

from analysis.proposals.models import ProposalBase, ProposalStatus

ActorKind = Literal["operator", "agent", "sentinel"]


class InvalidTransition(ValueError):
    """Raised when a proposal state transition is not allowed."""


@dataclass(frozen=True)
class AuditEntry:
    proposal_id: str
    from_status: str
    to_status: str
    actor: str
    actor_kind: ActorKind
    timestamp: datetime

    def to_dict(self) -> dict[str, str]:
        body = asdict(self)
        body["timestamp"] = self.timestamp.isoformat()
        return body


class ProposalQueue:
    """In-memory review queue with append-only audit entries.

    Persistence and policy application are later slices; this slice owns the
    hard no-auto-apply state boundary and transition audit contract.
    """

    _AGENT_ALLOWED = {
        ProposalStatus.PROPOSED: {ProposalStatus.READY_FOR_REVIEW},
        ProposalStatus.READY_FOR_REVIEW: {ProposalStatus.PROPOSED},
    }
    _OPERATOR_ALLOWED = {
        ProposalStatus.PROPOSED: {ProposalStatus.READY_FOR_REVIEW, ProposalStatus.APPROVED, ProposalStatus.REJECTED},
        ProposalStatus.READY_FOR_REVIEW: {ProposalStatus.PROPOSED, ProposalStatus.APPROVED, ProposalStatus.REJECTED},
        ProposalStatus.APPROVED: {ProposalStatus.APPLIED},
    }

    def __init__(self) -> None:
        self._proposals: dict[str, ProposalBase] = {}
        self._audit: list[AuditEntry] = []

    @property
    def audit_log(self) -> tuple[AuditEntry, ...]:
        return tuple(self._audit)

    def add(self, proposal: ProposalBase, *, actor: str = "sentinel") -> ProposalBase:
        if proposal.id in self._proposals:
            raise ValueError(f"proposal already exists: {proposal.id}")
        self._proposals[proposal.id] = proposal
        self._audit_transition(
            proposal.id,
            from_status="new",
            to_status=str(proposal.status),
            actor=actor,
            actor_kind="sentinel",
        )
        return proposal

    def get(self, proposal_id: str) -> ProposalBase:
        try:
            return self._proposals[proposal_id]
        except KeyError as exc:
            raise KeyError(f"unknown proposal: {proposal_id}") from exc

    def list_reviewable(self) -> list[ProposalBase]:
        return [
            proposal
            for proposal in self._proposals.values()
            if proposal.status in {ProposalStatus.PROPOSED, ProposalStatus.READY_FOR_REVIEW}
        ]

    def transition(
        self,
        proposal_id: str,
        to_status: ProposalStatus | str,
        *,
        actor: str,
        actor_kind: ActorKind = "agent",
        operator_action_token: str | None = None,
    ) -> ProposalBase:
        target = ProposalStatus(to_status)
        if actor_kind == "operator":
            raise PermissionError("operator transitions must use operator_transition()")
        if operator_action_token is not None:
            raise PermissionError("operator action token is only accepted by operator_transition()")
        self._reject_non_operator_states(target)

        return self._transition_checked(
            proposal_id,
            target,
            actor=actor,
            actor_kind=actor_kind,
        )

    def operator_transition(
        self,
        proposal_id: str,
        to_status: ProposalStatus | str,
        *,
        operator_id: str,
        action_token: str,
    ) -> ProposalBase:
        target = ProposalStatus(to_status)
        self._require_operator_token(target, action_token)
        return self._transition_checked(
            proposal_id,
            target,
            actor=operator_id,
            actor_kind="operator",
        )

    def _transition_checked(
        self,
        proposal_id: str,
        target: ProposalStatus,
        *,
        actor: str,
        actor_kind: ActorKind,
    ) -> ProposalBase:
        proposal = self.get(proposal_id)
        current = proposal.status
        allowed = self._OPERATOR_ALLOWED if actor_kind == "operator" else self._AGENT_ALLOWED
        if target not in allowed.get(current, set()):
            raise InvalidTransition(f"invalid transition: {current} -> {target}")
        proposal.status = target
        self._audit_transition(
            proposal_id,
            from_status=str(current),
            to_status=str(target),
            actor=actor,
            actor_kind=actor_kind,
        )
        return proposal

    def _audit_transition(
        self,
        proposal_id: str,
        *,
        from_status: str,
        to_status: str,
        actor: str,
        actor_kind: ActorKind,
    ) -> None:
        self._audit.append(
            AuditEntry(
                proposal_id=proposal_id,
                from_status=from_status,
                to_status=to_status,
                actor=actor,
                actor_kind=actor_kind,
                timestamp=datetime.now(tz=timezone.utc),
            )
        )

    @staticmethod
    def _reject_non_operator_states(target: ProposalStatus) -> None:
        if target == ProposalStatus.APPROVED:
            raise PermissionError("only operator approve() may transition to approved")
        if target == ProposalStatus.APPLIED:
            raise PermissionError("only operator approve() may transition to applied")

    @staticmethod
    def _require_operator_token(target: ProposalStatus, token: str | None) -> None:
        if target in {ProposalStatus.APPROVED, ProposalStatus.APPLIED, ProposalStatus.REJECTED} and not token:
            raise PermissionError("operator action token required")
