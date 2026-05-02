"""Tests for the swarm-runs loader."""
from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path

import pytest

from analysis.loaders import Window
from analysis.swarm_runs import (
    SwarmRun,
    bucket_b_rate,
    cost_by_driver,
    load_swarm_runs,
    outcomes_by_driver,
)


# ─── fixture builders ────────────────────────────────────────────────────


def _write_marker(state_dir: Path, entry_id: str, *, workflow_id: str, tier: str, driver: str, dispatched_at: str) -> None:
    state_dir.mkdir(parents=True, exist_ok=True)
    (state_dir / f"{entry_id}.json").write_text(
        json.dumps(
            {
                "entry_id": entry_id,
                "workflow_id": workflow_id,
                "tier": tier,
                "driver": driver,
                "dispatched_at": dispatched_at,
            }
        )
    )


def _write_envelope(
    tmp_dir: Path,
    *,
    workflow_id: str,
    exit_code: int,
    duration_ms: int,
    commits_added: int,
    diff_shortstat: str = "",
    stdout_tail: str = "",
) -> None:
    tmp_dir.mkdir(parents=True, exist_ok=True)
    (tmp_dir / f"result-{workflow_id}.json").write_text(
        json.dumps(
            {
                "workflow_id": workflow_id,
                "result": {
                    "exit_code": exit_code,
                    "stdout_tail": stdout_tail,
                    "stderr_tail": "",
                    "duration_ms": duration_ms,
                    "worktree": {
                        "path": "/tmp/wt",
                        "branch": "swarm/test",
                        "head_sha": "abc12345",
                        "commits_added": commits_added,
                        "has_uncommitted_changes": False,
                        "diff_shortstat": diff_shortstat,
                    },
                },
                "pr_title": "swarm: test",
                "pr_body": "test",
            }
        )
    )


# ─── tests ───────────────────────────────────────────────────────────────


def test_returns_empty_when_state_dir_missing(tmp_path):
    """Missing state_dir must not raise — matches loaders.load_gov_decisions."""
    runs = load_swarm_runs(tmp_path / "no-state", tmp_path / "no-tmp", window=None)
    assert runs == []


def test_returns_empty_when_tmp_dir_missing(tmp_path):
    state_dir = tmp_path / "state"
    _write_marker(state_dir, "e1", workflow_id="swarm-1", tier="T0", driver="copilot", dispatched_at="2026-05-02T03:14:38Z")
    runs = load_swarm_runs(state_dir, tmp_path / "no-tmp", window=None)
    assert runs == []


def test_joins_marker_with_envelope_by_workflow_id(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    _write_marker(state, "e1", workflow_id="swarm-1", tier="T0", driver="copilot", dispatched_at="2026-05-02T03:14:38Z")
    _write_envelope(tmp, workflow_id="swarm-1", exit_code=0, duration_ms=24000, commits_added=1, diff_shortstat="2 files changed, 5 insertions(+)")

    runs = load_swarm_runs(state, tmp, window=None)

    assert len(runs) == 1
    r = runs[0]
    assert r.entry_id == "e1"
    assert r.tier == "T0"
    assert r.driver == "copilot"
    assert r.exit_code == 0
    assert r.duration_ms == 24000
    assert r.commits_added == 1
    assert r.diff_shortstat == "2 files changed, 5 insertions(+)"
    assert r.bucket_b is False
    assert r.cost_usd is None  # copilot leaves no cost in stdout_tail
    assert r.model is None


def test_skips_envelopes_without_matching_marker(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    _write_envelope(tmp, workflow_id="swarm-orphan", exit_code=0, duration_ms=1, commits_added=0)
    assert load_swarm_runs(state, tmp, window=None) == []


def test_window_excludes_runs_outside_range(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    _write_marker(state, "early", workflow_id="swarm-early", tier="T0", driver="copilot", dispatched_at="2026-05-02T02:00:00Z")
    _write_marker(state, "in", workflow_id="swarm-in", tier="T0", driver="copilot", dispatched_at="2026-05-02T04:00:00Z")
    _write_marker(state, "late", workflow_id="swarm-late", tier="T0", driver="copilot", dispatched_at="2026-05-02T06:00:00Z")
    for wf in ("swarm-early", "swarm-in", "swarm-late"):
        _write_envelope(tmp, workflow_id=wf, exit_code=0, duration_ms=1, commits_added=0)

    window = Window(
        since=datetime(2026, 5, 2, 3, 0, tzinfo=timezone.utc),
        until=datetime(2026, 5, 2, 5, 0, tzinfo=timezone.utc),
    )
    runs = load_swarm_runs(state, tmp, window=window)
    assert [r.entry_id for r in runs] == ["in"]


def test_extracts_cost_and_model_from_claude_stdout_tail(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    _write_marker(state, "e2", workflow_id="swarm-2", tier="T2", driver="claude-code-headless", dispatched_at="2026-05-02T03:54:06Z")
    # Mimic the claude CLI's final-message JSON snippet.
    fake_tail = (
        '{"stop_reason":"end_turn","total_cost_usd":0.37654,'
        '"modelUsage":{"claude-sonnet-4-6":{"inputTokens":20,"outputTokens":4072}}}'
    )
    _write_envelope(tmp, workflow_id="swarm-2", exit_code=0, duration_ms=111599, commits_added=0, diff_shortstat="", stdout_tail=fake_tail)

    runs = load_swarm_runs(state, tmp, window=None)
    assert runs[0].cost_usd == pytest.approx(0.37654)
    assert runs[0].model == "claude-sonnet-4-6"


def test_extracts_pr_url_from_stdout_tail(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    _write_marker(state, "e3", workflow_id="swarm-3", tier="T0", driver="copilot", dispatched_at="2026-05-02T03:14:38Z")
    fake_tail = "[apply-result] PR -> https://github.com/chitinhq/chitin/pull/103"
    _write_envelope(tmp, workflow_id="swarm-3", exit_code=0, duration_ms=24000, commits_added=1, stdout_tail=fake_tail)
    assert load_swarm_runs(state, tmp, window=None)[0].pr_url == "https://github.com/chitinhq/chitin/pull/103"


def test_bucket_b_signature_matches_writeWorktreeClaudeSettings_overwrite(tmp_path):
    """The byte-identical .claude/settings.json overwrite has shortstat
    '1 file changed, 12 insertions(+), 10 deletions(-)' with commits_added=1."""
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    _write_marker(state, "e4", workflow_id="swarm-4", tier="T2", driver="claude-code-headless", dispatched_at="2026-05-02T03:54:06Z")
    _write_envelope(
        tmp,
        workflow_id="swarm-4",
        exit_code=0,
        duration_ms=111599,
        commits_added=1,
        diff_shortstat="1 file changed, 12 insertions(+), 10 deletions(-)",
    )
    assert load_swarm_runs(state, tmp, window=None)[0].bucket_b is True


def test_bucket_b_does_not_match_other_one_file_changes(tmp_path):
    """A legitimate small PR that happens to be 1 file changed but with
    different insertion/deletion counts is NOT bucket_b."""
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    _write_marker(state, "e5", workflow_id="swarm-5", tier="T0", driver="copilot", dispatched_at="2026-05-02T03:14:38Z")
    _write_envelope(
        tmp,
        workflow_id="swarm-5",
        exit_code=0,
        duration_ms=24000,
        commits_added=1,
        diff_shortstat="1 file changed, 3 insertions(+), 1 deletion(-)",
    )
    assert load_swarm_runs(state, tmp, window=None)[0].bucket_b is False


def test_cost_by_driver_sums_per_driver(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    for i, (entry, driver, cost_in_tail) in enumerate(
        [
            ("e1", "claude-code-headless", '"total_cost_usd":0.10'),
            ("e2", "claude-code-headless", '"total_cost_usd":0.25'),
            ("e3", "copilot", ""),  # no cost reported
        ]
    ):
        wf = f"swarm-{i}"
        _write_marker(state, entry, workflow_id=wf, tier="T1", driver=driver, dispatched_at=f"2026-05-02T03:14:0{i}Z")
        _write_envelope(tmp, workflow_id=wf, exit_code=0, duration_ms=1000, commits_added=0, stdout_tail=cost_in_tail)

    runs = load_swarm_runs(state, tmp, window=None)
    costs = cost_by_driver(runs)
    assert costs["claude-code-headless"] == pytest.approx(0.35)
    assert costs["copilot"] == 0.0


def test_outcomes_by_driver_buckets_exit_codes(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    for i, (entry, exit_code) in enumerate([("ok", 0), ("partial", 1), ("timeout", -1), ("other", 2)]):
        wf = f"swarm-{i}"
        _write_marker(state, entry, workflow_id=wf, tier="T1", driver="copilot", dispatched_at=f"2026-05-02T03:14:0{i}Z")
        _write_envelope(tmp, workflow_id=wf, exit_code=exit_code, duration_ms=1, commits_added=0)

    by = outcomes_by_driver(load_swarm_runs(state, tmp, window=None))
    assert by["copilot"] == {"success": 1, "partial": 1, "timeout": 1, "other": 1}


def test_bucket_b_rate(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    bad = "1 file changed, 12 insertions(+), 10 deletions(-)"
    good = "2 files changed, 30 insertions(+), 4 deletions(-)"
    for i, shortstat in enumerate([bad, bad, good, good, good]):
        wf = f"swarm-{i}"
        _write_marker(state, f"e{i}", workflow_id=wf, tier="T2", driver="claude-code-headless", dispatched_at=f"2026-05-02T03:14:0{i}Z")
        _write_envelope(tmp, workflow_id=wf, exit_code=0, duration_ms=1, commits_added=1, diff_shortstat=shortstat)

    assert bucket_b_rate(load_swarm_runs(state, tmp, window=None)) == pytest.approx(2 / 5)


def test_bucket_b_rate_empty_returns_zero():
    assert bucket_b_rate([]) == 0.0


def test_corrupt_marker_skipped_not_raised(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    state.mkdir()
    (state / "corrupt.json").write_text("{not valid json")
    _write_marker(state, "e1", workflow_id="swarm-1", tier="T0", driver="copilot", dispatched_at="2026-05-02T03:14:38Z")
    _write_envelope(tmp, workflow_id="swarm-1", exit_code=0, duration_ms=1, commits_added=0)
    runs = load_swarm_runs(state, tmp, window=None)
    assert len(runs) == 1
    assert runs[0].entry_id == "e1"


def test_corrupt_envelope_skipped_not_raised(tmp_path):
    state, tmp = tmp_path / "state", tmp_path / "tmp"
    _write_marker(state, "e1", workflow_id="swarm-1", tier="T0", driver="copilot", dispatched_at="2026-05-02T03:14:38Z")
    tmp.mkdir()
    (tmp / "result-swarm-corrupt.json").write_text("{not valid json")
    _write_envelope(tmp, workflow_id="swarm-1", exit_code=0, duration_ms=1, commits_added=0)
    runs = load_swarm_runs(state, tmp, window=None)
    assert len(runs) == 1
