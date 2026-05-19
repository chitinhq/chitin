import json
import sqlite3
import subprocess
import sys
from pathlib import Path

from analysis import analyzer
from analysis.loaders import load_gov_decisions, parse_window_str


FIXTURE = """\
{"ts":"2026-05-15T08:00:00Z","allowed":false,"mode":"enforce","rule_id":"no-destructive-rm","agent":"claude-code","action_type":"shell.exec","action_target":"rm -rf build/tmp","envelope_id":"env_a","workflow_id":"t_alpha","cost_usd":1.20}
{"ts":"2026-05-15T08:01:00Z","allowed":false,"mode":"enforce","rule_id":"no-destructive-rm","agent":"claude-code","action_type":"shell.exec","action_target":"rm -rf dist/tmp","envelope_id":"env_a","workflow_id":"t_alpha","cost_usd":1.10}
{"ts":"2026-05-15T08:02:00Z","allowed":false,"mode":"enforce","rule_id":"no-destructive-rm","agent":"claude-code","action_type":"shell.exec","action_target":"rm -rf cache/tmp","envelope_id":"env_a","workflow_id":"t_alpha","cost_usd":1.30}
{"ts":"2026-05-15T08:03:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-writes","agent":"claude-code","action_type":"file.write","action_target":"apps/x.ts","envelope_id":"env_a","workflow_id":"t_alpha","cost_usd":1.40}
{"ts":"2026-05-15T08:04:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-writes","agent":"claude-code","action_type":"file.write","action_target":"apps/y.ts","envelope_id":"env_a","workflow_id":"t_alpha","cost_usd":1.35}
{"ts":"2026-05-15T08:05:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-writes","agent":"claude-code","action_type":"file.write","action_target":"apps/z.ts","envelope_id":"env_a","workflow_id":"t_alpha","cost_usd":1.25}
{"ts":"2026-05-15T08:06:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-shell","agent":"claude-code","action_type":"shell.exec","action_target":"go test ./...","envelope_id":"env_a","workflow_id":"t_alpha","cost_usd":1.50}
{"ts":"2026-05-15T08:07:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-shell","agent":"claude-code","action_type":"shell.exec","action_target":"go test ./...","envelope_id":"env_a","workflow_id":"t_alpha","cost_usd":1.45}
{"ts":"2026-05-15T10:00:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-shell","agent":"codex","action_type":"shell.exec","action_target":"npm test","envelope_id":"env_b","workflow_id":"t_beta","cost_usd":0.80}
{"ts":"2026-05-15T10:01:00Z","allowed":true,"mode":"enforce","rule_id":"default-allow-read","agent":"codex","action_type":"file.read","action_target":"README.md","envelope_id":"env_b","workflow_id":"t_beta","cost_usd":0.20}
"""


def _write_fixture(tmp_path: Path) -> Path:
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    (decisions_dir / "gov-decisions-2026-05-15.jsonl").write_text(FIXTURE)
    return decisions_dir


def _write_policy(tmp_path: Path) -> Path:
    policy = tmp_path / "chitin.yaml"
    policy.write_text(
        "mode: enforce\n"
        "rules:\n"
        "  - id: no-destructive-rm\n"
        "  - id: stale-rule-no-fire\n"
    )
    return policy


def test_build_suggestions_covers_rubric_types(tmp_path):
    decisions_dir = _write_fixture(tmp_path)
    policy = _write_policy(tmp_path)
    now = analyzer._now_from_args("2026-05-16T12:00:00+00:00")
    window = parse_window_str("48h", now)
    decisions = load_gov_decisions(decisions_dir, window).decisions

    suggestions = analyzer.build_suggestions(decisions, policy, now, top_n=25)
    types = {item.type for item in suggestions}

    assert "policy_rule" in types
    assert "new_skill" in types
    assert "route_tweak" in types


def test_detect_stale_rules_produces_drop(tmp_path):
    decisions_dir = _write_fixture(tmp_path)
    policy = _write_policy(tmp_path)
    now = analyzer._now_from_args("2026-05-16T12:00:00+00:00")
    window = parse_window_str("30d", now)
    decisions = load_gov_decisions(decisions_dir, window).decisions

    drops = analyzer.detect_stale_rules(policy, decisions, now)

    assert any(item.type == "drop" and item.target == "stale-rule-no-fire" for item in drops)


def test_write_suggestions_creates_sqlite_rows(tmp_path):
    db_path = tmp_path / "analyzer.db"
    conn = analyzer.open_db(db_path)
    suggestion = analyzer.Suggestion(
        type="prompt_edit",
        target="claude-code:no-destructive-rm",
        diff="target: scripts/hermes/prompt-code.md",
        rationale="Repeated deny pattern.",
        created_at="2026-05-16T12:00:00+00:00",
        confidence=0.8,
    )
    analyzer.write_suggestions(conn, [suggestion])
    row = conn.execute(
        "SELECT id, type, target, diff, rationale, applied, created_at FROM analyzer_suggestions"
    ).fetchone()
    conn.close()

    assert row["id"] == suggestion.id
    assert row["type"] == "prompt_edit"
    assert row["applied"] == 0


def test_cli_writes_db_and_summary(tmp_path):
    decisions_dir = _write_fixture(tmp_path)
    policy = _write_policy(tmp_path)
    db_path = tmp_path / "analyzer.db"

    result = subprocess.run(
        [
            sys.executable,
            "-m",
            "analysis.analyzer",
            "--window",
            "48h",
            "--decisions-dir",
            str(decisions_dir),
            "--db-path",
            str(db_path),
            "--policy-file",
            str(policy),
            "--now",
            "2026-05-16T12:00:00+00:00",
            "--skip-llm",
        ],
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr
    payload = json.loads(result.stdout)
    assert payload["suggestions_written"] >= 1

    conn = sqlite3.connect(db_path)
    row = conn.execute("SELECT COUNT(*) FROM analyzer_suggestions").fetchone()
    conn.close()
    assert row[0] >= 1
