"""agent-bus DB helpers: connection bootstrap + schema init.

Two responsibilities only:
- Resolve the DB path (env override > default `~/.chitin/agent-bus/bus.db`).
- Ensure the schema exists before the first read/write.

Per FR-008 the schema is additive-only — never repurpose a column.
Bumping `schema_version` is a one-row insert in the migration script;
this module assumes the SQL file is the single source of truth.
"""
from __future__ import annotations

import os
import sqlite3
from pathlib import Path

DEFAULT_DB_PATH = Path.home() / ".chitin" / "agent-bus" / "bus.db"
SCHEMA_FILE = Path(__file__).resolve().parent / "schema.sql"


def db_path() -> Path:
    override = os.environ.get("AGENT_BUS_DB")
    return Path(override) if override else DEFAULT_DB_PATH


def connect(path: Path | None = None) -> sqlite3.Connection:
    """Open a connection, ensure the schema is applied, return the connection.

    `sqlite3.Row` for dict-style access. WAL is set in schema.sql but the
    PRAGMA only takes effect once per file, so re-applying it here is cheap
    insurance for fresh files.
    """
    p = path or db_path()
    p.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(p))
    conn.row_factory = sqlite3.Row
    init_schema(conn)
    return conn


def init_schema(conn: sqlite3.Connection) -> None:
    sql = SCHEMA_FILE.read_text()
    conn.executescript(sql)
    conn.commit()
