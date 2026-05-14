"""Tests for log-derived detectors."""
from __future__ import annotations

import hashlib
import json
import tempfile
from pathlib import Path

from argus.cross_source_db import init_cross_source_db
from argus.log_detectors import (
    detect_hermes_standup_gap,
    detect_openclaw_dispatch_failures,
    run_all_log_detectors,
)


def _insert(conn, source, kind, ts_unix, subject, payload=None, actor=None, suffix=""):
    key = hashlib.sha256(f"{source}:{kind}:{subject}:{ts_unix}:{suffix}".encode()).hexdigest()[:24]
    conn.execute(
        """
        INSERT INTO cross_source_events
          (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        """,
        (source, kind, ts_unix, subject, actor, json.dumps(payload or {}), key),
    )
    conn.commit()


class TestHermesStandupGap:
    def test_gap_above_threshold_triggers(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert(conn, "hermes", "hermes_standup", 1000, "agent.log", suffix="1")
            _insert(conn, "hermes", "hermes_standup", 1000 + 9 * 3600, "agent.log", suffix="2")
            conn.close()
            findings = detect_hermes_standup_gap(xs, max_gap_seconds=8 * 3600)
            assert len(findings) == 1
            assert findings[0].details["gap_hours"] == 9

    def test_gap_at_threshold_does_not_trigger(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert(conn, "hermes", "hermes_standup", 1000, "agent.log", suffix="1")
            _insert(conn, "hermes", "hermes_standup", 1000 + 8 * 3600, "agent.log", suffix="2")
            conn.close()
            assert detect_hermes_standup_gap(xs, max_gap_seconds=8 * 3600) == []

    def test_no_standup_events_zero_findings(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            init_cross_source_db(xs).close()
            assert detect_hermes_standup_gap(xs) == []


class TestOpenclawDispatchFailures:
    def test_failure_in_window_triggers(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert(conn, "openclaw", "openclaw_dispatch_fail",
                    1000, "dispatch-t_x.log", {"msg": "dispatch failed"})
            conn.close()
            findings = detect_openclaw_dispatch_failures(xs, now_ts=2000)
            assert len(findings) == 1
            assert findings[0].subject == "dispatch-t_x.log"

    def test_failure_outside_window_ignored(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            xs = Path(tmpdir) / "xs.db"
            conn = init_cross_source_db(xs)
            _insert(conn, "openclaw", "openclaw_dispatch_fail",
                    100, "dispatch-t_x.log", {"msg": "old failure"})
            conn.close()
            assert detect_openclaw_dispatch_failures(
                xs, window_seconds=3600, now_ts=100_000
            ) == []


def test_run_all_log_detectors_combines():
    with tempfile.TemporaryDirectory() as tmpdir:
        xs = Path(tmpdir) / "xs.db"
        conn = init_cross_source_db(xs)
        _insert(conn, "hermes", "hermes_standup", 1000, "agent.log", suffix="1")
        _insert(conn, "hermes", "hermes_standup", 1000 + 9 * 3600, "agent.log", suffix="2")
        _insert(conn, "openclaw", "openclaw_dispatch_fail",
                1000 + 9 * 3600 + 60, "dispatch-t_x.log", {"msg": "fail"})
        conn.close()
        findings = run_all_log_detectors(xs, now_ts=1000 + 10 * 3600)
        kinds = {f.detector for f in findings}
        assert "hermes_standup_gap" in kinds
        assert "openclaw_dispatch_failure" in kinds


def test_missing_xs_db_returns_no_findings():
    assert run_all_log_detectors(Path("/nonexistent/argus/xs.db")) == []
