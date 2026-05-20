import pytest

from analysis.proposals import (
    Attribution,
    BuildEvidence,
    DispatchPolicyUpdate,
    InvalidTransition,
    ProposalQueue,
    ProposalStatus,
    ThresholdStatus,
    VersionedPolicyLog,
    operator_approve,
)


def _proposal(proposal_id="prop_1", status=ProposalStatus.PROPOSED):
    return DispatchPolicyUpdate(
        id=proposal_id,
        attribution=Attribution(sentinel_source="sentinel:test"),
        evidence=BuildEvidence(),
        threshold_status=ThresholdStatus.ABOVE_THRESHOLD,
        status=status,
        update_summary="set promotion threshold",
    )


def test_operator_apply_creates_policy_version_and_marks_applied():
    queue = ProposalQueue()
    queue.add(_proposal())
    operator_approve(queue, "prop_1", "red", "human-reviewed-token")

    version = queue.apply(
        "prop_1",
        operator_id="red",
        action_token="human-reviewed-token",
        applied_diff={"sentinel.promotion_threshold": 7},
    )

    assert queue.get("prop_1").status == ProposalStatus.APPLIED
    assert version.version == 1
    assert version.proposal_id == "prop_1"
    assert version.previous_version_hash is None
    assert version.applied_diff == {"sentinel.promotion_threshold": 7}
    assert version.content == {"sentinel.promotion_threshold": 7}
    assert queue.audit_log[-1].to_status == "applied"


def test_apply_requires_approved_proposal():
    queue = ProposalQueue()
    queue.add(_proposal())

    with pytest.raises(InvalidTransition, match="only approved proposals may be applied"):
        queue.apply(
            "prop_1",
            operator_id="red",
            action_token="human-reviewed-token",
            applied_diff={"sentinel.promotion_threshold": 7},
        )


def test_policy_versions_are_append_only_snapshots():
    log = VersionedPolicyLog()
    first = log.apply("prop_1", {"sentinel.promotion_threshold": 7})
    snapshot = first.to_dict()
    log.apply("prop_2", {"sentinel.promotion_threshold": 9})

    assert log.get_version(1).to_dict() == snapshot
    assert log.get_version(2).content == {"sentinel.promotion_threshold": 9}
    with pytest.raises(TypeError):
        log.get_version(1).content["sentinel.promotion_threshold"] = 99


def test_rollback_creates_new_version_with_prior_content():
    log = VersionedPolicyLog()
    base = log.apply("prop_1", {"sentinel.promotion_threshold": 7})
    changed = log.apply("prop_2", {"sentinel.promotion_threshold": 9, "review.required": True})

    rollback = log.rollback("prop_2", "red", "human-reviewed-token")

    assert rollback.version == 3
    assert rollback.proposal_id == "rollback:prop_2"
    assert rollback.previous_version_hash == changed.hash
    assert rollback.content == base.content
    assert log.get_version(2).content == {
        "sentinel.promotion_threshold": 9,
        "review.required": True,
    }


def test_operator_transition_cannot_bypass_versioned_policy_apply():
    queue = ProposalQueue()
    queue.add(_proposal())
    operator_approve(queue, "prop_1", "red", "human-reviewed-token")

    with pytest.raises(InvalidTransition, match="invalid transition"):
        queue.operator_transition(
            "prop_1",
            ProposalStatus.APPLIED,
            operator_id="red",
            action_token="human-reviewed-token",
        )
