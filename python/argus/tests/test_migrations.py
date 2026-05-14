"""Tests for additive schema migrations."""
from __future__ import annotations

import tempfile
from pathlib import Path

from argus import migrations
from argus.indexer import init_db


def test_migrations_apply_idempotently():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        applied_first = migrations.apply_pending(conn)
        assert applied_first  # at least one migration applied
        applied_second = migrations.apply_pending(conn)
        assert applied_second == []  # nothing pending second time
        conn.close()


def test_migrations_create_expected_tables():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        names = {r[0] for r in conn.execute(
            "SELECT name FROM sqlite_master WHERE type='table'"
        ).fetchall()}
        for expected in {
            "events", "findings", "llm_calls", "hypotheses",
            "evidence_links", "memory", "kernel_state",
            "schema_migrations",
        }:
            assert expected in names, f"missing table: {expected}"
        conn.close()


def test_events_source_column_added_with_default():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        # Insert a row pre-migration to verify backfill default.
        conn.execute(
            """
            INSERT INTO events (line_hash, ts, ts_unix, allowed)
            VALUES ('h1', '2026-05-13T00:00:00+00:00', 1715567890, 1)
            """
        )
        conn.commit()
        migrations.apply_pending(conn)
        row = conn.execute("SELECT source FROM events WHERE line_hash = 'h1'").fetchone()
        assert row["source"] == "chain"
        conn.close()


def test_open_readonly_blocks_writes():
    import sqlite3
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        conn.close()
        ro = migrations.open_readonly(db)
        try:
            ro.execute("INSERT INTO events (line_hash, ts, ts_unix, allowed) VALUES ('h2', 't', 0, 1)")
        except sqlite3.OperationalError as e:
            assert "readonly" in str(e).lower() or "read-only" in str(e).lower()
        else:
            raise AssertionError("read-only connection accepted INSERT")
        finally:
            ro.close()


def test_integrity_check_returns_ok_on_fresh_db():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = init_db(db)
        migrations.apply_pending(conn)
        assert migrations.integrity_check(conn) == "ok"
        conn.close()
