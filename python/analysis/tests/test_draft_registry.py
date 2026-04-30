"""Tests for the template registry and draft dispatch."""
from datetime import datetime, timezone

from analysis.draft import draft_for_pattern, reason_no_template
from analysis.types import Pattern


def _pattern(rule_id="unknown_rule", count=5):
    return Pattern(
        rule_id=rule_id,
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=count,
        first_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=("e1", "e2", "e3"),
        decisions=(),
    )


def test_unknown_rule_id_returns_none():
    assert draft_for_pattern(_pattern(rule_id="totally_unknown")) is None


def test_registry_lookup_by_rule_id():
    from analysis.templates import REGISTRY
    assert isinstance(REGISTRY, dict)


def test_reason_no_template_for_unknown():
    msg = reason_no_template(_pattern(rule_id="totally_unknown"))
    assert "no template registered" in msg
    assert "totally_unknown" in msg
