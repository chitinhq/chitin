"""Tests for pattern detection."""
from datetime import datetime, timezone

from analysis.detect import detect_patterns
from analysis.types import Decision


def _decision(ts="2026-04-25T08:00:00Z", allowed=False, rule_id="r1",
              action_type="shell.exec", agent="copilot-cli", envelope_id="e1"):
    return Decision(
        ts=datetime.fromisoformat(ts.replace("Z", "+00:00")),
        allowed=allowed,
        rule_id=rule_id,
        action_type=action_type,
        agent=agent,
        envelope_id=envelope_id,
    )


def test_empty_input_returns_empty():
    assert detect_patterns([]) == []


def test_single_decision_one_pattern():
    patterns = detect_patterns([_decision()])
    assert len(patterns) == 1
    assert patterns[0].count == 1
    assert patterns[0].rule_id == "r1"


def test_all_allows_returns_empty():
    decisions = [_decision(allowed=True, envelope_id=f"e{i}") for i in range(5)]
    assert detect_patterns(decisions) == []


def test_three_same_pattern():
    decisions = [_decision(envelope_id=f"e{i}") for i in range(3)]
    patterns = detect_patterns(decisions)
    assert len(patterns) == 1
    assert patterns[0].count == 3


def test_count_descending_ranking():
    decisions = [
        _decision(rule_id="rule_a", envelope_id="e1"),
        _decision(rule_id="rule_a", envelope_id="e2"),
        _decision(rule_id="rule_a", envelope_id="e3"),
        *[_decision(rule_id="rule_b", envelope_id=f"eb{i}") for i in range(5)],
        _decision(rule_id="rule_c", envelope_id="ec1"),
    ]
    patterns = detect_patterns(decisions)
    assert [p.count for p in patterns] == [5, 3, 1]
    assert [p.rule_id for p in patterns] == ["rule_b", "rule_a", "rule_c"]


def test_tie_breaker_alphabetic_on_rule_id():
    decisions = [
        _decision(rule_id="zeta", envelope_id="e1"),
        _decision(rule_id="zeta", envelope_id="e2"),
        _decision(rule_id="alpha", envelope_id="ea1"),
        _decision(rule_id="alpha", envelope_id="ea2"),
    ]
    patterns = detect_patterns(decisions)
    assert [p.rule_id for p in patterns] == ["alpha", "zeta"]


def test_tie_breaker_secondary_on_action_type():
    decisions = [
        _decision(rule_id="r", action_type="zzz", envelope_id="e1"),
        _decision(rule_id="r", action_type="aaa", envelope_id="e2"),
    ]
    patterns = detect_patterns(decisions)
    assert [p.action_type for p in patterns] == ["aaa", "zzz"]


def test_tie_breaker_tertiary_on_agent():
    decisions = [
        _decision(rule_id="r", action_type="t", agent="zzz", envelope_id="e1"),
        _decision(rule_id="r", action_type="t", agent="aaa", envelope_id="e2"),
    ]
    patterns = detect_patterns(decisions)
    assert [p.agent_id for p in patterns] == ["aaa", "zzz"]


def test_null_rule_id_bucketed_as_none():
    decisions = [_decision(rule_id=None, envelope_id="e1")]
    patterns = detect_patterns(decisions)
    assert patterns[0].rule_id == "<none>"


def test_null_agent_bucketed_as_unknown():
    decisions = [_decision(agent=None, envelope_id="e1")]
    patterns = detect_patterns(decisions)
    assert patterns[0].agent_id == "<unknown>"


def test_first_last_seen_correct():
    decisions = [
        _decision(ts="2026-04-25T12:00:00Z", envelope_id="e1"),
        _decision(ts="2026-04-25T08:00:00Z", envelope_id="e2"),
        _decision(ts="2026-04-25T15:00:00Z", envelope_id="e3"),
    ]
    patterns = detect_patterns(decisions)
    assert patterns[0].first_seen == datetime(2026, 4, 25, 8, 0, tzinfo=timezone.utc)
    assert patterns[0].last_seen == datetime(2026, 4, 25, 15, 0, tzinfo=timezone.utc)


def test_sample_envelope_ids_are_first_three():
    decisions = [_decision(envelope_id=f"env_{i:03d}") for i in range(5)]
    patterns = detect_patterns(decisions)
    assert patterns[0].sample_envelope_ids == ("env_000", "env_001", "env_002")


def test_determinism_two_runs_byte_equal():
    decisions = [
        _decision(rule_id="b", envelope_id="e1"),
        _decision(rule_id="a", envelope_id="e2"),
        _decision(rule_id="b", envelope_id="e3"),
    ]
    a = detect_patterns(decisions)
    b = detect_patterns(decisions)
    assert a == b
