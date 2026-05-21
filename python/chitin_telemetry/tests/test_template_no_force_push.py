"""Tests for the no-force-push heuristic template."""
from datetime import datetime, timezone

from chitin_telemetry.templates.no_force_push import draft
from chitin_telemetry.models import Decision, Pattern


def _pattern(*targets):
    decisions = tuple(
        Decision(
            ts=datetime(2026, 4, 25, 8, i, tzinfo=timezone.utc),
            allowed=False,
            rule_id="no-force-push",
            action_type="git.force-push",
            action_target=t,
            envelope_id=f"e{i}",
        )
        for i, t in enumerate(targets)
    )
    return Pattern(
        rule_id="no-force-push",
        action_type="git.force-push",
        agent_id="copilot-cli",
        count=len(targets),
        first_seen=decisions[0].ts,
        last_seen=decisions[-1].ts,
        decision_class="deny",
        sample_envelope_ids=tuple(d.envelope_id for d in decisions[:3]),
        decisions=decisions,
    )


def test_personal_branches_drafted():
    p = _pattern("feat/foo", "fix/bar", "spike/x")
    d = draft(p)
    assert d is not None
    assert "no-force-push-feature-branches" in d.rule_yaml
    # Schema check: real chitin keys.
    assert "action: git.force-push" in d.rule_yaml
    assert "effect: allow" in d.rule_yaml
    assert "when:" not in d.rule_yaml
    assert d.predicted_impact.would_allow == 3


def test_main_still_denied():
    p = _pattern("main", "master")
    assert draft(p) is None


def test_mixed():
    p = _pattern("feat/foo", "main", "fix/bar")
    d = draft(p)
    assert d is not None
    assert d.predicted_impact.would_allow == 2
    assert d.predicted_impact.would_still_deny == 1


def test_empty_pattern_returns_none():
    p = Pattern(
        rule_id="no-force-push",
        action_type="git.force-push",
        agent_id="copilot-cli",
        count=0,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=(),
        decisions=(),
    )
    assert draft(p) is None
