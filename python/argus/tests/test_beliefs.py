"""Tests for belief ingestion adapters."""
from __future__ import annotations

import sqlite3
import tempfile
from pathlib import Path

from argus import beliefs, migrations
from argus.indexer import init_db


def _write_openclaw_memory_db(path: Path, rows: list[tuple[str, str, int]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    mem_conn = sqlite3.connect(path)
    mem_conn.execute(
        """
        CREATE TABLE chunks (
            id TEXT PRIMARY KEY,
            path TEXT NOT NULL,
            source TEXT NOT NULL,
            start_line INTEGER NOT NULL,
            end_line INTEGER NOT NULL,
            hash TEXT NOT NULL,
            model TEXT NOT NULL,
            text TEXT NOT NULL,
            embedding TEXT NOT NULL,
            updated_at INTEGER NOT NULL
        )
        """
    )
    for idx, (source_path, text, updated_at) in enumerate(rows, start=1):
        mem_conn.execute(
            """
            INSERT INTO chunks (id, path, source, start_line, end_line, hash, model, text, embedding, updated_at)
            VALUES (?, ?, 'memory', 1, 3, ?, 'm', ?, '[]', ?)
            """,
            (str(idx), source_path, f"h{idx}", text, updated_at),
        )
    mem_conn.commit()
    mem_conn.close()


def _belief_rows(conn: sqlite3.Connection) -> list[sqlite3.Row]:
    return conn.execute(
        "SELECT agent, subject, claim, source_kind, schema_version FROM beliefs ORDER BY id"
    ).fetchall()


def test_hermes_memory_ingests_markdown_sections():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "index.db"
        memory_root = Path(tmp) / "memories"
        memory_root.mkdir()
        (memory_root / "MEMORY.md").write_text(
            "# Project memory\n\n"
            "Infra: gateway :18789.\n\n"
            "§\n\n"
            "Board assign: clawta=dispatchable, red=needs operator.\n"
        )

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_hermes_memory(conn, roots=[memory_root])
        rows = _belief_rows(conn)
        conn.close()

        assert result.inserted >= 2
        assert any(row["agent"] == "hermes" for row in rows)
        assert any("gateway" in row["claim"] for row in rows)


def test_openclaw_memory_legacy_schema_version_stamped():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "index.db"
        memory_db = Path(tmp) / "clawta.sqlite"
        _write_openclaw_memory_db(memory_db, [("memory/task.md", "t_123 is P50 and active", 1715600000)])

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_openclaw_memory_db(
            conn,
            agent="clawta",
            db_path=memory_db,
            source_kind="clawta_memory",
        )
        rows = _belief_rows(conn)
        conn.close()

        assert result.inserted == 1
        assert rows[0]["schema_version"] == "legacy"


def test_clawta_default_reads_required_data_db_path(monkeypatch):
    with tempfile.TemporaryDirectory() as tmp:
        home = Path(tmp) / "home"
        db = Path(tmp) / "index.db"
        required_db = home / ".openclaw" / "data" / "clawta.db"
        _write_openclaw_memory_db(required_db, [("memory/task.md", "t_required is P50 and active", 1715600000)])
        monkeypatch.setattr(beliefs.Path, "home", lambda: home)

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_beliefs(conn, include_clawta=True)
        rows = _belief_rows(conn)
        conn.close()

        assert result.inserted == 1
        assert rows[0]["agent"] == "clawta"
        assert rows[0]["subject"] == "t_required"


def test_clawta_default_reads_legacy_memory_sqlite_path(monkeypatch):
    with tempfile.TemporaryDirectory() as tmp:
        home = Path(tmp) / "home"
        db = Path(tmp) / "index.db"
        legacy_db = home / ".openclaw" / "memory" / "clawta.sqlite"
        _write_openclaw_memory_db(legacy_db, [("memory/task.md", "t_legacy is P30 and active", 1715600000)])
        monkeypatch.setattr(beliefs.Path, "home", lambda: home)

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_beliefs(conn, include_clawta=True)
        rows = _belief_rows(conn)
        conn.close()

        assert result.inserted == 1
        assert rows[0]["agent"] == "clawta"
        assert rows[0]["subject"] == "t_legacy"


def test_clawta_empty_boundary_missing_default_paths_skips_without_alert(monkeypatch):
    with tempfile.TemporaryDirectory() as tmp:
        home = Path(tmp) / "home"
        db = Path(tmp) / "index.db"
        monkeypatch.setattr(beliefs.Path, "home", lambda: home)

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_beliefs(conn, include_clawta=True)
        count = conn.execute("SELECT COUNT(*) AS cnt FROM beliefs").fetchone()["cnt"]
        conn.close()

        assert result.inserted == 0
        assert result.skipped == 0
        assert result.alerts == ()
        assert count == 0


def test_openclaw_memory_max_boundary_truncates_claim_to_800_chars():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "index.db"
        memory_db = Path(tmp) / "clawta.sqlite"
        long_claim = "t_max " + ("x" * 900)
        _write_openclaw_memory_db(memory_db, [("memory/task.md", long_claim, 1715600000)])

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_openclaw_memory_db(
            conn,
            agent="clawta",
            db_path=memory_db,
            source_kind="clawta_memory",
        )
        row = conn.execute("SELECT claim FROM beliefs").fetchone()
        conn.close()

        assert result.inserted == 1
        assert len(row["claim"]) == 800


def test_openclaw_memory_error_boundary_invalid_store_alerts():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "index.db"
        invalid_db = Path(tmp) / "encrypted.sqlite"
        invalid_db.write_text("not a sqlite database")

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_openclaw_memory_db(
            conn,
            agent="clawta",
            db_path=invalid_db,
            source_kind="clawta_memory",
        )
        count = conn.execute("SELECT COUNT(*) AS cnt FROM beliefs").fetchone()["cnt"]
        conn.close()

        assert result.inserted == 0
        assert count == 0
        assert result.alerts
        assert "operator key" in result.alerts[0].message or "readable schema" in result.alerts[0].message


def test_encrypted_or_invalid_memory_store_skips_with_alert():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "index.db"
        invalid_db = Path(tmp) / "encrypted.sqlite"
        invalid_db.write_text("not a sqlite database")

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_openclaw_memory_db(
            conn,
            agent="glm-agent",
            db_path=invalid_db,
            source_kind="openclaw_agent_memory",
        )
        row = conn.execute("SELECT COUNT(*) AS cnt FROM beliefs").fetchone()
        conn.close()

        assert result.inserted == 0
        assert row["cnt"] == 0
        assert result.alerts
        assert "operator key" in result.alerts[0].message or "valid sqlite" in result.alerts[0].message


def test_wiki_article_without_frontmatter_degrades_to_heading_beliefs():
    with tempfile.TemporaryDirectory() as tmp:
        db = Path(tmp) / "index.db"
        wiki_root = Path(tmp) / "wiki"
        wiki_root.mkdir()
        (wiki_root / "topic.md").write_text(
            "# Ticket t_999\n\n"
            "## Decision\n\n"
            "The task is P50 and ready for review.\n"
        )

        conn = init_db(db)
        migrations.apply_pending(conn)
        result = beliefs.ingest_wiki_graph(conn, roots=[wiki_root])
        rows = _belief_rows(conn)
        conn.close()

        assert result.inserted >= 1
        assert any(row["agent"] == "wiki" for row in rows)
        assert any(row["subject"] == "t_999" for row in rows)
