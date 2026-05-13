"""Tests for argus.reporter."""
import tempfile
from datetime import datetime
from pathlib import Path

import pytest

from argus.reporter import generate_daily_report, _get_summary_stats
from argus.indexer import init_db


def _insert_event(conn, ts: str, allowed: bool, rule_id: str, agent: str = "test-agent"):
    """Helper to insert an event."""
    import hashlib
    from datetime import datetime as dt
    parsed_dt = dt.fromisoformat(ts.replace("Z", "+00:00"))
    ts_unix = int(parsed_dt.timestamp())

    # Create unique hash for each event
    event_id = f"{ts}-{rule_id}-{agent}-{allowed}"
    lh = hashlib.sha256(event_id.encode()).hexdigest()

    conn.execute("""
        INSERT INTO events (
            line_hash, ts, ts_unix, allowed, rule_id, agent, action_type
        ) VALUES (?, ?, ?, ?, ?, ?, ?)
    """, (
        lh,
        parsed_dt.isoformat(),
        ts_unix,
        int(allowed),
        rule_id,
        agent,
        "shell.exec",
    ))
    conn.commit()


class TestSummaryStats:
    """Test summary statistics gathering."""

    def test_summary_stats_empty_database(self):
        """Empty database returns zero counts."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            conn.close()

            stats = _get_summary_stats(str(db_path))
            assert stats["total_events"] == 0
            assert stats["deny_count"] == 0
            assert stats["allow_count"] == 0

    def test_summary_stats_single_event(self):
        """Single event renders without divide-by-zero."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)
            _insert_event(conn, "2026-05-13T08:00:00Z", False, "rule1")
            conn.close()

            stats = _get_summary_stats(str(db_path))
            assert stats["total_events"] == 1
            assert stats["deny_count"] == 1
            assert stats["allow_count"] == 0
            assert stats["deny_percent"] == 100.0

    def test_summary_stats_mixed_events(self):
        """Mixed allow/deny events are counted correctly."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            for i in range(7):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule1")

            for i in range(3):
                _insert_event(conn, f"2026-05-13T08:01:{i:02d}Z", True, "rule2")

            conn.close()

            stats = _get_summary_stats(str(db_path))
            assert stats["total_events"] == 10
            assert stats["deny_count"] == 7
            assert stats["allow_count"] == 3

    def test_summary_stats_top_rules(self):
        """Top deny rules are ranked correctly."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            conn = init_db(db_path)

            for i in range(5):
                _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule_frequent")

            for i in range(2):
                _insert_event(conn, f"2026-05-13T08:01:{i:02d}Z", False, "rule_rare")

            conn.close()

            stats = _get_summary_stats(str(db_path))
            assert stats["top_deny_rules"][0]["rule_id"] == "rule_frequent"
            assert stats["top_deny_rules"][0]["count"] == 5


class TestGenerateDailyReport:
    """Test daily report generation."""

    def test_generate_report_empty_database(self):
        """Report on empty database renders 'all quiet'."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            report_dir = Path(tmpdir) / "reports"

            conn = init_db(db_path)
            conn.close()

            report_path = generate_daily_report(str(db_path), report_dir)

            assert report_path.exists()
            content = report_path.read_text()
            assert "all quiet" in content or "No findings" in content

    def test_generate_report_single_event(self):
        """Report on single event renders without divide-by-zero."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            report_dir = Path(tmpdir) / "reports"

            conn = init_db(db_path)
            _insert_event(conn, "2026-05-13T08:00:00Z", False, "test-rule")
            conn.close()

            report_path = generate_daily_report(str(db_path), report_dir)

            assert report_path.exists()
            content = report_path.read_text()
            assert "test-rule" in content

    def test_generate_report_creates_file_with_timestamp(self):
        """Report is written with date-based filename."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            report_dir = Path(tmpdir) / "reports"

            conn = init_db(db_path)
            _insert_event(conn, "2026-05-13T08:00:00Z", False, "rule1")
            conn.close()

            report_path = generate_daily_report(str(db_path), report_dir)

            # Should have ISO date in filename
            assert "2026-05-13" in report_path.name or "digest.md" in report_path.name
            assert report_path.suffix == ".md"
