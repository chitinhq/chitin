"""Tests for argus.reporter."""
import os
import re
import tempfile
from datetime import datetime
from pathlib import Path

import pytest

from argus import migrations
from argus.beliefs import ingest_wiki_graph
from argus.reporter import generate_daily_report, _get_summary_stats
from argus.indexer import init_db


# Tests should not hit ollama; bypass narration for fast deterministic runs.
os.environ.setdefault("ARGUS_SKIP_QWEN", "1")


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
        """Report is written with today's ISO date in the filename."""
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "test.db"
            report_dir = Path(tmpdir) / "reports"

            conn = init_db(db_path)
            _insert_event(conn, "2026-05-13T08:00:00Z", False, "rule1")
            conn.close()

            report_path = generate_daily_report(str(db_path), report_dir)

            # Filename pattern is <YYYY-MM-DD>-argus-research.html where
            # the date is today's UTC date — don't pin to a fixed string
            # because the test must pass on any calendar day.
            assert re.match(r"\d{4}-\d{2}-\d{2}-argus-research\.html$", report_path.name)
            assert report_path.suffix == ".html"


def test_generate_report_writes_dark_html_latest_contract():
    """Daily report writes date-stamped HTML and an atomic latest symlink."""
    with tempfile.TemporaryDirectory() as tmpdir:
        db_path = Path(tmpdir) / "test.db"
        report_dir = Path(tmpdir) / "reports"
        conn = init_db(db_path)
        _insert_event(conn, "2026-05-13T08:00:00Z", False, "test-rule")
        conn.close()

        report_path = generate_daily_report(str(db_path), report_dir)
        latest = report_dir / "argus-research-latest.html"

        assert report_path.name.endswith("-argus-research.html")
        assert latest.is_symlink(), "latest must be a symlink, not a file copy"
        assert latest.resolve() == report_path.resolve()
        html = report_path.read_text()
        assert "Argus Research" in html
        assert "color-scheme: dark" in html
        # Reading through the symlink returns the dated file's content.
        assert latest.read_text() == html


def test_latest_symlink_retargets_atomically_on_rerun():
    """Re-running on the same day overwrites the dated file and the symlink continues to resolve."""
    with tempfile.TemporaryDirectory() as tmpdir:
        db_path = Path(tmpdir) / "test.db"
        report_dir = Path(tmpdir) / "reports"
        conn = init_db(db_path)
        _insert_event(conn, "2026-05-13T08:00:00Z", False, "rule1")
        conn.close()

        first = generate_daily_report(str(db_path), report_dir)
        second = generate_daily_report(str(db_path), report_dir)
        latest = report_dir / "argus-research-latest.html"

        # Same date → same dated file. Symlink still resolves cleanly.
        assert first == second
        assert latest.is_symlink()
        assert latest.resolve() == second.resolve()


def test_generate_report_includes_belief_drift_section():
    with tempfile.TemporaryDirectory() as tmpdir:
        db_path = Path(tmpdir) / "test.db"
        report_dir = Path(tmpdir) / "reports"
        wiki_root = Path(tmpdir) / "wiki"
        wiki_root.mkdir()

        conn = init_db(db_path)
        migrations.apply_pending(conn)
        (wiki_root / "belief.md").write_text("# Ticket t_rep_1\n\nBelief: ticket t_rep_1 is P50.\n")
        ingest_wiki_graph(conn, roots=[wiki_root])
        conn.close()

        report_path = generate_daily_report(str(db_path), report_dir)
        content = report_path.read_text()

        assert "Belief Drift" in content


def test_discord_post_skipped_on_quiet_day(monkeypatch):
    """Quiet-day default: no detector findings → Discord webhook is NOT called."""
    posted = {"calls": 0}

    def fake_post(url, headline, link=None):  # pragma: no cover - injected
        posted["calls"] += 1
        return True

    import argus.reporter as reporter_mod
    monkeypatch.setattr(reporter_mod, "_post_discord_summary", fake_post)

    with tempfile.TemporaryDirectory() as tmpdir:
        db_path = Path(tmpdir) / "test.db"
        report_dir = Path(tmpdir) / "reports"
        conn = init_db(db_path)
        conn.close()  # empty index → no findings

        generate_daily_report(
            str(db_path),
            report_dir,
            discord_webhook="https://example.invalid/webhook",
        )

    assert posted["calls"] == 0


def test_discord_post_fires_on_findings(monkeypatch):
    """Findings present → Discord webhook is called exactly once."""
    posted = {"calls": 0, "last_headline": None}

    def fake_post(url, headline, link=None):  # pragma: no cover - injected
        posted["calls"] += 1
        posted["last_headline"] = headline
        return True

    import argus.reporter as reporter_mod
    monkeypatch.setattr(reporter_mod, "_post_discord_summary", fake_post)

    with tempfile.TemporaryDirectory() as tmpdir:
        db_path = Path(tmpdir) / "test.db"
        report_dir = Path(tmpdir) / "reports"
        conn = init_db(db_path)
        # 4 denies within 300s → deny_cluster detector fires.
        for i in range(4):
            _insert_event(conn, f"2026-05-13T08:00:{i:02d}Z", False, "rule_x")
        conn.close()

        generate_daily_report(
            str(db_path),
            report_dir,
            discord_webhook="https://example.invalid/webhook",
        )

    assert posted["calls"] == 1
    assert "finding" in posted["last_headline"].lower()
