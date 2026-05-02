"""Tests for the swarm-daily-rollup health module."""
from __future__ import annotations

import json
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from analysis.loaders import Window
from analysis.swarm_health import (
    SHORT_RUN_MS,
    SUCCESS_ALARM_PCT,
    _failure_mode,
    _parse_window,
    format_slack,
    generate_rollup,
    main,
    rollup_to_dict,
)
from analysis.swarm_runs import SwarmRun


# ─── helpers ─────────────────────────────────────────────────────────────


def make_run(
    *,
    entry_id: str = "e1",
    tier: str = "T0",
    driver: str = "copilot",
    dispatched_at: datetime = datetime(2026, 5, 2, 4, 0, tzinfo=timezone.utc),
    exit_code: int = 0,
    duration_ms: int = 30_000,
    commits_added: int = 1,
    diff_shortstat: str = "1 file changed, 3 insertions(+)",
    pr_url: str | None = None,
    cost_usd: float | None = None,
    model: str | None = None,
    bucket_b: bool = False,
) -> SwarmRun:
    return SwarmRun(
        entry_id=entry_id, tier=tier, driver=driver, dispatched_at=dispatched_at,
        exit_code=exit_code, duration_ms=duration_ms, commits_added=commits_added,
        diff_shortstat=diff_shortstat, pr_url=pr_url, cost_usd=cost_usd, model=model,
        bucket_b=bucket_b,
    )


SINCE = datetime(2026, 5, 2, 0, 0, tzinfo=timezone.utc)
UNTIL = datetime(2026, 5, 3, 0, 0, tzinfo=timezone.utc)


# ─── _failure_mode tie-breaker (Copilot R-L143 + R-L202) ─────────────────


def test_failure_mode_prioritizes_contamination_over_timeout():
    run = make_run(bucket_b=True, exit_code=-1, duration_ms=1, commits_added=0)
    assert _failure_mode(run) == "contamination"


def test_failure_mode_timeout_for_exit_minus_one():
    run = make_run(exit_code=-1, duration_ms=1_800_000, commits_added=0, bucket_b=False)
    assert _failure_mode(run) == "timeout"


def test_failure_mode_short_run_when_under_15s_no_commits():
    run = make_run(exit_code=0, duration_ms=8_000, commits_added=0, bucket_b=False)
    assert _failure_mode(run) == "short-run-no-work"


def test_failure_mode_uses_actual_exit_code_not_partial_label():
    """Copilot R-L202: pre-fix, any non-zero exit was labelled
    'exit_code=1-partial', which is wrong for exit codes other than 1.
    The fix uses the actual numeric exit code."""
    run = make_run(exit_code=2, duration_ms=60_000, commits_added=0, bucket_b=False)
    assert _failure_mode(run) == "exit_code=2"

    run_137 = make_run(exit_code=137, duration_ms=60_000, commits_added=0, bucket_b=False)
    assert _failure_mode(run_137) == "exit_code=137"


def test_failure_mode_no_work_when_clean_exit_no_commits():
    run = make_run(exit_code=0, duration_ms=60_000, commits_added=0, bucket_b=False)
    assert _failure_mode(run) == "no-work-produced"


# ─── generate_rollup alarms ──────────────────────────────────────────────


def test_rollup_no_alarms_on_healthy_runs():
    # Tier T1 so we don't trip the QWEN IDLE alarm (that fires only on
    # T0 routed-away-from-local-qwen, which copilot-on-T0 always is).
    runs = [
        make_run(entry_id=f"e{i}", driver="copilot", tier="T1", commits_added=1)
        for i in range(5)
    ]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    assert report.alarms == []
    assert report.bucket_b_count == 0
    assert report.bucket_b_rate == 0.0
    assert report.success_by_driver["copilot"] == {"success": 5, "total": 5}


def test_rollup_fires_bucket_b_alarm_on_any_contamination():
    runs = [
        make_run(entry_id="ok", commits_added=1),
        make_run(entry_id="bad", commits_added=1, bucket_b=True),
    ]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    assert report.bucket_b_count == 1
    assert any("BUCKET-B REGRESSION" in a for a in report.alarms)


def test_rollup_low_success_alarm_per_driver():
    """Copilot's L213: alarm-condition coverage. < 70% triggers LOW SUCCESS."""
    runs = [
        make_run(entry_id="s1", driver="cch", tier="T2", commits_added=1),
        make_run(entry_id="f1", driver="cch", tier="T2", exit_code=1, duration_ms=60_000, commits_added=0),
        make_run(entry_id="f2", driver="cch", tier="T2", exit_code=1, duration_ms=60_000, commits_added=0),
        make_run(entry_id="f3", driver="cch", tier="T2", exit_code=1, duration_ms=60_000, commits_added=0),
    ]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    # 1/4 = 25% < 70% → alarm
    assert any("LOW SUCCESS: driver=cch" in a for a in report.alarms)


def test_rollup_high_short_run_alarm():
    runs = [
        make_run(entry_id=f"sr{i}", driver="cch", tier="T2",
                 exit_code=0, duration_ms=5_000, commits_added=0)  # 5s, no commits
        for i in range(3)
    ] + [
        make_run(entry_id="ok", driver="cch", tier="T2", commits_added=1, duration_ms=120_000),
    ]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    # 3/4 = 75% short-run rate > 25% → alarm
    assert any("HIGH SHORT-RUN: driver=cch" in a for a in report.alarms)


def test_rollup_qwen_idle_alarm_when_t0_routed_away():
    runs = [
        make_run(entry_id=f"t0_{i}", driver="copilot", tier="T0", commits_added=1)
        for i in range(10)
    ]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    # 10/10 = 100% T0 routed away from local-qwen → alarm
    assert report.local_qwen_t0_total == 10
    assert report.local_qwen_t0_idle == 10
    assert any("QWEN IDLE" in a for a in report.alarms)


def test_rollup_no_qwen_alarm_when_some_t0_on_local_qwen():
    runs = [
        make_run(entry_id="qwen", driver="local-qwen", tier="T0", commits_added=1),
        make_run(entry_id="cop", driver="copilot", tier="T0", commits_added=1),
    ]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    # 1/2 = 50% idle ≤ 80% threshold → no alarm
    assert all("QWEN IDLE" not in a for a in report.alarms)


# ─── cost extraction (Copilot R-L258) ────────────────────────────────────


def test_rollup_aggregates_cost_from_cch_runs():
    runs = [
        make_run(entry_id="c1", driver="cch", tier="T2", commits_added=1, cost_usd=0.10),
        make_run(entry_id="c2", driver="cch", tier="T2", commits_added=1, cost_usd=0.25),
        make_run(entry_id="cop", driver="copilot", tier="T0", commits_added=1, cost_usd=None),
    ]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    assert report.cost_total_usd == pytest.approx(0.35)
    assert report.cost_by_driver["cch"] == pytest.approx(0.35)
    # copilot reports no per-run cost; rolls up to 0.0 (not None).
    assert report.cost_by_driver["copilot"] == 0.0


# ─── in-flight gap surfaced ──────────────────────────────────────────────


def test_rollup_carries_in_flight_count_for_slack():
    report = generate_rollup([make_run(commits_added=1)], in_flight_or_lost=3, since=SINCE, until=UNTIL)
    assert report.in_flight_or_lost == 3
    payload = format_slack(report)
    text = json.dumps(payload)
    assert "in-flight or lost" in text


# ─── empty-runs corner ───────────────────────────────────────────────────


def test_empty_runs_produce_clean_report_no_zerodiv():
    report = generate_rollup([], in_flight_or_lost=0, since=SINCE, until=UNTIL)
    assert report.total_runs == 0
    assert report.bucket_b_rate == 0.0
    assert report.alarms == []


# ─── format_slack smoke ──────────────────────────────────────────────────


def test_format_slack_returns_blocks_structure():
    # T1 to avoid the QWEN IDLE alarm — see comment on
    # test_rollup_no_alarms_on_healthy_runs.
    runs = [make_run(commits_added=1, driver="copilot", tier="T1")]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    payload = format_slack(report)
    assert payload["text"].startswith("✅")
    assert any(b["type"] == "header" for b in payload["blocks"])


def test_format_slack_alarm_icon_when_alarms_present():
    runs = [make_run(commits_added=1, bucket_b=True)]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    payload = format_slack(report)
    assert payload["text"].startswith("🚨")


# ─── rollup_to_dict round-trip ───────────────────────────────────────────


def test_rollup_to_dict_is_json_serializable():
    runs = [make_run(commits_added=1)]
    report = generate_rollup(runs, in_flight_or_lost=0, since=SINCE, until=UNTIL)
    d = rollup_to_dict(report)
    json.dumps(d)  # must not raise


# ─── _parse_window ───────────────────────────────────────────────────────


def test_parse_window_supports_d_h_m():
    now = datetime(2026, 5, 2, 12, 0, tzinfo=timezone.utc)
    s, u = _parse_window("24h", now)
    assert u == now and (u - s) == timedelta(hours=24)
    s, u = _parse_window("7d", now)
    assert (u - s) == timedelta(days=7)
    s, u = _parse_window("30m", now)
    assert (u - s) == timedelta(minutes=30)


def test_parse_window_rejects_unknown_unit():
    with pytest.raises(ValueError, match="Unrecognized window"):
        _parse_window("1y", datetime.now(tz=timezone.utc))


# ─── main() end-to-end (Copilot R-L213) ──────────────────────────────────


def test_main_writes_journal_and_exits_zero_when_healthy(tmp_path, monkeypatch, capsys):
    """End-to-end: main reads markers + envelopes, writes a journal,
    exits 0 on no alarms."""
    state_dir = tmp_path / "state"
    tmp_dir = tmp_path / "tmp"
    rollup_dir = tmp_path / "rollups"
    state_dir.mkdir(); tmp_dir.mkdir()

    # One healthy run within the last 24h.
    now = datetime.now(tz=timezone.utc)
    ts = (now - timedelta(hours=2)).isoformat().replace("+00:00", "Z")
    (state_dir / "e1.json").write_text(json.dumps({
        # Tier T1 so the healthy run doesn't trip the QWEN IDLE alarm
        # (T0 routed away from local-qwen).
        "entry_id": "e1", "workflow_id": "swarm-1", "tier": "T1", "driver": "copilot",
        "dispatched_at": ts,
    }))
    (tmp_dir / "result-swarm-1.json").write_text(json.dumps({
        "workflow_id": "swarm-1",
        "result": {
            "exit_code": 0, "stdout_tail": "", "stderr_tail": "", "duration_ms": 24_000,
            "worktree": {
                "path": "/tmp/wt", "branch": "swarm/test", "head_sha": "abc",
                "commits_added": 1, "has_uncommitted_changes": False,
                "diff_shortstat": "1 file changed, 3 insertions(+)",
            },
        },
    }))

    monkeypatch.delenv("CHITIN_SLACK_WEBHOOK_URL", raising=False)
    rc = main([
        "--state-dir", str(state_dir),
        "--tmp-dir", str(tmp_dir),
        "--rollup-dir", str(rollup_dir),
        "--window", "24h",
        "--no-slack",
    ])
    assert rc == 0

    # Journal file written + json-parseable.
    journals = list(rollup_dir.glob("*.json"))
    assert len(journals) == 1
    parsed = json.loads(journals[0].read_text())
    assert parsed["total_runs"] == 1
    assert parsed["alarms"] == []


def test_main_returns_one_when_alarms_fire(tmp_path, monkeypatch):
    """End-to-end: alarm fires → exit 1 (so systemd flags the unit)."""
    state_dir = tmp_path / "state"
    tmp_dir = tmp_path / "tmp"
    rollup_dir = tmp_path / "rollups"
    state_dir.mkdir(); tmp_dir.mkdir()

    now = datetime.now(tz=timezone.utc)
    ts = (now - timedelta(hours=2)).isoformat().replace("+00:00", "Z")
    # Bucket-B contaminated run (matches the byte-identical signature).
    (state_dir / "bad.json").write_text(json.dumps({
        "entry_id": "bad", "workflow_id": "swarm-bad", "tier": "T2",
        "driver": "claude-code-headless", "dispatched_at": ts,
    }))
    (tmp_dir / "result-swarm-bad.json").write_text(json.dumps({
        "workflow_id": "swarm-bad",
        "result": {
            "exit_code": 0, "stdout_tail": "", "stderr_tail": "", "duration_ms": 1_000,
            "worktree": {
                "path": "/tmp/wt", "branch": "swarm/bad", "head_sha": "abc",
                "commits_added": 1, "has_uncommitted_changes": False,
                "diff_shortstat": "1 file changed, 12 insertions(+), 10 deletions(-)",
            },
        },
    }))

    monkeypatch.delenv("CHITIN_SLACK_WEBHOOK_URL", raising=False)
    rc = main([
        "--state-dir", str(state_dir),
        "--tmp-dir", str(tmp_dir),
        "--rollup-dir", str(rollup_dir),
        "--window", "24h",
        "--no-slack",
    ])
    assert rc == 1
