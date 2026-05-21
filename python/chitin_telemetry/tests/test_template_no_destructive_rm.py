"""Tests for the no-destructive-rm heuristic template."""
from datetime import datetime, timezone

from chitin_telemetry.templates.no_destructive_rm import draft
from chitin_telemetry.models import Decision, Pattern


def _pattern_with_targets(*targets):
    decisions = tuple(
        Decision(
            ts=datetime(2026, 4, 25, 8, i, tzinfo=timezone.utc),
            allowed=False,
            rule_id="no-destructive-rm",
            action_type="shell.exec",
            action_target=t,
            agent="copilot-cli",
            envelope_id=f"e{i}",
        )
        for i, t in enumerate(targets)
    )
    return Pattern(
        rule_id="no-destructive-rm",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=len(targets),
        first_seen=decisions[0].ts,
        last_seen=decisions[-1].ts,
        decision_class="deny",
        sample_envelope_ids=tuple(d.envelope_id for d in decisions[:3]),
        decisions=decisions,
    )


def test_safe_dirs_are_drafted_as_allow_exception():
    p = _pattern_with_targets(
        "rm -rf /tmp/cleanup",
        "rm -rf /test/output",
        "rm -rf out/build",
        "rm -rf graphify-out/wiki",
    )
    d = draft(p)
    assert d is not None
    assert d.kind == "heuristic"
    assert d.template == "no_destructive_rm"
    assert "no-destructive-rm-safe-dirs" in d.rule_yaml
    # Schema check: must use real chitin keys (action/effect/target_regex),
    # not made-up keys (when/decide/action_type).
    assert "action: shell.exec" in d.rule_yaml
    assert "effect: allow" in d.rule_yaml
    assert "target_regex:" in d.rule_yaml
    assert "when:" not in d.rule_yaml
    assert "decide:" not in d.rule_yaml
    assert d.predicted_impact.samples_evaluated == 4
    assert d.predicted_impact.would_allow == 4
    assert d.predicted_impact.would_still_deny == 0


def test_mixed_targets_split_correctly():
    p = _pattern_with_targets(
        "rm -rf /tmp/cleanup",
        "rm -rf /etc/passwd",
        "rm -rf /home/user/important",
    )
    d = draft(p)
    assert d is not None
    assert d.predicted_impact.would_allow == 1
    assert d.predicted_impact.would_still_deny == 2


def test_pattern_with_no_safe_targets_returns_none():
    p = _pattern_with_targets(
        "rm -rf /etc/passwd",
        "rm -rf /home/user/important",
    )
    assert draft(p) is None


def test_pattern_with_no_decisions_returns_none():
    p = Pattern(
        rule_id="no-destructive-rm",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=0,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=(),
        decisions=(),
    )
    assert draft(p) is None
