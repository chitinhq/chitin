import pytest

from analysis.proposals import (
    Attribution,
    BuildEvidence,
    DispatchPolicyUpdate,
    ProposalQueue,
    ProposalStatus,
    ThresholdStatus,
    operator_approve,
)


def _proposal(status=ProposalStatus.PROPOSED):
    return DispatchPolicyUpdate(
        id="prop_1",
        attribution=Attribution(
            spec_provenance="spec:062-attribution TBD",
            sentinel_source="sentinel:test",
        ),
        evidence=BuildEvidence(build_grounding="spec:063-build TBD"),
        threshold_status=ThresholdStatus.ABOVE_THRESHOLD,
        status=status,
        update_summary="test",
    )


def test_agent_cannot_approve_or_apply_proposal():
    queue = ProposalQueue()
    queue.add(_proposal())

    with pytest.raises(PermissionError, match=r"only operator approve\(\) may transition to approved"):
        queue.transition("prop_1", ProposalStatus.APPROVED, actor="clawta", actor_kind="agent")

    with pytest.raises(PermissionError, match=r"only operator approve\(\) may transition to applied"):
        queue.transition("prop_1", ProposalStatus.APPLIED, actor="clawta", actor_kind="agent")


def test_operator_approve_records_audit_transition():
    queue = ProposalQueue()
    queue.add(_proposal())

    approved = operator_approve(queue, "prop_1", "red", "human-reviewed-token")

    assert approved.status == ProposalStatus.APPROVED
    audit = queue.audit_log[-1]
    assert audit.proposal_id == "prop_1"
    assert audit.from_status == "proposed"
    assert audit.to_status == "approved"
    assert audit.actor == "red"
    assert audit.actor_kind == "operator"


def test_operator_cannot_use_generic_transition_path():
    queue = ProposalQueue()
    queue.add(_proposal())

    with pytest.raises(PermissionError, match=r"operator transitions must use operator_transition\(\)"):
        queue.transition(
            "prop_1",
            ProposalStatus.APPROVED,
            actor="red",
            actor_kind="operator",
            operator_action_token="human-reviewed-token",
        )


def test_every_transition_appends_audit_entry():
    queue = ProposalQueue()
    queue.add(_proposal())
    queue.transition("prop_1", ProposalStatus.READY_FOR_REVIEW, actor="clawta", actor_kind="agent")

    assert [(entry.from_status, entry.to_status, entry.actor_kind) for entry in queue.audit_log] == [
        ("new", "proposed", "sentinel"),
        ("proposed", "ready-for-review", "agent"),
    ]


def test_below_threshold_proposals_do_not_enter_review_queue():
    queue = ProposalQueue()
    queue.add(
        DispatchPolicyUpdate(
            id="prop_low",
            attribution=Attribution(),
            evidence=BuildEvidence(),
            threshold_status=ThresholdStatus.BELOW_THRESHOLD,
            status=ProposalStatus.PROPOSED,
        )
    )

    assert queue.get("prop_low").status == ProposalStatus.BELOW_THRESHOLD
    assert queue.list_reviewable() == []
