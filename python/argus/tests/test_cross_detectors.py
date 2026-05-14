"""Tests for cross-source detectors with boundary cases.

Each detector has a positive case at the threshold, a negative case
just below, and at least one missing-side join scenario.
"""
from __future__ import annotations

import json
import sqlite3
import tempfile
from pathlib import Path

from argus.cross_source_db import init_cross_source_db
from argus.cross_detectors import (
    detect_demote_loops,
    detect_stuck_prs_green_ci,
    detect_follow_up_clustering,
    run_all_cross_detectors,
)


def _insert_xs(conn, source, kind, ts_unix, subject, payload=None, actor=None, dedup_suffix=""):
    """Helper: insert one cross_source_events row."""
    import hashlib
    key = hashlib.sha256(f"{source}:{kind}:{subject}:{ts_unix}:{dedup_suffix}".encode()).hexdigest()[:24]
    conn.execute(
        """
        INSERT INTO cross_source_events
          (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        """,
        (source, kind, ts_unix, subject, actor, json.dumps(payload or {}), key),
    )
    conn.commit()


class TestDemoteLoop:
    def test_exactly_two_bounces_triggers(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "kanban", "kanban_status_transition", 1000, "t_x",
                       {"from": "ready", "to": "triage", "by": "red"}, dedup_suffix="1")
            _insert_xs(conn, "kanban", "kanban_status_transition", 2000, "t_x",
                       {"from": "ready", "to": "triage", "by": "red"}, dedup_suffix="2")
            conn.close()

            findings = detect_demote_loops(xs, now_ts=3000, min_bounces=2)
            assert len(findings) == 1
            assert findings[0].subject == "t_x"
            assert findings[0].details["bounce_count"] == 2

    def test_one_bounce_does_not_trigger(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "kanban", "kanban_status_transition", 1000, "t_x",
                       {"from": "ready", "to": "triage"})
            conn.close()

            assert detect_demote_loops(xs, now_ts=3000, min_bounces=2) == []

    def test_other_transition_kinds_ignored(self):
        """Only ready→triage counts as a bounce."""
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            for i in range(3):
                _insert_xs(conn, "kanban", "kanban_status_transition", 1000 + i, "t_x",
                           {"from": "in_progress", "to": "blocked"}, dedup_suffix=str(i))
            conn.close()

            assert detect_demote_loops(xs, now_ts=3000, min_bounces=2) == []

    def test_outside_window_ignored(self):
        """Bounces older than window_seconds don't count."""
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "kanban", "kanban_status_transition", 100, "t_x",
                       {"from": "ready", "to": "triage"}, dedup_suffix="1")
            _insert_xs(conn, "kanban", "kanban_status_transition", 200, "t_x",
                       {"from": "ready", "to": "triage"}, dedup_suffix="2")
            conn.close()

            # window is 24h ending at now_ts=100000; both events older.
            assert detect_demote_loops(xs, now_ts=100000, window_seconds=3600, min_bounces=2) == []


class TestStuckPrGreenCi:
    def test_open_pr_no_merge_triggers(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "git", "git_pr_opened", 1000, "#123",
                       {"number": 123, "title": "fix x"}, actor="alice")
            conn.close()

            findings = detect_stuck_prs_green_ci(xs, min_open_seconds=3600,
                                                 now_ts=1000 + 7200)
            assert len(findings) == 1
            assert findings[0].subject == "#123"

    def test_open_pr_below_threshold_does_not_trigger(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "git", "git_pr_opened", 1000, "#123",
                       {"number": 123, "title": "fresh"})
            conn.close()

            # Opened 100s ago, threshold is 3600.
            assert detect_stuck_prs_green_ci(xs, min_open_seconds=3600,
                                              now_ts=1100) == []

    def test_merged_pr_does_not_trigger(self):
        """If the PR has a git_pr_merged row, it's not stuck."""
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "git", "git_pr_opened", 1000, "#123", {"number": 123})
            _insert_xs(conn, "git", "git_pr_merged", 1500, "#123", {"number": 123},
                       dedup_suffix="merged")
            conn.close()

            assert detect_stuck_prs_green_ci(xs, min_open_seconds=3600,
                                              now_ts=1000 + 7200) == []

    def test_draft_pr_does_not_trigger(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "git", "git_pr_opened", 1000, "#123",
                       {"number": 123, "draft": True})
            conn.close()

            assert detect_stuck_prs_green_ci(xs, min_open_seconds=3600,
                                              now_ts=1000 + 7200) == []


class TestFollowUpClustering:
    def test_three_tickets_after_merge_triggers(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "git", "git_pr_merged", 1000, "#42", {"number": 42})
            for i in range(3):
                _insert_xs(conn, "kanban", "kanban_event", 1100 + i, f"t_{i}",
                           dedup_suffix=str(i))
            conn.close()

            findings = detect_follow_up_clustering(xs, window_seconds=7200,
                                                    min_tickets=3, now_ts=2000)
            assert len(findings) == 1
            assert findings[0].subject == "#42"
            assert findings[0].details["follow_up_count"] == 3

    def test_two_tickets_after_merge_does_not_trigger(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "git", "git_pr_merged", 1000, "#42", {"number": 42})
            for i in range(2):
                _insert_xs(conn, "kanban", "kanban_event", 1100 + i, f"t_{i}",
                           dedup_suffix=str(i))
            conn.close()

            assert detect_follow_up_clustering(xs, window_seconds=7200,
                                                min_tickets=3, now_ts=2000) == []

    def test_tickets_outside_window_ignored(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert_xs(conn, "git", "git_pr_merged", 1000, "#42", {"number": 42})
            # 5 tickets but all outside the 1-hour window after the merge.
            for i in range(5):
                _insert_xs(conn, "kanban", "kanban_event", 5000 + i, f"t_{i}",
                           dedup_suffix=str(i))
            conn.close()

            assert detect_follow_up_clustering(xs, window_seconds=3600,
                                                min_tickets=3, now_ts=10000) == []


def test_run_all_cross_detectors_combines_findings():
    """Top-level entrypoint returns findings from every detector, deterministically sorted."""
    with tempfile.TemporaryDirectory() as tmpdir:
        xs = Path(tmpdir) / "xs.db"
        conn = init_cross_source_db(xs)
        # Demote loop for t_y — keep both bounces inside the 24h window
        # that ends at now_ts.
        now_ts = 100_000_000
        _insert_xs(conn, "kanban", "kanban_status_transition", now_ts - 3600, "t_y",
                   {"from": "ready", "to": "triage"}, dedup_suffix="1")
        _insert_xs(conn, "kanban", "kanban_status_transition", now_ts - 1800, "t_y",
                   {"from": "ready", "to": "triage"}, dedup_suffix="2")
        # Stuck PR: opened 48h before now_ts, never merged.
        _insert_xs(conn, "git", "git_pr_opened", now_ts - 48 * 3600, "#777", {"number": 777})
        conn.close()

        findings = run_all_cross_detectors(xs, now_ts=now_ts)
        kinds = {f.detector for f in findings}
        assert "demote_loop" in kinds
        assert "stuck_pr_green_ci" in kinds


def test_missing_xs_db_returns_no_findings():
    """A missing cross-source DB yields zero findings, no error."""
    findings = run_all_cross_detectors(Path("/nonexistent/argus/xs.db"))
    assert findings == []
