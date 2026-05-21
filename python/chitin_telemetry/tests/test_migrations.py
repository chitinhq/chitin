"""Tests for additive schema migrations."""
from __future__ import annotations

import sqlite3
import tempfile
from pathlib import Path

import pytest

from chitin_telemetry import migrations
from chitin_telemetry.indexer import init_db, insert_event, insert_source_event


def _legacy_conn(db: Path) -> sqlite3.Connection:
    """Create a pre-Slice-3 events table (no source/payload_json columns)."""
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


def _create_legacy_events_without_payload_json(conn):
    """Create a pre-cross-source events table (has source, no payload_json)
    and mark migrations 1-6 as already applied."""
    conn.execute(
        """
        CREATE TABLE events (
            id INTEGER PRIMARY KEY,
            line_hash TEXT UNIQUE NOT NULL,
            source TEXT NOT NULL DEFAULT 'chain',
            ts TEXT NOT NULL,
            ts_unix INTEGER NOT NULL,
            allowed INTEGER NOT NULL DEFAULT 1,
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
    migrations._ensure_migrations_table(conn)
    for version in range(1, 7):
        conn.execute(
            "INSERT INTO schema_migrations (version, name, applied_ts) VALUES (?, ?, 0)",
            (version, f"legacy_{version}"),
        )
    conn.commit()


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
            "evidence_links", "memory", "kernel_state", "beliefs",
            "schema_migrations",
        }:
            assert expected in names, f"missing table: {expected}"
        cols = {r[1] for r in conn.execute("PRAGMA table_info(events)").fetchall()}
        for expected in {"kind", "ticket_id", "pr_number", "commit_sha", "review_id", "status", "last_seen_ts"}:
            assert expected in cols, f"missing events column: {expected}"
        conn.close()


def test_boundary_empty_legacy_events_table_gets_payload_json():
    """empty: legacy upgraded DBs with no rows still gain payload_json."""
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = migrations.open_writable(db)
        _create_legacy_events_without_payload_json(conn)

        applied = migrations.apply_pending(conn)

        cols = {r[1] for r in conn.execute("PRAGMA table_info(events)").fetchall()}
        assert 8 in applied
        assert "payload_json" in cols
        assert conn.execute("SELECT COUNT(*) FROM events").fetchone()[0] == 0
        conn.close()


def test_boundary_max_cross_source_event_insert_after_legacy_migration():
    """max: all cross-source fields plus payload_json insert after upgrade."""
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = migrations.open_writable(db)
        _create_legacy_events_without_payload_json(conn)
        migrations.apply_pending(conn)

        event_id = insert_event(conn, {
            "line_hash": "max-cross-source",
            "external_id": "github:chitinhq/chitin:pull:652:review:1",
            "source": "github",
            "kind": "pr_review",
            "subject": "review requested changes",
            "ts": "2026-05-14T12:00:00+00:00",
            "ts_unix": 1778760000,
            "last_seen_ts": 1778760000,
            "allowed": 1,
            "mode": "observe",
            "rule_id": "chitin_telemetry.cross_source",
            "reason": "source ingest",
            "escalation": "none",
            "agent": "clawta",
            "action_type": "github.review",
            "action_target": "PR #652",
            "envelope_id": "env_652",
            "tier": "review",
            "cost_usd": 0.0,
            "input_bytes": 4096,
            "tool_calls": 3,
            "model": "local",
            "role": "reviewer",
            "workflow_id": "wf_652",
            "fingerprint": "fp_652",
            "payload_json": '{"state":"REQUEST_CHANGES"}',
            "repo": "chitinhq/chitin",
            "board": "swarm",
            "ticket_id": "t_670fd95f",
            "pr_number": 652,
            "commit_sha": "3fac0760e2a15dc597428f90c5a4d27998adea77",
            "review_id": "1",
            "file_path": "python/chitin_telemetry/migrations.py",
            "status": "open",
            "source_ref": "https://github.com/chitinhq/chitin/pull/652",
        })

        row = conn.execute(
            "SELECT payload_json, repo, pr_number, status FROM events WHERE id = ?",
            (event_id,),
        ).fetchone()
        assert row["payload_json"] == '{"state":"REQUEST_CHANGES"}'
        assert row["repo"] == "chitinhq/chitin"
        assert row["pr_number"] == 652
        assert row["status"] == "open"
        conn.close()


def test_boundary_error_missing_events_table_surfaces_operational_error():
    """error: migrations fail loudly when the base events table is absent."""
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "i.db"
        conn = migrations.open_writable(db)
        with pytest.raises(sqlite3.OperationalError, match="no such table: events"):
            migrations.apply_pending(conn)
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
        row = conn.execute("SELECT source, kind FROM events WHERE line_hash = 'h1'").fetchone()
        assert row["source"] == "chain"
        assert row["kind"] == "chain_decision"
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
        assert insert_source_event(
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
