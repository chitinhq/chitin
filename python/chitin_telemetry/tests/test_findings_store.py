"""Tests for chitin_telemetry.findings_store persistence layer."""
from __future__ import annotations

import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path

import pytest

from chitin_telemetry import findings_store, migrations
from chitin_telemetry.detectors import Finding
from chitin_telemetry.indexer import init_db


def _f(detector="x", ts=None, severity="info", title="t", details=None):
    return Finding(
        detector=detector,
        ts=ts or datetime.now(timezone.utc),
        severity=severity,
        title=title,
        details=details or {},
    )


def test_persist_inserts_new_findings():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        inserted, skipped = findings_store.persist(conn, [_f(title="a"), _f(title="b")])
        assert inserted == 2
        assert skipped == 0
        conn.close()


def test_persist_dedupes_within_bucket():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        f = _f(title="dup")
        findings_store.persist(conn, [f])
        inserted, skipped = findings_store.persist(conn, [f])
        assert inserted == 0
        assert skipped == 1
        conn.close()


def test_since_returns_recent_findings():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        findings_store.persist(conn, [_f(title="recent")])
        rows = findings_store.since(conn, 0)
        assert len(rows) == 1
        assert rows[0].title == "recent"
        conn.close()


def test_set_operator_action_marks_finding():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        findings_store.persist(conn, [_f(title="x")])
        row = conn.execute("SELECT id FROM findings").fetchone()
        ok = findings_store.set_operator_action(conn, int(row["id"]), "ack")
        assert ok
        again = conn.execute(
            "SELECT operator_action FROM findings WHERE id = ?", (int(row["id"]),)
        ).fetchone()
        assert again["operator_action"] == "ack"
        conn.close()


def test_set_operator_action_rejects_invalid():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        findings_store.persist(conn, [_f(title="x")])
        row = conn.execute("SELECT id FROM findings").fetchone()
        with pytest.raises(ValueError):
            findings_store.set_operator_action(conn, int(row["id"]), "wat")
        conn.close()


def test_since_filters_acked_by_default():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        findings_store.persist(conn, [_f(title="a"), _f(title="b")])
        row = conn.execute("SELECT id FROM findings ORDER BY id LIMIT 1").fetchone()
        findings_store.set_operator_action(conn, int(row["id"]), "ack")
        visible = findings_store.since(conn, 0, include_acked=False)
        titles = {r.title for r in visible}
        assert "a" not in titles or "b" not in titles  # one is acked, hidden
        assert len(visible) == 1
        all_visible = findings_store.since(conn, 0, include_acked=True)
        assert len(all_visible) == 2
        conn.close()


def test_action_rate_zero_when_no_findings():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        ar = findings_store.action_rate(conn)
        assert ar["surfaced"] == 0
        assert ar["action_rate"] == 0.0
        conn.close()


def test_action_rate_computed_correctly():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        # 10 findings, 2 acked, 1 applied -> action_rate = 0.3
        findings_store.persist(conn, [_f(title=f"t{i}") for i in range(10)])
        ids = [r["id"] for r in conn.execute("SELECT id FROM findings ORDER BY id").fetchall()]
        findings_store.set_operator_action(conn, ids[0], "ack")
        findings_store.set_operator_action(conn, ids[1], "ack")
        findings_store.set_operator_action(conn, ids[2], "apply")
        ar = findings_store.action_rate(conn)
        assert ar["surfaced"] == 10
        assert ar["acked"] == 2
        assert ar["applied"] == 1
        assert ar["action_rate"] == pytest.approx(0.3)
        conn.close()
