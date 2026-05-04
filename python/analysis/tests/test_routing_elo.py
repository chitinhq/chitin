"""Tests for analysis.routing_elo — pairwise ELO leaderboard.

Pins the determinism and convergence properties:
- same input → same scores (re-run safe)
- known-better fingerprint converges higher than baseline
- ties / no-data cases stay at baseline
- markdown shows fingerprint dimensions, not just hash
"""
from __future__ import annotations

import sqlite3
from datetime import datetime, timezone
from pathlib import Path

import pytest

from analysis.routing_elo import (
    BASELINE_RATING,
    FingerprintRow,
    LeaderboardEntry,
    apply_match,
    build_leaderboard,
    compute_elo,
    derive_match_record,
    expected_score,
    load_rows,
    render_leaderboard,
)


# ─── ELO math primitives ──────────────────────────────────────────────────


def test_expected_score_equal_ratings_is_50_percent() -> None:
    assert expected_score(1500.0, 1500.0) == pytest.approx(0.5)


def test_expected_score_higher_rating_favors_self() -> None:
    """Rating gap of 400 → ~91% expected score for the higher-rated player."""
    assert expected_score(1900.0, 1500.0) == pytest.approx(0.909, abs=0.01)


def test_apply_match_win_against_equal_opponent_increases_rating() -> None:
    new_rating = apply_match(1500.0, outcome=1.0, opponent=1500.0)
    assert new_rating > 1500.0
    # K=32, expected=0.5, actual=1 → +16
    assert new_rating == pytest.approx(1516.0)


def test_apply_match_loss_against_equal_opponent_decreases_rating() -> None:
    new_rating = apply_match(1500.0, outcome=0.0, opponent=1500.0)
    assert new_rating < 1500.0
    assert new_rating == pytest.approx(1484.0)


def test_apply_match_draw_against_equal_opponent_no_change() -> None:
    """Outcome=0.5, expected=0.5 → delta=0."""
    new_rating = apply_match(1500.0, outcome=0.5, opponent=1500.0)
    assert new_rating == pytest.approx(1500.0)


# ─── compute_elo determinism + convergence ────────────────────────────────


def test_compute_elo_zero_matches_returns_baseline() -> None:
    """A brand-new fingerprint with no matches sits at the baseline."""
    assert compute_elo(0, 0) == BASELINE_RATING


def test_compute_elo_is_deterministic() -> None:
    """Re-running on the same (W, L) input produces identical ELO."""
    a = compute_elo(20, 5)
    b = compute_elo(20, 5)
    assert a == b


def test_compute_elo_higher_win_count_yields_higher_rating() -> None:
    """Known-better fingerprint converges higher."""
    strong = compute_elo(50, 5)
    weak = compute_elo(5, 50)
    assert strong > weak
    assert strong > BASELINE_RATING
    assert weak < BASELINE_RATING


def test_compute_elo_equal_wins_losses_returns_close_to_baseline() -> None:
    """Tied W=L should converge near baseline (path-dependent ordering
    pulls slightly off, but within tens of points)."""
    rating = compute_elo(20, 20)
    assert abs(rating - BASELINE_RATING) < 100  # within ~6% of baseline


def test_compute_elo_only_wins_strictly_above_baseline() -> None:
    """No-loss edge case (Copilot review #296): the Bresenham
    interleaving condition can mis-classify when one side is zero.
    Explicit handling: N wins + 0 losses applies N win-updates,
    rating is strictly higher than baseline."""
    rating = compute_elo(10, 0)
    assert rating > BASELINE_RATING


def test_compute_elo_only_losses_strictly_below_baseline() -> None:
    """Symmetric no-win edge case."""
    rating = compute_elo(0, 10)
    assert rating < BASELINE_RATING


def test_compute_elo_one_win_zero_loss_increases_rating() -> None:
    """Smallest win-only case (1, 0): one apply_match win update."""
    rating = compute_elo(1, 0)
    # K=32, expected=0.5 against equal opponent → +16
    assert rating == pytest.approx(BASELINE_RATING + 16)


def test_compute_elo_zero_win_one_loss_decreases_rating() -> None:
    """Smallest loss-only case (0, 1)."""
    rating = compute_elo(0, 1)
    assert rating == pytest.approx(BASELINE_RATING - 16)


# ─── Match derivation ─────────────────────────────────────────────────────


def test_derive_match_record_counts_chain_and_swarm_successes() -> None:
    row = FingerprintRow(
        fingerprint="fp",
        model="m",
        role="r",
        task_class="",
        dispatch_count=10,
        allow_count=8,
        deny_count=2,
        lockdown_count=0,
        pr_opened_count=3,  # additional swarm-runs side success
        bucket_b_count=0,
        total_cost_usd=0.0,
        mean_duration_ms=0.0,
        first_seen_ts=None,
        last_seen_ts=None,
    )
    wins, losses = derive_match_record(row)
    assert wins == 11  # 8 allow + 3 pr_opened
    assert losses == 2


def test_derive_match_record_counts_lockdown_and_bucket_b_as_losses() -> None:
    row = FingerprintRow(
        fingerprint="fp",
        model="m",
        role="r",
        task_class="",
        dispatch_count=5,
        allow_count=3,
        deny_count=1,
        lockdown_count=1,
        pr_opened_count=0,
        bucket_b_count=2,
        total_cost_usd=0.0,
        mean_duration_ms=0.0,
        first_seen_ts=None,
        last_seen_ts=None,
    )
    wins, losses = derive_match_record(row)
    assert wins == 3
    assert losses == 4  # 1 deny + 1 lockdown + 2 bucket_b


def test_derive_match_record_swarm_only_bucket_with_zero_dispatch_count() -> None:
    """Pre-P2 / swarm-only rows have dispatch_count=0 but pr_opened_count
    or bucket_b_count from the swarm-runs side. The previous capped
    derivation under-counted these — fixed per Copilot #296."""
    row = FingerprintRow(
        fingerprint="",
        model="haiku",
        role="programmer",
        task_class="",
        dispatch_count=0,  # zero chain decisions
        allow_count=0,
        deny_count=0,
        lockdown_count=0,
        pr_opened_count=8,  # but 8 successful PRs from swarm-runs
        bucket_b_count=2,
        total_cost_usd=0.0,
        mean_duration_ms=0.0,
        first_seen_ts=None,
        last_seen_ts=None,
    )
    wins, losses = derive_match_record(row)
    assert wins == 8  # not capped to 0 * 2 = 0
    assert losses == 2


# ─── Sqlite read ──────────────────────────────────────────────────────────


def _make_sqlite(path: Path, rows: list[tuple]) -> None:
    """Write a fingerprint-outcomes-shaped sqlite for testing.

    Schema mirrors fingerprint_outcomes.write_sqlite but defined here
    locally so the test doesn't depend on the writer (avoids circular
    test coupling)."""
    conn = sqlite3.connect(path)
    try:
        cur = conn.cursor()
        cur.execute(
            """
            CREATE TABLE fingerprint_outcomes (
              fingerprint TEXT NOT NULL,
              model TEXT NOT NULL,
              role TEXT NOT NULL,
              task_class TEXT NOT NULL,
              dispatch_count INTEGER NOT NULL,
              allow_count INTEGER NOT NULL,
              deny_count INTEGER NOT NULL,
              lockdown_count INTEGER NOT NULL,
              total_cost_usd REAL NOT NULL,
              pr_opened_count INTEGER NOT NULL,
              pr_merged_count INTEGER NOT NULL,
              bucket_b_count INTEGER NOT NULL,
              mean_duration_ms REAL NOT NULL,
              first_seen_ts TEXT,
              last_seen_ts TEXT,
              PRIMARY KEY (fingerprint, model, role, task_class)
            )
            """
        )
        cur.executemany(
            "INSERT INTO fingerprint_outcomes VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            rows,
        )
        conn.commit()
    finally:
        conn.close()


def test_load_rows_returns_empty_for_missing_db(tmp_path: Path) -> None:
    rows = load_rows(tmp_path / "nope.sqlite")
    assert rows == []


def test_load_rows_returns_empty_for_db_without_table(tmp_path: Path) -> None:
    """A sqlite file that exists but has no fingerprint_outcomes table —
    e.g., a stale db from a different recipe — returns empty rather than
    crashing on the SELECT."""
    db = tmp_path / "empty.sqlite"
    conn = sqlite3.connect(db)
    conn.close()
    assert load_rows(db) == []


def test_load_rows_returns_all_rows_when_role_filter_is_none(tmp_path: Path) -> None:
    db = tmp_path / "out.sqlite"
    _make_sqlite(
        db,
        [
            ("fp1", "haiku", "reviewer", "", 5, 4, 1, 0, 0.0, 0, 0, 0, 0.0, None, None),
            ("fp2", "opus", "programmer", "", 3, 3, 0, 0, 0.0, 0, 0, 0, 0.0, None, None),
        ],
    )
    rows = load_rows(db)
    assert len(rows) == 2


def test_load_rows_filters_by_role(tmp_path: Path) -> None:
    db = tmp_path / "out.sqlite"
    _make_sqlite(
        db,
        [
            ("fp1", "haiku", "reviewer", "", 5, 4, 1, 0, 0.0, 0, 0, 0, 0.0, None, None),
            ("fp2", "opus", "programmer", "", 3, 3, 0, 0, 0.0, 0, 0, 0, 0.0, None, None),
        ],
    )
    rows = load_rows(db, role_filter="reviewer")
    assert len(rows) == 1
    assert rows[0].role == "reviewer"


# ─── Leaderboard build ────────────────────────────────────────────────────


def test_build_leaderboard_orders_by_elo_descending_within_role() -> None:
    rows = [
        FingerprintRow("fp1", "m1", "reviewer", "", 50, 45, 5, 0, 0, 0, 0.0, 0.0, None, None),
        FingerprintRow("fp2", "m2", "reviewer", "", 50, 5, 45, 0, 0, 0, 0.0, 0.0, None, None),
    ]
    entries = build_leaderboard(rows)
    # build_leaderboard returns in input order; the markdown render sorts
    # within bucket. The leaderboard data structure itself is order-
    # agnostic.
    by_fp = {e.fingerprint: e for e in entries}
    assert by_fp["fp1"].elo > by_fp["fp2"].elo


def test_build_leaderboard_computes_cost_per_success() -> None:
    rows = [
        FingerprintRow("fp1", "m", "r", "", 10, 8, 2, 0, 0, 0, 0.40, 0.0, None, None),
    ]
    entries = build_leaderboard(rows)
    assert len(entries) == 1
    # 8 wins, $0.40 total → $0.05 per success
    assert entries[0].cost_per_success == pytest.approx(0.05)


def test_build_leaderboard_handles_zero_dispatches() -> None:
    rows = [
        FingerprintRow("fp1", "m", "r", "", 0, 0, 0, 0, 0, 0, 0.0, 0.0, None, None),
    ]
    entries = build_leaderboard(rows)
    assert entries[0].elo == BASELINE_RATING
    assert entries[0].cost_per_success is None


# ─── Markdown render ──────────────────────────────────────────────────────


def test_render_leaderboard_groups_by_role_and_task_class() -> None:
    entries = [
        LeaderboardEntry("fp1", "haiku", "reviewer", "", 10, 8, 2, 1550, 0.8, 0.05),
        LeaderboardEntry("fp2", "opus", "programmer", "", 3, 3, 0, 1540, 1.0, 0.10),
    ]
    out = render_leaderboard(entries, datetime(2026, 5, 4, tzinfo=timezone.utc), None)
    assert "## `reviewer`" in out
    assert "## `programmer`" in out
    # Fingerprint dimensions visible (not just opaque hash)
    assert "haiku" in out
    assert "opus" in out
    # ELO numbers visible
    assert "1550" in out
    assert "1540" in out


def test_render_leaderboard_handles_empty_input() -> None:
    out = render_leaderboard([], datetime(2026, 5, 4, tzinfo=timezone.utc), None)
    assert "Routing ELO leaderboard" in out
    assert "No data" in out


def test_render_leaderboard_shows_role_filter_when_set() -> None:
    out = render_leaderboard(
        [], datetime(2026, 5, 4, tzinfo=timezone.utc), role_filter="reviewer"
    )
    assert "Role filter: `reviewer`" in out
