"""Human operator review gate for proposals."""
from __future__ import annotations

from analysis.proposals.models import ProposalStatus
from analysis.proposals.queue import ProposalQueue


def operator_approve(
    queue: ProposalQueue,
    proposal_id: str,
    operator_id: str,
    action_token: str,
):
    """Approve a proposal through the only allowed approval path."""

    return queue.operator_transition(
        proposal_id,
        ProposalStatus.APPROVED,
        operator_id=operator_id,
        action_token=action_token,
    )
