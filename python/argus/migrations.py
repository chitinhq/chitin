"""Additive schema migrations for the Argus index.

Each migration is idempotent. Migrations are tracked in
`schema_migrations(version, applied_ts)` so they apply once, in order,
across restarts. The user_version PRAGMA is bumped after each batch.

Migrations are append-only. To revise a migration, add a new one — never
mutate the migration list, which would break existing operator installs.
"""
from __future__ import annotations

import json
import sqlite3
import time
from pathlib import Path

# A migration is (version_int, name_str, list_of_sql_statements).
# Order matters and must be monotonic. Statements run in a single
# transaction per migration.
MIGRATIONS: list[tuple[int, str, list[str]]] = [
    (
        1,
        "events_source_and_payload",
        [
            # source column: 'chain' for existing rows, other ingesters set their own.
            "ALTER TABLE events ADD COLUMN source TEXT NOT NULL DEFAULT 'chain'",
            "ALTER TABLE events ADD COLUMN payload_json TEXT",
            "CREATE INDEX IF NOT EXISTS idx_source_ts ON events(source, ts_unix)",
        ],
    ),
    (
        2,
        "findings_table",
        [
            """
            CREATE TABLE IF NOT EXISTS findings (
                id INTEGER PRIMARY KEY,
                finding_hash TEXT UNIQUE NOT NULL,
                ts_unix INTEGER NOT NULL,
                detector TEXT NOT NULL,
                severity TEXT NOT NULL,
                title TEXT NOT NULL,
                body TEXT NOT NULL,
                citations TEXT NOT NULL DEFAULT '[]',
                superseded_by INTEGER REFERENCES findings(id),
                operator_action TEXT,
                operator_action_ts INTEGER,
                pushed_ts INTEGER
            )
            """,
            "CREATE INDEX IF NOT EXISTS idx_findings_ts ON findings(ts_unix)",
            "CREATE INDEX IF NOT EXISTS idx_findings_detector ON findings(detector)",
            "CREATE INDEX IF NOT EXISTS idx_findings_severity ON findings(severity)",
        ],
    ),
    (
        3,
        "llm_calls_table",
        [
            """
            CREATE TABLE IF NOT EXISTS llm_calls (
                id INTEGER PRIMARY KEY,
                ts_unix INTEGER NOT NULL,
                purpose TEXT NOT NULL,
                prompt_hash TEXT NOT NULL,
                prompt TEXT NOT NULL,
                response TEXT,
                tokens_in INTEGER,
                tokens_out INTEGER,
                duration_ms INTEGER,
                judge_verdict TEXT,
                judge_reason TEXT,
                retention_ts INTEGER NOT NULL,
                redaction_applied INTEGER NOT NULL DEFAULT 0
            )
            """,
            "CREATE INDEX IF NOT EXISTS idx_llm_ts ON llm_calls(ts_unix)",
            "CREATE INDEX IF NOT EXISTS idx_llm_purpose ON llm_calls(purpose)",
            "CREATE INDEX IF NOT EXISTS idx_llm_prompt_hash ON llm_calls(prompt_hash)",
            "CREATE INDEX IF NOT EXISTS idx_llm_retention ON llm_calls(retention_ts)",
        ],
    ),
    (
        4,
        "hypotheses_table",
        [
            """
            CREATE TABLE IF NOT EXISTS hypotheses (
                id INTEGER PRIMARY KEY,
                slug TEXT UNIQUE NOT NULL,
                created_ts INTEGER NOT NULL,
                title TEXT NOT NULL,
                statement TEXT NOT NULL,
                rationale TEXT NOT NULL,
                status TEXT NOT NULL DEFAULT 'open',
                evidence TEXT NOT NULL DEFAULT '[]',
                next_action TEXT,
                verdict TEXT,
                verdict_ts INTEGER,
                verdict_body TEXT,
                eig_score REAL NOT NULL DEFAULT 0.5,
                supersedes_id INTEGER REFERENCES hypotheses(id),
                last_advanced_ts INTEGER
            )
            """,
            "CREATE INDEX IF NOT EXISTS idx_hyp_status ON hypotheses(status)",
            "CREATE INDEX IF NOT EXISTS idx_hyp_eig ON hypotheses(eig_score DESC)",
            """
            CREATE TABLE IF NOT EXISTS evidence_links (
                id INTEGER PRIMARY KEY,
                hypothesis_id INTEGER NOT NULL REFERENCES hypotheses(id),
                ref_kind TEXT NOT NULL,
                ref_id TEXT NOT NULL,
                weight REAL NOT NULL DEFAULT 1.0,
                added_ts INTEGER NOT NULL,
                UNIQUE(hypothesis_id, ref_kind, ref_id)
            )
            """,
            "CREATE INDEX IF NOT EXISTS idx_evlinks_hyp ON evidence_links(hypothesis_id)",
            "CREATE INDEX IF NOT EXISTS idx_evlinks_ref ON evidence_links(ref_kind, ref_id)",
        ],
    ),
    (
        5,
        "memory_table",
        [
            """
            CREATE TABLE IF NOT EXISTS memory (
                id INTEGER PRIMARY KEY,
                slug TEXT UNIQUE NOT NULL,
                created_ts INTEGER NOT NULL,
                last_seen_ts INTEGER NOT NULL,
                kind TEXT NOT NULL,
                title TEXT NOT NULL,
                body TEXT NOT NULL,
                tags TEXT NOT NULL DEFAULT '[]',
                weight REAL NOT NULL DEFAULT 1.0,
                citations TEXT NOT NULL DEFAULT '[]',
                pinned INTEGER NOT NULL DEFAULT 0,
                archived_ts INTEGER,
                source_hypothesis_id INTEGER REFERENCES hypotheses(id)
            )
            """,
            "CREATE INDEX IF NOT EXISTS idx_memory_kind ON memory(kind)",
            "CREATE INDEX IF NOT EXISTS idx_memory_weight ON memory(weight DESC)",
            "CREATE INDEX IF NOT EXISTS idx_memory_pinned ON memory(pinned)",
        ],
    ),
    (
        6,
        "kernel_state",
        [
            """
            CREATE TABLE IF NOT EXISTS kernel_state (
                key TEXT PRIMARY KEY,
                value TEXT NOT NULL,
                updated_ts INTEGER NOT NULL
            )
            """,
        ],
    ),
    (
        7,
        "cross_source_events",
        [
            "ALTER TABLE events ADD COLUMN external_id TEXT",
            "ALTER TABLE events ADD COLUMN kind TEXT NOT NULL DEFAULT 'chain_decision'",
            "ALTER TABLE events ADD COLUMN subject TEXT",
            "ALTER TABLE events ADD COLUMN repo TEXT",
            "ALTER TABLE events ADD COLUMN board TEXT",
            "ALTER TABLE events ADD COLUMN ticket_id TEXT",
            "ALTER TABLE events ADD COLUMN pr_number INTEGER",
            "ALTER TABLE events ADD COLUMN commit_sha TEXT",
            "ALTER TABLE events ADD COLUMN review_id TEXT",
            "ALTER TABLE events ADD COLUMN file_path TEXT",
            "ALTER TABLE events ADD COLUMN status TEXT",
            "ALTER TABLE events ADD COLUMN last_seen_ts INTEGER",
            "ALTER TABLE events ADD COLUMN source_ref TEXT",
            "CREATE UNIQUE INDEX IF NOT EXISTS idx_events_external_id ON events(external_id)",
            "CREATE INDEX IF NOT EXISTS idx_kind_ts ON events(kind, ts_unix)",
            "CREATE INDEX IF NOT EXISTS idx_ticket_ts ON events(ticket_id, ts_unix)",
            "CREATE INDEX IF NOT EXISTS idx_pr_ts ON events(pr_number, ts_unix)",
            "CREATE INDEX IF NOT EXISTS idx_repo_ts ON events(repo, ts_unix)",
            "CREATE INDEX IF NOT EXISTS idx_commit_sha ON events(commit_sha)",
            "CREATE INDEX IF NOT EXISTS idx_file_path ON events(file_path)",
        ],
    ),
    (
        8,
        "events_payload_json_legacy_backfill",
        [
            # Some pre-cross-source operator DBs have migration 1 recorded
            # from before payload_json existed. Cross-source ingest needs it.
            "ALTER TABLE events ADD COLUMN payload_json TEXT",
        ],
    ),
    (
        9,
        "log_subject_index",
        [
            # Log ingestion (Slice 3) queries events by `subject`. The
            # `kind`, `subject`, and `source_ref` columns are already added
            # by migration 7 (cross_source_events); only the subject index
            # is still missing. Idempotent on DBs that somehow have it.
            "CREATE INDEX IF NOT EXISTS idx_subject_ts ON events(subject, ts_unix)",
        ],
    ),
    (
        10,
        "source_checkpoints",
        [
            """
            CREATE TABLE IF NOT EXISTS source_checkpoints (
                source_key TEXT PRIMARY KEY,
                source TEXT NOT NULL,
                path TEXT,
                offset INTEGER NOT NULL DEFAULT 0,
                inode INTEGER,
                cursor TEXT,
                updated_ts INTEGER NOT NULL,
                meta_json TEXT NOT NULL DEFAULT '{}'
            )
            """,
            "CREATE INDEX IF NOT EXISTS idx_source_checkpoints_source ON source_checkpoints(source)",
        ],
    ),
    (
        11,
        "beliefs_table",
        [
            """
            CREATE TABLE IF NOT EXISTS beliefs (
                id INTEGER PRIMARY KEY,
                belief_hash TEXT UNIQUE NOT NULL,
                agent TEXT NOT NULL,
                subject TEXT NOT NULL,
                claim TEXT NOT NULL,
                ts_recorded INTEGER NOT NULL,
                source_path TEXT NOT NULL,
                source_kind TEXT NOT NULL,
                schema_version TEXT NOT NULL DEFAULT 'v1',
                private INTEGER NOT NULL DEFAULT 0,
                created_ts INTEGER NOT NULL
            )
            """,
            "CREATE INDEX IF NOT EXISTS idx_beliefs_agent_subject ON beliefs(agent, subject)",
            "CREATE INDEX IF NOT EXISTS idx_beliefs_ts ON beliefs(ts_recorded DESC)",
            "CREATE INDEX IF NOT EXISTS idx_beliefs_subject ON beliefs(subject)",
            "CREATE INDEX IF NOT EXISTS idx_beliefs_source_kind ON beliefs(source_kind)",
        ],
    ),
]


def _ensure_migrations_table(conn: sqlite3.Connection) -> None:
    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            applied_ts INTEGER NOT NULL
        )
        """
    )


def applied_versions(conn: sqlite3.Connection) -> set[int]:
    _ensure_migrations_table(conn)
    cur = conn.execute("SELECT version FROM schema_migrations")
    return {r[0] for r in cur.fetchall()}


def apply_pending(conn: sqlite3.Connection) -> list[int]:
    """Apply all pending migrations. Returns list of applied versions."""
    # Set row_factory so callers can use string-indexed row access.
    # Callers passing in their own factory (e.g. detectors with their own
    # _get_conn) typically already have this set — re-setting is harmless.
    conn.row_factory = sqlite3.Row
    applied = applied_versions(conn)
    newly_applied: list[int] = []
    now = int(time.time())
    for version, name, statements in MIGRATIONS:
        if version in applied:
            continue
        try:
            conn.execute("BEGIN IMMEDIATE")
            for stmt in statements:
                conn.execute(stmt)
            conn.execute(
                "INSERT INTO schema_migrations (version, name, applied_ts) VALUES (?, ?, ?)",
                (version, name, now),
            )
            conn.execute(f"PRAGMA user_version = {version}")
            conn.commit()
            newly_applied.append(version)
        except sqlite3.OperationalError as e:
            conn.rollback()
            # ALTER TABLE ADD COLUMN fails if column exists. Treat as applied.
            if "duplicate column name" in str(e).lower():
                conn.execute(
                    "INSERT OR IGNORE INTO schema_migrations (version, name, applied_ts) VALUES (?, ?, ?)",
                    (version, name, now),
                )
                conn.commit()
                newly_applied.append(version)
                continue
            raise
    return newly_applied


def integrity_check(conn: sqlite3.Connection) -> str:
    """Run SQLite integrity_check. Returns 'ok' or first error line."""
    row = conn.execute("PRAGMA integrity_check").fetchone()
    return row[0] if row else "unknown"


def open_readonly(db_path: Path | str) -> sqlite3.Connection:
    """Open a strictly read-only connection via SQLite URI mode."""
    uri = f"file:{db_path}?mode=ro"
    conn = sqlite3.connect(uri, uri=True)
    conn.execute("PRAGMA query_only = ON")
    conn.row_factory = sqlite3.Row
    return conn


def open_writable(db_path: Path | str) -> sqlite3.Connection:
    """Open a writable connection with WAL journaling."""
    conn = sqlite3.connect(str(db_path))
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA foreign_keys=ON")
    conn.row_factory = sqlite3.Row
    return conn


def get_checkpoint(conn: sqlite3.Connection, source_key: str) -> sqlite3.Row | None:
    """Return one source checkpoint row, if present."""
    return conn.execute(
        "SELECT * FROM source_checkpoints WHERE source_key = ?",
        (source_key,),
    ).fetchone()


def upsert_checkpoint(
    conn: sqlite3.Connection,
    *,
    source_key: str,
    source: str,
    path: str | None,
    offset: int = 0,
    inode: int | None = None,
    cursor: str | None = None,
    meta: dict | None = None,
) -> None:
    """Persist a checkpoint for a tailed source."""
    conn.execute(
        """
        INSERT INTO source_checkpoints (
            source_key, source, path, offset, inode, cursor, updated_ts, meta_json
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(source_key) DO UPDATE SET
            source = excluded.source,
            path = excluded.path,
            offset = excluded.offset,
            inode = excluded.inode,
            cursor = excluded.cursor,
            updated_ts = excluded.updated_ts,
            meta_json = excluded.meta_json
        """,
        (
            source_key,
            source,
            path,
            int(offset),
            inode,
            cursor,
            int(time.time()),
            json.dumps(meta or {}, sort_keys=True),
        ),
    )
    conn.commit()
