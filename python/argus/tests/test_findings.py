"""Tests for `argus findings` (Slice 5: Hermes standup-fold)."""
from __future__ import annotations

import hashlib
import json
import sqlite3
import tempfile
from pathlib import Path

from argus.cross_source_db import init_cross_source_db
from argus.findings import collect_findings, render_findings_json
from argus.indexer import init_db


def _insert_xs(conn, source, kind, ts_unix, subject, payload=None, dedup="x"):
    key = hashlib.sha256(f"{source}:{kind}:{subject}:{ts_unix}:{dedup}".encode()).hexdigest()[:24]
    conn.execute(
        """
        INSERT INTO cross_source_events
          (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        """,
        (source, kind, ts_unix, subject, None, json.dumps(payload or {}), key),
    )
    conn.commit()


def test_findings_empty_dbs_returns_empty_list():
    """No data → empty list, no exception."""
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)
        chain = tmpdir / "chain.db"
        xs = tmpdir / "xs.db"
        init_db(chain).close()
        init_cross_source_db(xs).close()
        findings = collect_findings(chain, xs)
        assert findings == []


def test_findings_includes_demote_loop():
    """A demote_loop cross-source finding flows through as JSON."""
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)
        chain = tmpdir / "chain.db"
        xs = tmpdir / "xs.db"
        init_db(chain).close()
        conn = init_cross_source_db(xs)
        now_ts = 100_000_000
        _insert_xs(conn, "kanban", "kanban_status_transition", now_ts - 3600, "t_x",
                   {"from": "ready", "to": "triage"}, dedup="1")
        _insert_xs(conn, "kanban", "kanban_status_transition", now_ts - 1800, "t_x",
                   {"from": "ready", "to": "triage"}, dedup="2")
        conn.close()

        findings = collect_findings(chain, xs, now_ts=now_ts)
        kinds = [f["kind"] for f in findings]
        assert "demote_loop" in kinds
        loop = next(f for f in findings if f["kind"] == "demote_loop")
        assert loop["suggested_action"] == "investigate"
        assert "kanban_event#" in loop["evidence_links"][0]


def test_findings_severity_sort():
    """Critical sorts before warning sorts before info."""
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)
        chain = tmpdir / "chain.db"
        xs = tmpdir / "xs.db"
        init_db(chain).close()
        conn = init_cross_source_db(xs)
        now_ts = 100_000_000
        # demote_loop fires at warning severity
        _insert_xs(conn, "kanban", "kanban_status_transition", now_ts - 3600, "t_x",
                   {"from": "ready", "to": "triage"}, dedup="1")
        _insert_xs(conn, "kanban", "kanban_status_transition", now_ts - 1800, "t_x",
                   {"from": "ready", "to": "triage"}, dedup="2")
        # stuck PR fires at info severity
        _insert_xs(conn, "git", "git_pr_opened", now_ts - 48 * 3600, "#1",
                   {"number": 1})
        conn.close()

        findings = collect_findings(chain, xs, now_ts=now_ts)
        severities = [f["severity"] for f in findings]
        # warning before info
        assert severities.index("warning") < severities.index("info")


def test_findings_since_filter():
    """Findings older than --since are excluded."""
    with tempfile.TemporaryDirectory() as tmpdir:
        tmpdir = Path(tmpdir)
        chain = tmpdir / "chain.db"
        xs = tmpdir / "xs.db"
        init_db(chain).close()
        conn = init_cross_source_db(xs)
        # Stuck PR opened at ts=1000, gates on 48h threshold.
        _insert_xs(conn, "git", "git_pr_opened", 1000, "#1", {"number": 1})
        conn.close()
        now_ts = 1000 + 48 * 3600 + 1

        all_findings = collect_findings(chain, xs, now_ts=now_ts)
        assert len(all_findings) >= 1

        # Filter: only findings at or after ts=now_ts (none qualify).
        filtered = collect_findings(chain, xs, since_ts=now_ts, now_ts=now_ts)
        assert filtered == []


def test_render_findings_json_compact():
    """Compact JSON output is a single line of valid JSON."""
    out = render_findings_json([{"kind": "x", "summary": "y"}])
    parsed = json.loads(out)
    assert parsed[0]["kind"] == "x"
    assert "\n" not in out


def test_render_findings_json_indent():
    """Indented output parses and contains newlines."""
    out = render_findings_json([{"kind": "x"}], indent=2)
    parsed = json.loads(out)
    assert parsed[0]["kind"] == "x"
    assert "\n" in out
