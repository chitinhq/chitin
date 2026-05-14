"""Indexer: tail JSONL → SQLite with replay-safety via line_hash idempotency."""
from __future__ import annotations

import hashlib
import json
import os
import sqlite3
import time
from datetime import datetime
from pathlib import Path
from typing import Optional

from argus import migrations
from analysis.models import parse_decision_line


def _line_hash(line: str) -> str:
    """Deterministic hash of a decision line for idempotency."""
    return hashlib.sha256(line.encode()).hexdigest()


def _source_line_hash(source: str, source_ref: str, line: str) -> str:
    """Deterministic per-source hash for non-chain ingest."""
    payload = f"{source}\0{source_ref}\0{line}"
    return hashlib.sha256(payload.encode()).hexdigest()


def init_db(db_path: Path) -> sqlite3.Connection:
    """Create index schema. Idempotent."""
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path))
    conn.execute("PRAGMA journal_mode=WAL")

    conn.execute("""
        CREATE TABLE IF NOT EXISTS events (
            id INTEGER PRIMARY KEY,
            line_hash TEXT UNIQUE NOT NULL,
            source TEXT NOT NULL DEFAULT 'chain',
            kind TEXT NOT NULL DEFAULT 'chain_decision',
            subject TEXT,
            source_ref TEXT,
            payload_json TEXT,
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

    conn.execute("""
        CREATE INDEX IF NOT EXISTS idx_source_ts
        ON events(source, ts_unix)
    """)

    conn.execute("""
        CREATE INDEX IF NOT EXISTS idx_kind_ts
        ON events(kind, ts_unix)
    """)

    conn.execute("""
        CREATE INDEX IF NOT EXISTS idx_subject_ts
        ON events(subject, ts_unix)
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



def _decision_files(decisions_dir: Path) -> list[Path]:
    """Return known gov decision JSONL files in deterministic order."""
    import re

    if not decisions_dir.exists():
        return []
    pattern = re.compile(r"^gov-decisions-\d{4}-\d{2}-\d{2}\.jsonl$")
    return sorted([
        f for f in decisions_dir.iterdir()
        if pattern.match(f.name) and f.is_file()
    ])


def index_jsonl_file_from_offset(
    conn: sqlite3.Connection, file_path: Path, offset: int = 0
) -> tuple[int, int, int]:
    """Index newly appended lines from offset; return (inserted, skipped, next_offset)."""
    if not file_path.exists():
        return 0, 0, offset

    with file_path.open("rb") as f:
        f.seek(offset)
        chunk = f.read()
        complete, next_offset = _split_complete_lines(chunk, offset)
        inserted = 0
        skipped = 0
        for raw in complete:
            if not raw.strip():
                continue

            d = parse_decision_line(raw)
            if d is None:
                continue

            lh = _line_hash(raw)
            ts_unix = _parse_ts_unix(d.ts.isoformat())
            if ts_unix is None:
                continue

            try:
                conn.execute("""
                    INSERT INTO events (
                        line_hash, source, kind, subject, source_ref, payload_json,
                        ts, ts_unix, allowed, mode, rule_id, reason,
                        escalation, agent, action_type, action_target, envelope_id,
                        tier, cost_usd, input_bytes, tool_calls, model, role,
                        workflow_id, fingerprint
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """, (
                    lh, "chain", "chain_decision", None, str(file_path), raw,
                    d.ts.isoformat(), ts_unix, int(d.allowed),
                    d.mode, d.rule_id, d.reason, d.escalation,
                    d.agent, d.action_type, d.action_target, d.envelope_id,
                    d.tier, d.cost_usd, d.input_bytes, d.tool_calls,
                    d.model, d.role, d.workflow_id, d.fingerprint
                ))
                inserted += 1
            except sqlite3.IntegrityError:
                skipped += 1
            except Exception:
                continue
    conn.commit()
    return inserted, skipped, next_offset

def index_jsonl_file(conn: sqlite3.Connection, file_path: Path) -> tuple[int, int]:
    """Index a gov-decisions JSONL file. Returns (inserted, skipped_duplicates)."""
    inserted, skipped, _ = index_jsonl_file_from_offset(conn, file_path, 0)
    return inserted, skipped

def tail_all_decisions(decisions_dir: Path) -> Path:
    """Create index.db in ~/.argus/ from all gov-decisions-*.jsonl files."""
    db_path = Path.home() / ".argus" / "index.db"
    conn = init_db(db_path)
    migrations.apply_pending(conn)

    for f in _decision_files(decisions_dir):
        index_jsonl_file(conn, f)

    conn.close()
    return db_path


def follow_all_decisions(decisions_dir: Path, poll_seconds: float = 1.0) -> Path:
    """Continuously index existing and appended gov decision lines.

    Handles three rotation/lifecycle edge cases:
      * date rollover — new files appear via _decision_files() each tick.
      * truncation — if file size drops below the stored offset, reset
        to 0 so newly-appended content from the start is not skipped.
      * deletion — drop offsets for files no longer listed so the dict
        does not grow unbounded across long-lived runs.
    """
    db_path = Path.home() / ".argus" / "index.db"
    conn = init_db(db_path)
    migrations.apply_pending(conn)
    offsets: dict[Path, int] = {}
    try:
        while True:
            current = list(_decision_files(decisions_dir))
            current_set = set(current)
            stale = [p for p in offsets if p not in current_set]
            for p in stale:
                offsets.pop(p, None)
            for f in current:
                offset = offsets.get(f, 0)
                try:
                    size = f.stat().st_size
                except FileNotFoundError:
                    offsets.pop(f, None)
                    continue
                if size < offset:
                    offset = 0
                inserted, skipped, next_offset = index_jsonl_file_from_offset(conn, f, offset)
                offsets[f] = next_offset
            time.sleep(poll_seconds)
    finally:
        conn.close()
    return db_path


def _split_complete_lines(data: bytes, offset: int) -> tuple[list[str], int]:
    """Return complete newline-terminated lines and the safe next offset."""
    if not data:
        return [], offset
    if data.endswith(b"\n"):
        complete = data
        next_offset = offset + len(data)
    else:
        nl = data.rfind(b"\n")
        if nl < 0:
            return [], offset
        complete = data[: nl + 1]
        next_offset = offset + nl + 1
    lines = [
        line.decode("utf-8", errors="replace").rstrip("\n")
        for line in complete.splitlines()
    ]
    return lines, next_offset


def insert_event(
    conn: sqlite3.Connection,
    *,
    source: str,
    kind: str,
    ts: str,
    source_ref: str,
    allowed: int = 1,
    subject: str | None = None,
    payload: dict | None = None,
    agent: str | None = None,
    action_type: str | None = None,
    action_target: str | None = None,
    reason: str | None = None,
    rule_id: str | None = None,
) -> bool:
    """Insert one external event; returns True when new."""
    ts_unix = _parse_ts_unix(ts)
    if ts_unix is None:
        return False
    payload_json = json.dumps(payload or {}, sort_keys=True)
    raw = payload_json if payload_json != "{}" else f"{kind}:{ts}:{subject or ''}"
    line_hash = _source_line_hash(source, source_ref, raw)
    try:
        conn.execute(
            """
            INSERT INTO events (
                line_hash, source, kind, subject, source_ref, payload_json,
                ts, ts_unix, allowed, agent, action_type, action_target, reason, rule_id
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                line_hash,
                source,
                kind,
                subject,
                source_ref,
                payload_json,
                ts,
                ts_unix,
                int(allowed),
                agent,
                action_type or kind,
                action_target,
                reason,
                rule_id,
            ),
        )
        conn.commit()
        return True
    except sqlite3.IntegrityError:
        return False


def checkpoint_key(source: str, path_or_name: str) -> str:
    """Stable source checkpoint key."""
    return f"{source}:{path_or_name}"


def file_inode(path: Path) -> int | None:
    """Best-effort inode read."""
    try:
        return int(path.stat().st_ino)
    except (FileNotFoundError, OSError, ValueError):
        return None


def env_path(name: str, default: str) -> Path:
    """Read a source path from env with ~ expansion."""
    return Path(os.environ.get(name, default)).expanduser()
