"""Indexer: tail JSONL → SQLite with replay-safety via line_hash idempotency."""
from __future__ import annotations

import hashlib
import json
import sqlite3
from datetime import datetime
from pathlib import Path
from typing import Optional

from analysis.models import parse_decision_line


def _line_hash(line: str) -> str:
    """Deterministic hash of a decision line for idempotency."""
    return hashlib.sha256(line.encode()).hexdigest()


def init_db(db_path: Path) -> sqlite3.Connection:
    """Create index schema. Idempotent."""
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path))
    conn.execute("PRAGMA journal_mode=WAL")

    conn.execute("""
        CREATE TABLE IF NOT EXISTS events (
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
    """)

    conn.execute("""
        CREATE INDEX IF NOT EXISTS idx_ts_unix
        ON events(ts_unix)
    """)

    conn.execute("""
        CREATE INDEX IF NOT EXISTS idx_rule_id
        ON events(rule_id)
    """)

    conn.execute("""
        CREATE INDEX IF NOT EXISTS idx_allowed
        ON events(allowed)
    """)

    conn.execute("""
        CREATE INDEX IF NOT EXISTS idx_agent
        ON events(agent)
    """)

    conn.commit()
    return conn


def _parse_ts_unix(ts_str: str) -> Optional[int]:
    """Parse ISO 8601 ts to unix timestamp. Returns None on error."""
    if not isinstance(ts_str, str):
        return None
    try:
        dt = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
        return int(dt.timestamp())
    except (ValueError, TypeError):
        return None


def index_jsonl_file(conn: sqlite3.Connection, file_path: Path) -> tuple[int, int]:
    """Index a gov-decisions JSONL file. Returns (inserted, skipped_duplicates)."""
    inserted = 0
    skipped = 0

    if not file_path.exists():
        return inserted, skipped

    with file_path.open("r") as f:
        for line in f:
            line = line.rstrip("\n")
            if not line.strip():
                continue

            d = parse_decision_line(line)
            if d is None:
                continue

            lh = _line_hash(line)
            ts_unix = _parse_ts_unix(d.ts.isoformat())
            if ts_unix is None:
                continue

            try:
                conn.execute("""
                    INSERT INTO events (
                        line_hash, ts, ts_unix, allowed, mode, rule_id, reason,
                        escalation, agent, action_type, action_target, envelope_id,
                        tier, cost_usd, input_bytes, tool_calls, model, role,
                        workflow_id, fingerprint
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """, (
                    lh, d.ts.isoformat(), ts_unix, int(d.allowed),
                    d.mode, d.rule_id, d.reason, d.escalation,
                    d.agent, d.action_type, d.action_target, d.envelope_id,
                    d.tier, d.cost_usd, d.input_bytes, d.tool_calls,
                    d.model, d.role, d.workflow_id, d.fingerprint
                ))
                inserted += 1
            except sqlite3.IntegrityError:
                # Duplicate line_hash; skip (idempotent)
                skipped += 1
            except Exception:
                # Other errors (malformed data); skip
                continue

    conn.commit()
    return inserted, skipped


def tail_all_decisions(decisions_dir: Path) -> Path:
    """Create index.db in ~/.argus/ from all gov-decisions-*.jsonl files."""
    import re

    db_path = Path.home() / ".argus" / "index.db"
    conn = init_db(db_path)

    pattern = re.compile(r"^gov-decisions-\d{4}-\d{2}-\d{2}\.jsonl$")
    files = sorted([
        f for f in decisions_dir.iterdir()
        if pattern.match(f.name) and f.is_file()
    ])

    total_inserted = 0
    total_skipped = 0

    for f in files:
        inserted, skipped = index_jsonl_file(conn, f)
        total_inserted += inserted
        total_skipped += skipped

    conn.close()
    return db_path
