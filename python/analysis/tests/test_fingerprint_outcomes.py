"""Tests for analysis.fingerprint_outcomes — chain × swarm-runs join.

The recipe is the substrate for the P4 ELO board, so the contract this
test suite pins is the join semantics + the table-shape: which dispatches
roll up into which (fingerprint, model, role) cells.
"""
from __future__ import annotations

import json
import sqlite3
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

from analysis.fingerprint_outcomes import (
    Accumulator,
    FingerprintKey,
    build_table,
    render_markdown,
    write_sqlite,
)


# ─── Fixtures ─────────────────────────────────────────────────────────────


def _write_decisions_log(decisions_dir: Path, date: str, rows: list[dict]) -> None:
    """Write a gov-decisions-<date>.jsonl with the given rows."""
    decisions_dir.mkdir(parents=True, exist_ok=True)
    log_path = decisions_dir / f"gov-decisions-{date}.jsonl"
    with log_path.open("w") as f:
        for row in rows:
            f.write(json.dumps(row) + "\n")


def _write_marker(state_dir: Path, entry_id: str, marker: dict) -> None:
    """Write a dispatcher marker file. state_dir is the `dispatched/`
    directory itself (not its parent — load_swarm_runs iterates directly)."""
    state_dir.mkdir(parents=True, exist_ok=True)
    (state_dir / f"{entry_id}.json").write_text(json.dumps(marker))


def _write_envelope(tmp_dir: Path, workflow_id: str, envelope: dict) -> None:
    """Write a workflow result envelope. The loader looks for files
    matching `result-swarm-*.json`."""
    tmp_dir.mkdir(parents=True, exist_ok=True)
    (tmp_dir / f"result-{workflow_id}.json").write_text(json.dumps(envelope))


# ─── build_table — chain side ────────────────────────────────────────────


def test_build_table_aggregates_decisions_by_fingerprint(tmp_path: Path) -> None:
    """Decisions sharing a fingerprint roll up into one row; counts increment.
    The pre-P2-fingerprint case (fingerprint='') is its own bucket."""
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    decisions = tmp_path / "chain"
    state = tmp_path / "state"
    tmpd = tmp_path / "tmp"

    _write_decisions_log(
        decisions,
        "2026-05-04",
        [
            {
                "ts": "2026-05-04T11:00:00Z",
                "allowed": True,
                "rule_id": "default-allow",
                "agent": "claude-code",
                "model": "claude-haiku-4-5",
                "role": "reviewer",
                "fingerprint": "abc123",
                "workflow_id": "swarm-foo-1",
            },
            {
                "ts": "2026-05-04T11:01:00Z",
                "allowed": False,
                "rule_id": "no-rm",
                "agent": "claude-code",
                "model": "claude-haiku-4-5",
                "role": "reviewer",
                "fingerprint": "abc123",
                "workflow_id": "swarm-foo-1",
                "escalation": "elevated",
            },
            {
                "ts": "2026-05-04T11:02:00Z",
                "allowed": True,
                "rule_id": "default-allow",
                "agent": "copilot",
                # No fingerprint dims → groups under empty-string bucket
            },
        ],
    )

    table = build_table(
        decisions_dir=decisions,
        state_dir=state,
        tmp_dir=tmpd,
        window_str="7d",
        now=now,
    )

    # Two rows: one for (abc123, haiku, reviewer), one untagged.
    assert len(table) == 2

    tagged_key = FingerprintKey(
        fingerprint="abc123",
        model="claude-haiku-4-5",
        role="reviewer",
        task_class="",
    )
    tagged = table[tagged_key]
    assert tagged.dispatch_count == 2
    assert tagged.allow_count == 1
    assert tagged.deny_count == 1

    untagged_key = FingerprintKey(fingerprint="", model="", role="", task_class="")
    assert table[untagged_key].dispatch_count == 1


def test_build_table_counts_lockdown_decisions(tmp_path: Path) -> None:
    """escalation=lockdown rows roll up into the lockdown_count column."""
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    decisions = tmp_path / "chain"
    _write_decisions_log(
        decisions,
        "2026-05-04",
        [
            {
                "ts": "2026-05-04T11:00:00Z",
                "allowed": False,
                "rule_id": "lockdown",
                "escalation": "lockdown",
                "model": "x",
                "role": "y",
                "fingerprint": "fp1",
            },
        ],
    )

    table = build_table(
        decisions_dir=decisions,
        state_dir=tmp_path / "state",
        tmp_dir=tmp_path / "tmp",
        window_str="7d",
        now=now,
    )
    only = next(iter(table.values()))
    assert only.lockdown_count == 1
    assert only.deny_count == 1


def test_build_table_window_filters_old_decisions(tmp_path: Path) -> None:
    """Decisions older than the window don't aggregate."""
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    decisions = tmp_path / "chain"
    _write_decisions_log(
        decisions,
        "2026-04-01",
        [
            {
                "ts": "2026-04-01T11:00:00Z",
                "allowed": True,
                "model": "x",
                "fingerprint": "old",
            },
        ],
    )
    _write_decisions_log(
        decisions,
        "2026-05-04",
        [
            {
                "ts": "2026-05-04T11:00:00Z",
                "allowed": True,
                "model": "x",
                "fingerprint": "fresh",
            },
        ],
    )

    table = build_table(
        decisions_dir=decisions,
        state_dir=tmp_path / "state",
        tmp_dir=tmp_path / "tmp",
        window_str="7d",
        now=now,
    )

    fingerprints = {key.fingerprint for key in table.keys()}
    assert "fresh" in fingerprints
    assert "old" not in fingerprints


# ─── build_table — swarm-runs side ───────────────────────────────────────


def test_build_table_joins_swarm_runs_via_workflow_id(tmp_path: Path) -> None:
    """Swarm-run with matching workflow_id (chain side has the fingerprint)
    enriches the chain-side row with PR/duration metrics."""
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    decisions = tmp_path / "chain"
    state = tmp_path / "state"
    tmpd = tmp_path / "tmp"

    workflow_id = "swarm-test-entry-1234567890"

    _write_decisions_log(
        decisions,
        "2026-05-04",
        [
            {
                "ts": "2026-05-04T11:00:00Z",
                "allowed": True,
                "model": "haiku",
                "role": "programmer",
                "fingerprint": "fp-X",
                "workflow_id": workflow_id,
            },
        ],
    )

    _write_marker(
        state,
        "test-entry",
        {
            "entry_id": "test-entry",
            "workflow_id": workflow_id,
            "tier": "T2",
            "driver": "copilot",
            "dispatched_at": "2026-05-04T11:00:00Z",
        },
    )
    _write_envelope(
        tmpd,
        workflow_id,
        {
            "workflow_id": workflow_id,
            "result": {
                "exit_code": 0,
                "stdout_tail": "PR → https://github.com/chitinhq/chitin/pull/999",
                "stderr_tail": "",
                "duration_ms": 12000,
                "worktree": {
                    "path": "/tmp/x",
                    "branch": "swarm/test-entry",
                    "head_sha": "abc",
                    "commits_added": 1,
                    "has_uncommitted_changes": False,
                    "diff_shortstat": "2 files changed, 5 insertions(+), 1 deletion(-)",
                },
            },
        },
    )

    table = build_table(
        decisions_dir=decisions,
        state_dir=state,
        tmp_dir=tmpd,
        window_str="7d",
        now=now,
    )

    # Find the (fp-X, haiku, programmer) row — the PR-opened + duration
    # metrics from the swarm-run side should have joined onto it via
    # workflow_id substring match.
    target = next(
        (acc for key, acc in table.items() if key.fingerprint == "fp-X"),
        None,
    )
    assert target is not None
    assert target.dispatch_count == 1
    assert target.pr_opened_count == 1
    assert 11000 <= target.mean_duration_ms <= 13000


def test_build_table_swarm_runs_without_chain_match_use_untagged_bucket(
    tmp_path: Path,
) -> None:
    """A swarm-run whose workflow_id never appeared on the chain (older
    runs from before P2 dogfooding) lands in the empty-fingerprint
    bucket rather than getting silently dropped."""
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    decisions = tmp_path / "chain"
    state = tmp_path / "state"
    tmpd = tmp_path / "tmp"

    decisions.mkdir(parents=True, exist_ok=True)  # empty chain
    _write_marker(
        state,
        "ancient-entry",
        {
            "entry_id": "ancient-entry",
            "workflow_id": "swarm-ancient-entry-0",
            "tier": "T2",
            "driver": "copilot",
            "dispatched_at": "2026-05-04T11:00:00Z",
        },
    )
    _write_envelope(
        tmpd,
        "swarm-ancient-entry-0",
        {
            "workflow_id": "swarm-ancient-entry-0",
            "result": {
                "exit_code": 0,
                "stdout_tail": "PR → https://github.com/chitinhq/chitin/pull/1",
                "stderr_tail": "",
                "duration_ms": 5000,
                "worktree": {
                    "path": "/tmp",
                    "branch": "swarm/ancient",
                    "head_sha": "x",
                    "commits_added": 1,
                    "has_uncommitted_changes": False,
                    "diff_shortstat": "1 file changed",
                },
            },
        },
    )

    table = build_table(
        decisions_dir=decisions,
        state_dir=state,
        tmp_dir=tmpd,
        window_str="7d",
        now=now,
    )

    # Untagged bucket exists with the PR-opened count from the swarm-run.
    untagged_key = FingerprintKey(fingerprint="", model="", role="", task_class="")
    assert untagged_key in table
    assert table[untagged_key].pr_opened_count == 1


# ─── Sqlite write ─────────────────────────────────────────────────────────


def test_write_sqlite_creates_table_with_expected_schema(tmp_path: Path) -> None:
    """The sqlite output has the exact column set ELO board (P4) consumes."""
    table: dict[FingerprintKey, Accumulator] = {
        FingerprintKey("fp-1", "haiku", "reviewer", ""): Accumulator(
            dispatch_count=5,
            allow_count=4,
            deny_count=1,
            total_cost_usd=0.12,
            pr_opened_count=2,
        ),
    }
    db_path = tmp_path / "out.sqlite"
    write_sqlite(table, db_path)

    conn = sqlite3.connect(db_path)
    try:
        cur = conn.cursor()
        cur.execute("PRAGMA table_info(fingerprint_outcomes)")
        cols = {row[1] for row in cur.fetchall()}
        for required in {
            "fingerprint",
            "model",
            "role",
            "task_class",
            "dispatch_count",
            "allow_count",
            "deny_count",
            "lockdown_count",
            "total_cost_usd",
            "pr_opened_count",
            "pr_merged_count",
            "bucket_b_count",
            "mean_duration_ms",
        }:
            assert required in cols, f"missing column {required}"

        cur.execute("SELECT COUNT(*) FROM fingerprint_outcomes")
        assert cur.fetchone()[0] == 1
    finally:
        conn.close()


def test_write_sqlite_is_idempotent(tmp_path: Path) -> None:
    """Re-running drop+recreate doesn't accumulate stale rows."""
    db_path = tmp_path / "out.sqlite"

    table_a: dict[FingerprintKey, Accumulator] = {
        FingerprintKey("fp-A", "x", "y", ""): Accumulator(dispatch_count=1),
    }
    table_b: dict[FingerprintKey, Accumulator] = {
        FingerprintKey("fp-B", "x", "y", ""): Accumulator(dispatch_count=1),
    }
    write_sqlite(table_a, db_path)
    write_sqlite(table_b, db_path)

    conn = sqlite3.connect(db_path)
    try:
        cur = conn.cursor()
        cur.execute("SELECT fingerprint FROM fingerprint_outcomes")
        rows = [r[0] for r in cur.fetchall()]
        # First write's row must be gone; only second's remains.
        assert rows == ["fp-B"]
    finally:
        conn.close()


# ─── Markdown render ─────────────────────────────────────────────────────


def test_render_markdown_groups_by_role(tmp_path: Path) -> None:
    """Report has one ## section per role, no global average."""
    table: dict[FingerprintKey, Accumulator] = {
        FingerprintKey("fp-1", "haiku", "reviewer", ""): Accumulator(
            dispatch_count=10, allow_count=9, deny_count=1
        ),
        FingerprintKey("fp-2", "opus", "programmer", ""): Accumulator(
            dispatch_count=3, allow_count=3
        ),
    }
    out = render_markdown(table, "7d", datetime(2026, 5, 4, tzinfo=timezone.utc))

    assert "## Role: `reviewer`" in out
    assert "## Role: `programmer`" in out
    # No global "all-roles" section
    assert "## Role: all" not in out
    assert "## Role: global" not in out


def test_render_markdown_handles_empty_table() -> None:
    """No data → no role sections, but the report header still renders
    cleanly so the operator can tell the recipe ran."""
    out = render_markdown({}, "1d", datetime(2026, 5, 4, tzinfo=timezone.utc))
    assert "Fingerprint × outcomes report" in out
    assert "## Role:" not in out
