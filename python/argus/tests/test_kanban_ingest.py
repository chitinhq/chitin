"""Tests for the kanban poller — read-only, idempotent, locked-DB tolerant."""
from __future__ import annotations

import json
import sqlite3
import tempfile
from pathlib import Path

import pytest

from argus.cross_source_db import init_cross_source_db
from argus.kanban_ingest import ingest_board, ingest_all_kanban


def _make_kanban_db(path: Path) -> sqlite3.Connection:
    """Create a minimal kanban DB with the schema the real hermes board uses."""
    conn = sqlite3.connect(str(path))
    conn.executescript("""
        CREATE TABLE task_events (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id TEXT NOT NULL,
            run_id INTEGER,
            kind TEXT NOT NULL,
            payload TEXT,
            created_at INTEGER NOT NULL
        );
        CREATE TABLE task_comments (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            task_id TEXT NOT NULL,
            author TEXT NOT NULL,
            body TEXT NOT NULL,
            created_at INTEGER NOT NULL
        );
    """)
    return conn


def _add_event(conn, task_id, kind, payload, created_at):
    conn.execute(
        "INSERT INTO task_events (task_id, kind, payload, created_at) VALUES (?, ?, ?, ?)",
        (task_id, kind, json.dumps(payload), created_at),
    )
    conn.commit()


def _add_comment(conn, task_id, author, body, created_at):
    conn.execute(
        "INSERT INTO task_comments (task_id, author, body, created_at) VALUES (?, ?, ?, ?)",
        (task_id, author, body, created_at),
    )
    conn.commit()


class TestIngestBoard:
    def test_empty_board_zero_inserts(self):
        """An empty kanban DB yields zero inserts, no error."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            kanban_db = tmpdir / "board" / "kanban.db"
            kanban_db.parent.mkdir()
            _make_kanban_db(kanban_db).close()

            xs_db = tmpdir / "xs.db"
            xs_conn = init_cross_source_db(xs_db)
            try:
                inserted, skipped = ingest_board(kanban_db, xs_conn, board="board")
            finally:
                xs_conn.close()

            assert inserted == 0
            assert skipped == 0

    def test_status_transition_ingested(self):
        """A ready→triage transition lands as kanban_status_transition."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            kanban_db = tmpdir / "board" / "kanban.db"
            kanban_db.parent.mkdir()
            kbn = _make_kanban_db(kanban_db)
            _add_event(kbn, "t_abc", "status_transition",
                       {"from": "ready", "to": "triage", "by": "red"}, 1000)
            kbn.close()

            xs_db = tmpdir / "xs.db"
            xs_conn = init_cross_source_db(xs_db)
            try:
                inserted, skipped = ingest_board(kanban_db, xs_conn, board="board")
                rows = xs_conn.execute(
                    "SELECT source, kind, ts_unix, subject FROM cross_source_events"
                ).fetchall()
            finally:
                xs_conn.close()

            assert inserted == 1
            assert rows[0] == ("kanban", "kanban_status_transition", 1000, "t_abc")

    def test_idempotent_on_replay(self):
        """Running the ingester twice over the same data inserts 0 the second time."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            kanban_db = tmpdir / "board" / "kanban.db"
            kanban_db.parent.mkdir()
            kbn = _make_kanban_db(kanban_db)
            _add_event(kbn, "t_abc", "status_transition",
                       {"from": "ready", "to": "triage", "by": "red"}, 1000)
            _add_comment(kbn, "t_abc", "red", "demoted because...", 1001)
            kbn.close()

            xs_db = tmpdir / "xs.db"
            # First pass.
            xs_conn = init_cross_source_db(xs_db)
            i1, s1 = ingest_board(kanban_db, xs_conn, board="board")
            xs_conn.close()
            # Second pass — watermark advance prevents re-scan.
            xs_conn = init_cross_source_db(xs_db)
            try:
                i2, s2 = ingest_board(kanban_db, xs_conn, board="board")
                total = xs_conn.execute(
                    "SELECT COUNT(*) FROM cross_source_events"
                ).fetchone()[0]
            finally:
                xs_conn.close()

            assert i1 == 2
            assert i2 == 0  # watermark blocks the second pass entirely
            assert total == 2

    def test_incremental_after_watermark(self):
        """New rows added after first poll are picked up on the next poll."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            kanban_db = tmpdir / "board" / "kanban.db"
            kanban_db.parent.mkdir()
            kbn = _make_kanban_db(kanban_db)
            _add_event(kbn, "t_abc", "status_transition",
                       {"from": "ready", "to": "triage"}, 1000)
            kbn.close()

            xs_db = tmpdir / "xs.db"
            xs_conn = init_cross_source_db(xs_db)
            ingest_board(kanban_db, xs_conn, board="board")
            xs_conn.close()

            # Add a fresh event after first poll completed.
            kbn = sqlite3.connect(str(kanban_db))
            _add_event(kbn, "t_abc", "status_transition",
                       {"from": "triage", "to": "ready"}, 2000)
            kbn.close()

            xs_conn = init_cross_source_db(xs_db)
            try:
                i2, _ = ingest_board(kanban_db, xs_conn, board="board")
                rows = xs_conn.execute(
                    "SELECT ts_unix FROM cross_source_events ORDER BY ts_unix"
                ).fetchall()
            finally:
                xs_conn.close()
            assert i2 == 1
            assert [r[0] for r in rows] == [1000, 2000]

    def test_missing_board_db_returns_zero(self):
        """Non-existent kanban.db path is a no-op."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            xs_conn = init_cross_source_db(tmpdir / "xs.db")
            try:
                inserted, skipped = ingest_board(tmpdir / "nope" / "kanban.db", xs_conn)
            finally:
                xs_conn.close()
            assert (inserted, skipped) == (0, 0)


class TestIngestAllKanban:
    def test_discovers_multiple_boards(self):
        """ingest_all_kanban finds every kanban.db under the boards root."""
        with tempfile.TemporaryDirectory() as tmpdir:
            tmpdir = Path(tmpdir)
            for board in ("a", "b"):
                p = tmpdir / board / "kanban.db"
                p.parent.mkdir()
                k = _make_kanban_db(p)
                _add_event(k, f"t_{board}", "status_transition",
                           {"from": "ready", "to": "triage"}, 1000)
                k.close()

            result = ingest_all_kanban(tmpdir, tmpdir / "xs.db")
            assert set(result["boards"]) == {"a", "b"}
            assert result["inserted"] == 2

    def test_no_boards_root_returns_zero(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            result = ingest_all_kanban(Path(tmpdir) / "missing", Path(tmpdir) / "xs.db")
            assert result["boards"] == []
            assert result["inserted"] == 0
