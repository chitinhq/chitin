"""Tests for JSON canonical writer."""
import json
from datetime import datetime, timezone

from analysis.types import Pattern, PredictedImpact, RuleDraft
from analysis.writers import build_finding, write_json


def _pattern():
    return Pattern(
        rule_id="r1",
        action_type="shell.exec",
        agent_id="copilot-cli",
        count=3,
        first_seen=datetime(2026, 4, 25, 8, 0, tzinfo=timezone.utc),
        last_seen=datetime(2026, 4, 25, 9, 0, tzinfo=timezone.utc),
        decision_class="deny",
        sample_envelope_ids=("e1", "e2", "e3"),
        decisions=(),
    )


def _draft():
    return RuleDraft(
        kind="heuristic",
        template="t1",
        confidence="medium",
        rule_yaml="rules: []\n",
        predicted_impact=PredictedImpact(
            samples_evaluated=3, would_allow=2, would_still_deny=1, method="m"
        ),
        notes="n",
    )


def test_build_finding_pairs_pattern_with_draft():
    f = build_finding(_pattern(), _draft(), rank=1)
    assert f.rank == 1
    assert f.pattern.rule_id == "r1"
    assert f.draft is not None


def test_build_finding_with_no_draft():
    f = build_finding(_pattern(), None, rank=2)
    assert f.draft is None


def test_write_json_produces_deterministic_output(tmp_path):
    findings = [build_finding(_pattern(), _draft(), rank=1)]
    summary = {"total_decisions": 1225, "denies": 62, "allows": 1163,
               "files_read": 6, "parse_errors": 0,
               "distinct_rule_ids": 14}
    now = datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc)
    window_since = datetime(2026, 4, 23, 12, 0, tzinfo=timezone.utc)

    out = tmp_path / "out.json"
    write_json(out, findings=findings, no_template=[], input_summary=summary,
               generated_at=now, window_since=window_since,
               window_until=now, window_size="7d")
    a = out.read_bytes()

    out2 = tmp_path / "out2.json"
    write_json(out2, findings=findings, no_template=[], input_summary=summary,
               generated_at=now, window_since=window_since,
               window_until=now, window_size="7d")
    b = out2.read_bytes()

    assert a == b
    parsed = json.loads(a)
    assert parsed["schema_version"] == "1"
    assert parsed["stream"] == "decisions"
    assert parsed["window"]["size"] == "7d"
    assert parsed["window"]["total_seconds"] == 7 * 86400
    assert parsed["input_summary"]["total_decisions"] == 1225
    assert len(parsed["patterns"]) == 1
    assert parsed["patterns"][0]["rank"] == 1
    assert parsed["patterns"][0]["draft"]["predicted_impact"]["would_allow"] == 2


def test_write_json_handles_empty_findings(tmp_path):
    out = tmp_path / "empty.json"
    now = datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc)
    write_json(out, findings=[], no_template=[],
               input_summary={"total_decisions": 0, "denies": 0, "allows": 0,
                              "files_read": 0, "parse_errors": 0,
                              "distinct_rule_ids": 0},
               generated_at=now, window_since=now, window_until=now, window_size="7d")
    parsed = json.loads(out.read_bytes())
    assert parsed["patterns"] == []
    assert parsed["no_template_patterns"] == []


def test_write_json_records_sub_day_windows_precisely(tmp_path):
    """Regression: --window 60m must not record window.size as '1d'."""
    out = tmp_path / "subday.json"
    now = datetime(2026, 4, 30, 12, 0, tzinfo=timezone.utc)
    since = datetime(2026, 4, 30, 11, 0, tzinfo=timezone.utc)
    write_json(out, findings=[], no_template=[],
               input_summary={"total_decisions": 0, "denies": 0, "allows": 0,
                              "files_read": 0, "parse_errors": 0,
                              "distinct_rule_ids": 0},
               generated_at=now, window_since=since,
               window_until=now, window_size="60m")
    parsed = json.loads(out.read_bytes())
    assert parsed["window"]["size"] == "60m"
    assert parsed["window"]["total_seconds"] == 3600


def test_write_json_creates_parent_dirs(tmp_path):
    out = tmp_path / "subdir" / "nested" / "out.json"
    now = datetime(2026, 4, 30, tzinfo=timezone.utc)
    write_json(out, findings=[], no_template=[],
               input_summary={"total_decisions": 0, "denies": 0, "allows": 0,
                              "files_read": 0, "parse_errors": 0,
                              "distinct_rule_ids": 0},
               generated_at=now, window_since=now, window_until=now, window_size="7d")
    assert out.exists()
