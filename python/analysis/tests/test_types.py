"""Tests for Decision dataclass + parsing."""
from datetime import datetime, timezone

from analysis.types import Decision, parse_decision_line


def test_parse_full_decision_line():
    line = (
        '{"ts":"2026-04-29T10:15:32Z","allowed":false,"mode":"enforce",'
        '"rule_id":"no-destructive-rm","reason":"matched destructive pattern",'
        '"escalation":false,"agent":"copilot-cli","action_type":"shell.exec",'
        '"action_target":"rm -rf /tmp/foo","envelope_id":"env_abc123",'
        '"tier":"T0","cost_usd":0.0,"input_bytes":42,"tool_calls":1}'
    )
    d = parse_decision_line(line)
    assert d is not None
    assert d.ts == datetime(2026, 4, 29, 10, 15, 32, tzinfo=timezone.utc)
    assert d.allowed is False
    assert d.rule_id == "no-destructive-rm"
    assert d.agent == "copilot-cli"
    assert d.action_type == "shell.exec"
    assert d.action_target == "rm -rf /tmp/foo"
    assert d.envelope_id == "env_abc123"


def test_parse_decision_with_missing_optional_fields():
    line = '{"ts":"2026-04-29T10:15:32Z","allowed":true,"mode":"enforce"}'
    d = parse_decision_line(line)
    assert d is not None
    assert d.allowed is True
    assert d.rule_id is None
    assert d.agent is None
    assert d.action_type is None


def test_parse_malformed_json_returns_none():
    assert parse_decision_line("not valid json") is None
    assert parse_decision_line("") is None
    assert parse_decision_line("{") is None


def test_parse_missing_ts_returns_none():
    line = '{"allowed":false,"rule_id":"x"}'
    assert parse_decision_line(line) is None


def test_parse_malformed_ts_returns_none():
    line = '{"ts":"not-a-timestamp","allowed":false}'
    assert parse_decision_line(line) is None


def test_decision_is_frozen():
    d = Decision(ts=datetime(2026, 1, 1, tzinfo=timezone.utc), allowed=True)
    import dataclasses
    try:
        d.allowed = False  # type: ignore[misc]
        raised = False
    except dataclasses.FrozenInstanceError:
        raised = True
    assert raised, "Decision must be frozen"
