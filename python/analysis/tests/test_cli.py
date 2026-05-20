"""Tests for the decisions CLI entry."""
import json
import subprocess
import sys
from pathlib import Path


def _stage_fixture(decisions_dir: Path, fixtures_dir: Path):
    src = fixtures_dir / "gov-decisions-fixture.jsonl"
    (decisions_dir / "gov-decisions-2026-04-25.jsonl").write_text(src.read_text())


def _stage_repeated_deny_fixture(decisions_dir: Path, count: int):
    lines = [
        json.dumps(
            {
                "ts": f"2026-04-25T08:{i:02d}:00Z",
                "allowed": False,
                "mode": "enforce",
                "rule_id": "no-destructive-rm",
                "reason": "destructive",
                "agent": "copilot-cli",
                "action_type": "shell.exec",
                "action_target": f"rm -rf /tmp/{i}",
                "envelope_id": f"env_{i:03d}",
            }
        )
        for i in range(count)
    ]
    (decisions_dir / "gov-decisions-2026-04-25.jsonl").write_text("\n".join(lines))


def test_cli_runs_end_to_end_on_fixture(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)
    out_dir = tmp_path / "out"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.decisions",
         "--window", "100d",
         "--top-n", "10",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr

    out_files = sorted(out_dir.iterdir())
    json_files = [f for f in out_files if f.suffix == ".json"]
    md_files = [f for f in out_files if f.suffix == ".md"]
    assert len(json_files) == 1
    assert len(md_files) == 1

    body = json.loads(json_files[0].read_text())
    assert body["stream"] == "decisions"
    rule_ids = {p["rule_id"] for p in body["patterns"]}
    assert "no-destructive-rm" in rule_ids
    # New schema (post-review): window stores size + total_seconds, not days.
    assert body["window"]["size"] == "100d"
    assert body["window"]["total_seconds"] == 100 * 86400
    # New summary metric: decisions missing envelope_id.
    assert "decisions_missing_envelope_id" in body["input_summary"]


def test_cli_handles_missing_decisions_dir(tmp_path):
    result = subprocess.run(
        [sys.executable, "-m", "analysis.decisions",
         "--window", "7d",
         "--out-dir", str(tmp_path / "out"),
         "--decisions-dir", str(tmp_path / "does-not-exist"),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 2
    assert "does not exist" in result.stderr.lower()


def test_cli_empty_window_succeeds(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)

    result = subprocess.run(
        [sys.executable, "-m", "analysis.decisions",
         "--window", "1m",
         "--out-dir", str(tmp_path / "out"),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0
    body = json.loads(list((tmp_path / "out").glob("*.json"))[0].read_text())
    assert body["patterns"] == []


def test_cli_deterministic_with_fixed_now(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)

    def run(out_dir):
        return subprocess.run(
            [sys.executable, "-m", "analysis.decisions",
             "--window", "100d",
             "--out-dir", str(out_dir),
             "--decisions-dir", str(decisions_dir),
             "--now", "2026-04-30T12:00:00+00:00"],
            capture_output=True, text=True, check=True,
        )

    a_dir = tmp_path / "a"
    b_dir = tmp_path / "b"
    run(a_dir)
    run(b_dir)
    a = list(a_dir.glob("*.json"))[0].read_bytes()
    b = list(b_dir.glob("*.json"))[0].read_bytes()
    assert a == b


def test_sentinel_cli_outputs_candidate_invariant_proposals(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)
    out_dir = tmp_path / "out"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.sentinel",
         "--window", "100d",
         "--top-n", "5",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr

    body = json.loads(list(out_dir.glob("*.json"))[0].read_text())
    assert body["stream"] == "sentinel"
    assert body["patterns"]
    proposals = body["metadata"]["promotion"]["proposals"]
    assert body["metadata"]["promotion"]["proposal_path"] == "chitin.yaml"
    assert proposals
    assert proposals[0]["proposal_path"] == "chitin.yaml"
    assert proposals[0]["confidence"] in {"medium", "high", "low"}
    assert body["patterns"][0]["draft"]["confidence"]


def test_sentinel_empty_boundary_writes_no_candidate_proposals(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)
    out_dir = tmp_path / "out"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.sentinel",
         "--window", "1m",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr

    body = json.loads(list(out_dir.glob("sentinel-*.json"))[0].read_text())
    assert body["patterns"] == []
    assert body["metadata"]["promotion"]["proposal_count"] == 0
    assert body["metadata"]["promotion"]["status"] == "no-candidate"


def test_sentinel_max_boundary_limits_patterns_to_top_n(tmp_path, fixtures_dir):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_fixture(decisions_dir, fixtures_dir)
    out_dir = tmp_path / "out"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.sentinel",
         "--window", "100d",
         "--top-n", "1",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 0, result.stderr

    body = json.loads(list(out_dir.glob("sentinel-*.json"))[0].read_text())
    assert len(body["patterns"]) == 1
    assert body["patterns"][0]["rank"] == 1
    assert any(
        p["reason_no_template"] == "below top-N cutoff"
        for p in body["no_template_patterns"]
    )


def test_sentinel_error_boundary_rejects_missing_decisions_dir(tmp_path):
    result = subprocess.run(
        [sys.executable, "-m", "analysis.sentinel",
         "--window", "7d",
         "--out-dir", str(tmp_path / "out"),
         "--decisions-dir", str(tmp_path / "does-not-exist"),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )
    assert result.returncode == 2
    assert "does not exist" in result.stderr.lower()


def test_sentinel_threshold_above_enters_review_queue(tmp_path):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_repeated_deny_fixture(decisions_dir, 7)
    out_dir = tmp_path / "out"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.sentinel",
         "--window", "100d",
         "--top-n", "5",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )

    assert result.returncode == 0, result.stderr
    body = json.loads(list(out_dir.glob("sentinel-*.json"))[0].read_text())
    proposal = body["metadata"]["promotion"]["proposals"][0]
    assert proposal["threshold_status"] == "above-threshold"
    assert proposal["status"] == "proposed"
    assert proposal["attribution"]["spec_provenance"] == "spec:062-attribution TBD"
    assert proposal["attribution"]["sentinel_source"]
    assert proposal["evidence"]["build_grounding"] == "spec:063-build TBD"
    assert body["metadata"]["promotion"]["review_queue_count"] == 1


def test_sentinel_threshold_below_stays_out_of_review_queue(tmp_path):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_repeated_deny_fixture(decisions_dir, 2)
    out_dir = tmp_path / "out"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.sentinel",
         "--window", "100d",
         "--top-n", "5",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )

    assert result.returncode == 0, result.stderr
    body = json.loads(list(out_dir.glob("sentinel-*.json"))[0].read_text())
    proposal = body["metadata"]["promotion"]["proposals"][0]
    assert proposal["threshold_status"] == "below-threshold"
    assert proposal["status"] == "below-threshold"
    assert body["metadata"]["promotion"]["review_queue_count"] == 0


def test_sentinel_threshold_from_config_clamps_below_floor(tmp_path):
    decisions_dir = tmp_path / "decisions"
    decisions_dir.mkdir()
    _stage_repeated_deny_fixture(decisions_dir, 2)
    config = tmp_path / "chitin.yaml"
    config.write_text("sentinel:\n  promotion_threshold: 1\n")
    out_dir = tmp_path / "out"

    result = subprocess.run(
        [sys.executable, "-m", "analysis.sentinel",
         "--window", "100d",
         "--top-n", "5",
         "--out-dir", str(out_dir),
         "--decisions-dir", str(decisions_dir),
         "--config", str(config),
         "--now", "2026-04-30T12:00:00+00:00"],
        capture_output=True, text=True,
    )

    assert result.returncode == 0, result.stderr
    assert "clamped to 3" in result.stderr
    body = json.loads(list(out_dir.glob("sentinel-*.json"))[0].read_text())
    assert body["metadata"]["promotion"]["min_evidence_threshold"] == 3
    assert body["metadata"]["promotion"]["proposals"][0]["status"] == "below-threshold"
