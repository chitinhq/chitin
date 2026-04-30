"""Tests for markdown projection writer."""
import json
from pathlib import Path

from analysis.writers import write_markdown_from_json


def _sample_json(tmp_path: Path) -> Path:
    p = tmp_path / "decisions-2026-04-30.json"
    body = {
        "schema_version": "1",
        "stream": "decisions",
        "generated_at": "2026-04-30T12:00:00+00:00",
        "window": {
            "size": "7d",
            "since": "2026-04-23T12:00:00+00:00",
            "until": "2026-04-30T12:00:00+00:00",
            "total_seconds": 604800,
        },
        "input_summary": {
            "total_decisions": 1225, "denies": 62, "allows": 1163,
            "files_read": 6, "parse_errors": 0, "distinct_rule_ids": 14,
        },
        "patterns": [
            {
                "rank": 1,
                "rule_id": "no-destructive-rm",
                "action_type": "shell.exec",
                "agent_id": "copilot-cli",
                "count": 19,
                "first_seen": "2026-04-23T08:00:00+00:00",
                "last_seen": "2026-04-30T11:00:00+00:00",
                "decision_class": "deny",
                "sample_envelope_ids": ["env_001", "env_002", "env_003"],
                "draft": {
                    "kind": "heuristic",
                    "template": "no_destructive_rm",
                    "confidence": "medium",
                    "rule_yaml": "rules:\n  - id: x\n",
                    "predicted_impact": {
                        "samples_evaluated": 19, "would_allow": 12,
                        "would_still_deny": 7, "method": "regex",
                    },
                    "notes": "n",
                },
            }
        ],
        "no_template_patterns": [
            {"rule_id": "envelope-exhausted", "action_type": "?",
             "agent_id": "?", "count": 2,
             "reason_no_template": "structural"}
        ],
    }
    p.write_text(json.dumps(body, indent=2, sort_keys=True))
    return p


def test_markdown_renders_top_pattern(tmp_path):
    json_path = _sample_json(tmp_path)
    md_path = tmp_path / "decisions-2026-04-30.md"
    write_markdown_from_json(json_path, md_path)
    md = md_path.read_text()
    assert "# Decisions Analysis — 2026-04-30" in md
    assert "1225 decisions" in md
    assert "no-destructive-rm" in md
    assert "19 denies" in md
    assert "samples_evaluated: 19" in md
    assert "would_allow: 12" in md
    assert "rules:" in md


def test_markdown_renders_no_template_section(tmp_path):
    json_path = _sample_json(tmp_path)
    md_path = tmp_path / "out.md"
    write_markdown_from_json(json_path, md_path)
    md = md_path.read_text()
    assert "envelope-exhausted" in md


def test_markdown_deterministic(tmp_path):
    json_path = _sample_json(tmp_path)
    a = tmp_path / "a.md"
    b = tmp_path / "b.md"
    write_markdown_from_json(json_path, a)
    write_markdown_from_json(json_path, b)
    assert a.read_bytes() == b.read_bytes()


def test_markdown_surfaces_missing_envelope_id(tmp_path):
    p = tmp_path / "missing.json"
    body = {
        "schema_version": "1",
        "stream": "decisions",
        "generated_at": "2026-04-30T12:00:00+00:00",
        "window": {"size": "7d", "since": "2026-04-23T00:00:00+00:00",
                   "until": "2026-04-30T12:00:00+00:00", "total_seconds": 649800},
        "input_summary": {"total_decisions": 100, "denies": 5, "allows": 95,
                          "files_read": 1, "parse_errors": 0,
                          "distinct_rule_ids": 3,
                          "decisions_missing_envelope_id": 17},
        "patterns": [],
        "no_template_patterns": [],
    }
    p.write_text(json.dumps(body, sort_keys=True))
    md_path = tmp_path / "missing.md"
    write_markdown_from_json(p, md_path)
    md = md_path.read_text()
    assert "17 missing envelope_id" in md


def test_markdown_with_no_patterns(tmp_path):
    p = tmp_path / "empty.json"
    body = {
        "schema_version": "1",
        "stream": "decisions",
        "generated_at": "2026-04-30T12:00:00+00:00",
        "window": {"size": "7d", "since": "2026-04-23T00:00:00+00:00",
                   "until": "2026-04-30T12:00:00+00:00",
                   "total_seconds": 649800},
        "input_summary": {"total_decisions": 0, "denies": 0, "allows": 0,
                          "files_read": 0, "parse_errors": 0,
                          "distinct_rule_ids": 0},
        "patterns": [],
        "no_template_patterns": [],
    }
    p.write_text(json.dumps(body, sort_keys=True))
    md_path = tmp_path / "empty.md"
    write_markdown_from_json(p, md_path)
    md = md_path.read_text()
    assert "No deny patterns" in md
