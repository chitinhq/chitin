"""Tests for additive schema migrations."""
from __future__ import annotations

import sqlite3
import tempfile
from pathlib import Path

from argus import migrations
from argus.indexer import init_db, insert_event


def _legacy_conn(db: Path) -> sqlite3.Connection:
    conn = sqlite3.connect(str(db))
    conn.row_factory = sqlite3.Row
    conn.execute(
        """
        CREATE TABLE events (
            id INTEGER PRIMARY KEY,
            line_hash TEXT UNIQUE NOT NULL,
            ts TEXT NOT NULL,
            ts_unix INTEGER NOT NULL,
            allowed INTEGER NOT NULL,
            mode TEXT,
            rule_id TEXT,
            reason TEXT,
            escalation TEXT,
            agent TEXT,
            action_type TEXT,
            action_target TEXT,
            envelope_id TEXT,
            tier TEXT,
            cost_usd REAL,
            input_bytes INTEGER,
            tool_calls INTEGER,
            model TEXT,
            role TEXT,
            workflow_id TEXT,
            fingerprint TEXT
        )
        """
    )
    conn.commit()
    return conn


def _mark_migrations_applied(conn: sqlite3.Connection, versions: range) -> None:
    migrations._ensure_migrations_table(conn)
    for version in versions:
        conn.execute(
            "INSERT INTO schema_migrations (version, name, applied_ts) VALUES (?, ?, ?)",
            (version, f"legacy_{version}", 0),
        )
    conn.commit()


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
        conn = _legacy_conn(db)
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


def test_log_columns_and_checkpoint_table_added():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = _legacy_conn(db)
        migrations.apply_pending(conn)
        event_cols = {row["name"] for row in conn.execute("PRAGMA table_info(events)").fetchall()}
        assert {"kind", "subject", "source_ref"}.issubset(event_cols)
        tables = {r[0] for r in conn.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()}
        assert "source_checkpoints" in tables
        conn.close()


def test_upgrade_existing_slice_db_can_insert_payload_json_events():
    """Existing DBs with migration 1 recorded but no payload_json can ingest logs."""
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = _legacy_conn(db)
        conn.execute("ALTER TABLE events ADD COLUMN source TEXT NOT NULL DEFAULT 'chain'")
        _mark_migrations_applied(conn, range(1, 7))

        migrations.apply_pending(conn)

        event_cols = {row["name"] for row in conn.execute("PRAGMA table_info(events)").fetchall()}
        assert {"kind", "subject", "source_ref", "payload_json"}.issubset(event_cols)
        assert insert_event(
            conn,
            source="hermes",
            kind="hermes_standup",
            ts="2026-05-13T08:00:00+00:00",
            source_ref="/tmp/hermes.log:1",
            subject="t_deadbeef",
            payload={"raw": "2026-05-13T08:00:00Z standup"},
        )
        row = conn.execute(
            "SELECT payload_json FROM events WHERE source = 'hermes'"
        ).fetchone()
        assert row["payload_json"] == '{"raw": "2026-05-13T08:00:00Z standup"}'
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
