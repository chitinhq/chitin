"""Shared schema + open helpers for the cross-source (kanban + git) index.

Slice 2 keeps cross-source events in a separate table from the chain
`events` table — the column shapes are too different to merge cleanly
(chain has allowed/rule_id, kanban/git don't). Detectors that need to
join across sources read both tables.

Invariants:
    - Append-only. Idempotent on `dedup_key` UNIQUE.
    - Source attribution: every row carries `source` and `subject` (the
      task_id, commit sha, or PR number). Detectors must cite these.
    - Read-only consumers: detectors and report open the same DB with
      mode=ro URI. Only ingesters write.
"""
from __future__ import annotations

import sqlite3
from pathlib import Path


def init_cross_source_db(db_path: Path) -> sqlite3.Connection:
    """Create the cross_source_events table + indexes. Idempotent."""
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path))
    conn.execute("PRAGMA journal_mode=WAL")

    conn.execute("""
        CREATE TABLE IF NOT EXISTS cross_source_events (
            id INTEGER PRIMARY KEY,
            source TEXT NOT NULL,
            kind TEXT NOT NULL,
            ts_unix INTEGER NOT NULL,
            subject TEXT NOT NULL,
            actor TEXT,
            payload_json TEXT,
            dedup_key TEXT UNIQUE NOT NULL
        )
    """)
    conn.execute("CREATE INDEX IF NOT EXISTS idx_xs_source_ts ON cross_source_events(source, ts_unix)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_xs_kind_ts   ON cross_source_events(kind, ts_unix)")
    conn.execute("CREATE INDEX IF NOT EXISTS idx_xs_subject   ON cross_source_events(subject)")

    # Per-source watermark table: last successfully ingested timestamp
    # per (source, board/repo). Lets pollers resume without rescanning.
    conn.execute("""
        CREATE TABLE IF NOT EXISTS ingest_watermarks (
            source TEXT NOT NULL,
            scope TEXT NOT NULL,
            last_ts_unix INTEGER NOT NULL,
            PRIMARY KEY (source, scope)
        )
    """)

    conn.commit()
    return conn


def get_watermark(conn: sqlite3.Connection, source: str, scope: str) -> int:
    """Return last_ts_unix for (source, scope), or 0 if not set."""
    row = conn.execute(
        "SELECT last_ts_unix FROM ingest_watermarks WHERE source=? AND scope=?",
        (source, scope),
    ).fetchone()
    return int(row[0]) if row else 0


def set_watermark(conn: sqlite3.Connection, source: str, scope: str, ts_unix: int) -> None:
    """Upsert the watermark for (source, scope)."""
    conn.execute(
        """
        INSERT INTO ingest_watermarks (source, scope, last_ts_unix)
        VALUES (?, ?, ?)
        ON CONFLICT(source, scope) DO UPDATE SET
            last_ts_unix=excluded.last_ts_unix
        """,
        (source, scope, ts_unix),
    )
    conn.commit()


def open_readonly(db_path: Path) -> sqlite3.Connection:
    """Open the cross-source DB in read-only mode (mode=ro URI)."""
    conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    conn.execute("PRAGMA query_only = ON")
    conn.row_factory = sqlite3.Row
    return conn
