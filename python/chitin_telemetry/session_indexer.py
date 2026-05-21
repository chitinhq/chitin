"""Session indexer: watches codex-events-*.jsonl and Claude Code session JSONL
files, upserts their contents into chain_index.sqlite within 60 seconds of file
creation or modification.

Design:
- Extends chain_index.sqlite with an events table that holds per-line records
  from Codex events and Claude Code session files.
- Adds a driver_type column ('codex' | 'claude-code') to distinguish sources.
- Idempotent: each line is hashed (source + line content) so re-processing a
  file does not duplicate rows.
- Uses filesystem polling (not inotify) for cross-platform robustness. Polling
  interval defaults to 5 seconds; initial bulk scan must complete within 60s.
- Checkpoints file offsets so subsequent scans only process new lines.
"""
from __future__ import annotations

import hashlib
import json
import os
import re
import sqlite3
import time
from pathlib import Path
from typing import Optional

# ---------------------------------------------------------------------------
# Schema
# ---------------------------------------------------------------------------

EVENTS_TABLE_DDL = """
CREATE TABLE IF NOT EXISTS session_events (
    id          INTEGER PRIMARY KEY,
    line_hash   TEXT UNIQUE NOT NULL,
    driver_type TEXT NOT NULL,           -- 'codex' | 'claude-code'
    chain_id    TEXT NOT NULL DEFAULT '',
    session_id  TEXT NOT NULL DEFAULT '',
    event_type  TEXT NOT NULL DEFAULT '',
    ts          TEXT NOT NULL DEFAULT '',
    ts_unix     INTEGER NOT NULL DEFAULT 0,
    agent       TEXT NOT NULL DEFAULT '',
    surface     TEXT NOT NULL DEFAULT '',
    action_type TEXT NOT NULL DEFAULT '',
    action_target TEXT NOT NULL DEFAULT '',
    tool_name   TEXT NOT NULL DEFAULT '',
    decision    TEXT NOT NULL DEFAULT '',
    rule_id     TEXT NOT NULL DEFAULT '',
    reason      TEXT NOT NULL DEFAULT '',
    escalation  TEXT NOT NULL DEFAULT '',
    mode        TEXT NOT NULL DEFAULT '',
    cwd         TEXT NOT NULL DEFAULT '',
    model       TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL DEFAULT '',
    fingerprint TEXT NOT NULL DEFAULT '',
    workflow_id TEXT NOT NULL DEFAULT '',
    envelope_id TEXT NOT NULL DEFAULT '',
    payload_json TEXT,
    source_file TEXT NOT NULL,
    source_line  INTEGER NOT NULL DEFAULT 0,
    last_seen_ts INTEGER NOT NULL DEFAULT 0
);
"""

EVENTS_INDEXES = [
    "CREATE INDEX IF NOT EXISTS idx_se_ts_unix ON session_events(ts_unix);",
    "CREATE INDEX IF NOT EXISTS idx_se_chain_id ON session_events(chain_id);",
    "CREATE INDEX IF NOT EXISTS idx_se_session_id ON session_events(session_id, ts_unix);",
    "CREATE INDEX IF NOT EXISTS idx_se_driver_type ON session_events(driver_type, ts_unix);",
    "CREATE INDEX IF NOT EXISTS idx_se_event_type ON session_events(event_type, ts_unix);",
    "CREATE INDEX IF NOT EXISTS idx_se_action_type ON session_events(action_type, ts_unix);",
    "CREATE INDEX IF NOT EXISTS idx_se_decision ON session_events(decision);",
    "CREATE INDEX IF NOT EXISTS idx_se_rule_id ON session_events(rule_id);",
    "CREATE INDEX IF NOT EXISTS idx_se_workflow ON session_events(workflow_id);",
]

CHECKPOINT_TABLE_DDL = """
CREATE TABLE IF NOT EXISTS session_index_checkpoints (
    source_key  TEXT PRIMARY KEY,
    file_path   TEXT NOT NULL,
    offset      INTEGER NOT NULL DEFAULT 0,
    file_mtime  REAL NOT NULL DEFAULT 0,
    file_size   INTEGER NOT NULL DEFAULT 0,
    inode       INTEGER NOT NULL DEFAULT 0,
    updated_at  REAL NOT NULL DEFAULT 0
);
"""

# Migration: add driver_type to chains if missing
MIGRATIONS = [
    "ALTER TABLE chains ADD COLUMN driver_type TEXT NOT NULL DEFAULT ''",
    "CREATE INDEX IF NOT EXISTS idx_chains_driver_type ON chains(driver_type);",
]

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

_CODEX_EVENT_RE = re.compile(r"^codex-events-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$")
_CHAIN_EVENT_RE = re.compile(r"^events-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$")
_CC_SESSION_RE = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$")


def _parse_ts_unix(ts_str: str) -> int:
    """Parse RFC3339/ISO8601 timestamp to unix int. Returns 0 on failure."""
    if not ts_str:
        return 0
    from datetime import datetime, timezone
    for fmt in ("%Y-%m-%dT%H:%M:%S.%fZ", "%Y-%m-%dT%H:%M:%SZ",
                "%Y-%m-%dT%H:%M:%S.%f%z", "%Y-%m-%dT%H:%M:%S%z"):
        try:
            dt = datetime.strptime(ts_str, fmt)
            if dt.tzinfo is None:
                dt = dt.replace(tzinfo=timezone.utc)
            return int(dt.timestamp())
        except ValueError:
            continue
    # Try fromisoformat as last resort
    try:
        dt = datetime.fromisoformat(ts_str.replace("Z", "+00:00"))
        return int(dt.timestamp())
    except (ValueError, TypeError):
        return 0


def _line_hash(source: str, line: str) -> str:
    """Deterministic hash for idempotency: sha256(source\\0line)."""
    h = hashlib.sha256()
    h.update(source.encode())
    h.update(b"\0")
    h.update(line.encode())
    return h.hexdigest()


def _source_file_key(driver_type: str, file_path: Path) -> str:
    """Stable key for checkpoint tracking."""
    return f"{driver_type}:{file_path}"


# ---------------------------------------------------------------------------
# DB initialization
# ---------------------------------------------------------------------------

def init_session_db(db_path: Path) -> sqlite3.Connection:
    """Create or open chain_index.sqlite with the session_events schema."""
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA busy_timeout = 5000")

    # Create tables
    conn.execute(EVENTS_TABLE_DDL)
    for idx_sql in EVENTS_INDEXES:
        conn.execute(idx_sql)
    conn.execute(CHECKPOINT_TABLE_DDL)

    # Run migrations (extend existing chains table — only if table exists)
    tables = {row[0] for row in conn.execute(
        "SELECT name FROM sqlite_master WHERE type='table'"
    ).fetchall()}
    if "chains" in tables:
        for mig in MIGRATIONS:
            try:
                conn.execute(mig)
            except sqlite3.OperationalError as e:
                if "duplicate column" not in str(e).lower():
                    raise
    conn.commit()
    return conn


# ---------------------------------------------------------------------------
# Codex events parser
# ---------------------------------------------------------------------------

def parse_codex_line(raw: str) -> Optional[dict]:
    """Parse a single codex-events JSONL line into an event dict.
    
    Codex event format (v1, codex-events-*.jsonl):
    {
      "ts": "2026-05-13T14:12:12.994Z",
      "chain_id": "019e21ae-...",
      "event_type": "decision" | "task_start" | ...,
      "payload": {
        "tool_name": "...",
        "action_type": "...",
        "action_target": "...",
        "decision": "allow" | "deny",
        "rule_id": "...",
        ...
      }
    }
    """
    try:
        d = json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return None

    chain_id = d.get("chain_id", "")
    event_type = d.get("event_type", "")
    ts = d.get("ts", "")
    if not chain_id and not event_type:
        return None

    payload = d.get("payload", {})
    if isinstance(payload, str):
        try:
            payload = json.loads(payload)
        except (json.JSONDecodeError, TypeError):
            payload = {}

    labels = d.get("labels", {}) or {}

    return {
        "chain_id": chain_id,
        "session_id": d.get("session_id", chain_id),
        "event_type": event_type,
        "ts": ts,
        "ts_unix": _parse_ts_unix(ts),
        "agent": labels.get("agent", "") or payload.get("agent", ""),
        "surface": d.get("surface", "codex"),
        "action_type": payload.get("action_type", ""),
        "action_target": payload.get("action_target", ""),
        "tool_name": payload.get("tool_name", ""),
        "decision": payload.get("decision", ""),
        "rule_id": payload.get("rule_id", ""),
        "reason": payload.get("reason", ""),
        "escalation": payload.get("escalation", ""),
        "mode": payload.get("mode", ""),
        "cwd": payload.get("cwd", ""),
        "model": payload.get("model_provider", "") or payload.get("model", ""),
        "role": labels.get("role", "") or payload.get("role", ""),
        "fingerprint": d.get("agent_fingerprint", ""),
        "workflow_id": labels.get("workflow_id", ""),
        "envelope_id": payload.get("envelope_id", ""),
        "payload_json": raw.strip(),
    }


# ---------------------------------------------------------------------------
# Claude Code session parser
# ---------------------------------------------------------------------------

def _extract_cc_tool_calls(message: dict) -> list[dict]:
    """Extract tool_use blocks from a Claude Code assistant message."""
    content = message.get("content", [])
    if not isinstance(content, list):
        return []
    calls = []
    for block in content:
        if not isinstance(block, dict):
            continue
        if block.get("type") != "tool_use":
            continue
        calls.append({
            "tool_name": block.get("name", ""),
            "tool_input": block.get("input", {}),
            "tool_id": block.get("id", ""),
        })
    return calls


def parse_claude_code_line(raw: str, file_session_id: str = "") -> list[dict]:
    """Parse a single Claude Code session JSONL line.
    
    Returns 0 or more event dicts. Claude Code sessions don't have the
    same structure as codex events, so we synthesize events from tool_use
    blocks in assistant messages.
    
    Claude Code session format:
    {
      "type": "assistant" | "user" | "result" | ...,
      "timestamp": "2026-05-04T12:18:51.261Z",
      "sessionId": "fdabfbca-...",
      "message": { "role": "assistant", "content": [ { "type": "tool_use", ... } ] }
    }
    """
    try:
        d = json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return []

    event_type = d.get("type", "")
    session_id = d.get("sessionId", file_session_id)
    ts = d.get("timestamp", "")
    ts_unix = _parse_ts_unix(ts)

    results = []

    if event_type == "assistant":
        message = d.get("message", {})
        if not isinstance(message, dict):
            return []
        tool_calls = _extract_cc_tool_calls(message)
        for tc in tool_calls:
            tool_name = tc.get("tool_name", "")
            tool_input = tc.get("tool_input", {})
            # Map Claude Code tool names to chitin action_types
            action_type = _cc_tool_to_action_type(tool_name)
            action_target = _extract_action_target(tool_name, tool_input)
            
            results.append({
                "chain_id": session_id,  # use session_id as chain_id for CC
                "session_id": session_id,
                "event_type": "tool_call",
                "ts": ts,
                "ts_unix": ts_unix,
                "agent": "claude-code",
                "surface": "claude-code",
                "action_type": action_type,
                "action_target": action_target,
                "tool_name": tool_name,
                "decision": "",  # not gated by chitin kernel
                "rule_id": "",
                "reason": "",
                "escalation": "",
                "mode": "",
                "cwd": d.get("cwd", ""),
                "model": message.get("model", ""),
                "role": "assistant",
                "fingerprint": "",
                "workflow_id": "",
                "envelope_id": "",
                "payload_json": raw.strip(),
            })

    elif event_type == "user":
        # Record user messages as session_start if it's the first,
        # otherwise as user_input
        message = d.get("message", {})
        content = ""
        if isinstance(message, dict):
            c = message.get("content", "")
            if isinstance(c, str):
                content = c[:200]  # truncate for storage
            elif isinstance(c, list):
                # Extract text blocks
                parts = []
                for block in c:
                    if isinstance(block, dict) and block.get("type") == "text":
                        parts.append(block.get("text", "")[:100])
                content = " ".join(parts)[:200]
        
        results.append({
            "chain_id": session_id,
            "session_id": session_id,
            "event_type": "user_input",
            "ts": ts,
            "ts_unix": ts_unix,
            "agent": "claude-code",
            "surface": "claude-code",
            "action_type": "",
            "action_target": "",
            "tool_name": "",
            "decision": "",
            "rule_id": "",
            "reason": "",
            "escalation": "",
            "mode": "",
            "cwd": d.get("cwd", ""),
            "model": "",
            "role": "user",
            "fingerprint": "",
            "workflow_id": "",
            "envelope_id": "",
            "payload_json": raw.strip(),
        })

    # Skip queue-operation, attachment, and other metadata types
    return results


def _cc_tool_to_action_type(tool_name: str) -> str:
    """Map Claude Code tool names to chitin action_types."""
    mapping = {
        "Bash": "shell.exec",
        "Edit": "file.write",
        "Read": "file.read",
        "Write": "file.write",
        "Glob": "file.read",
        "Grep": "file.read",
        "WebFetch": "http.request",
        "Task": "delegate.task",
        "TodoWrite": "delegate.task",
        "EnterWorktree": "git.worktree.add",
        "ExitWorktree": "git.worktree.remove",
    }
    return mapping.get(tool_name, f"cc.{tool_name.lower()}")


def _extract_action_target(tool_name: str, tool_input: dict) -> str:
    """Best-effort extraction of the target of a tool call."""
    if not isinstance(tool_input, dict):
        return ""
    if tool_name in ("Bash",):
        return str(tool_input.get("command", ""))[:500]
    if tool_name in ("Edit", "Write", "Read"):
        return str(tool_input.get("file_path", ""))
    if tool_name in ("Glob", "Grep"):
        return str(tool_input.get("pattern", tool_input.get("path", "")))
    return ""


# ---------------------------------------------------------------------------
# Indexing logic
# ---------------------------------------------------------------------------

def index_codex_file(conn: sqlite3.Connection, file_path: Path, offset: int = 0) -> tuple[int, int, int]:
    """Index a codex-events-*.jsonl file from the given byte offset.
    
    Returns (inserted, skipped_duplicates, next_offset).
    """
    if not file_path.exists():
        return 0, 0, offset

    source_key = _source_file_key("codex", file_path)
    inserted = 0
    skipped = 0
    new_offset = offset

    with file_path.open("rb") as f:
        f.seek(offset)
        chunk = f.read()
        # Process only complete lines (terminated by newline)
        if chunk.endswith(b"\n"):
            complete = chunk
            new_offset = offset + len(chunk)
        else:
            nl = chunk.rfind(b"\n")
            if nl < 0:
                return inserted, skipped, offset  # no complete lines yet
            complete = chunk[: nl + 1]
            new_offset = offset + nl + 1

        for raw_line in complete.splitlines():
            raw = raw_line.decode("utf-8", errors="replace").strip()
            if not raw:
                continue

            parsed = parse_codex_line(raw)
            if parsed is None:
                continue

            lh = _line_hash(source_key, raw)
            ts_unix = parsed.get("ts_unix", 0)
            if ts_unix == 0:
                ts_unix = _parse_ts_unix(parsed.get("ts", ""))

            try:
                conn.execute("""
                    INSERT INTO session_events (
                        line_hash, driver_type, chain_id, session_id, event_type,
                        ts, ts_unix, agent, surface, action_type, action_target,
                        tool_name, decision, rule_id, reason, escalation, mode,
                        cwd, model, role, fingerprint, workflow_id, envelope_id,
                        payload_json, source_file, source_line, last_seen_ts
                    ) VALUES (
                        ?, 'codex', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
                    )
                """, (
                    lh, parsed["chain_id"], parsed["session_id"],
                    parsed["event_type"], parsed["ts"], ts_unix,
                    parsed["agent"], parsed["surface"], parsed["action_type"],
                    parsed["action_target"], parsed["tool_name"], parsed["decision"],
                    parsed["rule_id"], parsed["reason"], parsed["escalation"],
                    parsed["mode"], parsed["cwd"], parsed["model"],
                    parsed["role"], parsed["fingerprint"], parsed["workflow_id"],
                    parsed["envelope_id"], parsed["payload_json"],
                    str(file_path), 0, int(time.time()),
                ))
                inserted += 1
            except sqlite3.IntegrityError:
                skipped += 1

    conn.commit()
    return inserted, skipped, new_offset


def index_claude_code_file(conn: sqlite3.Connection, file_path: Path, offset: int = 0) -> tuple[int, int, int]:
    """Index a Claude Code session JSONL file from the given byte offset.
    
    Returns (inserted, skipped_duplicates, next_offset).
    """
    if not file_path.exists():
        return 0, 0, offset

    # Extract session_id from filename (UUID pattern)
    filename = file_path.stem
    file_session_id = filename  # e.g. fdabfbca-c66d-40a3-a2fc-adc5223c9cf8

    source_key = _source_file_key("claude-code", file_path)
    inserted = 0
    skipped = 0
    new_offset = offset

    with file_path.open("rb") as f:
        f.seek(offset)
        chunk = f.read()
        if chunk.endswith(b"\n"):
            complete = chunk
            new_offset = offset + len(chunk)
        else:
            nl = chunk.rfind(b"\n")
            if nl < 0:
                return inserted, skipped, offset
            complete = chunk[: nl + 1]
            new_offset = offset + nl + 1

        for line_num, raw_line in enumerate(complete.splitlines(), 1):
            raw = raw_line.decode("utf-8", errors="replace").strip()
            if not raw:
                continue

            events = parse_claude_code_line(raw, file_session_id)
            for ev_idx, parsed in enumerate(events):
                # Include event index in hash so multi-tool lines get unique hashes
                lh = _line_hash(source_key + f"#{ev_idx}", raw)
                ts_unix = parsed.get("ts_unix", 0)
                if ts_unix == 0:
                    ts_unix = _parse_ts_unix(parsed.get("ts", ""))

                try:
                    conn.execute("""
                        INSERT INTO session_events (
                            line_hash, driver_type, chain_id, session_id, event_type,
                            ts, ts_unix, agent, surface, action_type, action_target,
                            tool_name, decision, rule_id, reason, escalation, mode,
                            cwd, model, role, fingerprint, workflow_id, envelope_id,
                            payload_json, source_file, source_line, last_seen_ts
                        ) VALUES (
                            ?, 'claude-code', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
                        )
                    """, (
                        lh, parsed["chain_id"], parsed["session_id"],
                        parsed["event_type"], parsed["ts"], ts_unix,
                        parsed["agent"], parsed["surface"], parsed["action_type"],
                        parsed["action_target"], parsed["tool_name"], parsed["decision"],
                        parsed["rule_id"], parsed["reason"], parsed["escalation"],
                        parsed["mode"], parsed["cwd"], parsed["model"],
                        parsed["role"], parsed["fingerprint"], parsed["workflow_id"],
                        parsed["envelope_id"], parsed["payload_json"],
                        str(file_path), line_num, int(time.time()),
                    ))
                    inserted += 1
                except sqlite3.IntegrityError:
                    skipped += 1

    conn.commit()
    return inserted, skipped, new_offset


# ---------------------------------------------------------------------------
# Checkpoint management
# ---------------------------------------------------------------------------

def _get_checkpoint(conn: sqlite3.Connection, source_key: str) -> Optional[dict]:
    row = conn.execute(
        "SELECT offset, file_mtime, file_size, inode FROM session_index_checkpoints WHERE source_key = ?",
        (source_key,),
    ).fetchone()
    if row is None:
        return None
    return {
        "offset": row["offset"],
        "file_mtime": row["file_mtime"],
        "file_size": row["file_size"],
        "inode": row["inode"],
    }


def _save_checkpoint(conn: sqlite3.Connection, source_key: str, file_path: str,
                     offset: int, mtime: float, size: int, inode: int) -> None:
    conn.execute("""
        INSERT INTO session_index_checkpoints (source_key, file_path, offset, file_mtime, file_size, inode, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(source_key) DO UPDATE SET
            offset = excluded.offset,
            file_mtime = excluded.file_mtime,
            file_size = excluded.file_size,
            inode = excluded.inode,
            updated_at = excluded.updated_at
    """, (source_key, file_path, offset, mtime, size, inode, time.time()))
    conn.commit()


def _file_fingerprint(path: Path) -> tuple[float, int, int]:
    """Return (mtime, size, inode) for a file, or (0, 0, 0) if missing."""
    try:
        st = path.stat()
        return st.st_mtime, st.st_size, int(st.st_ino)
    except (FileNotFoundError, OSError):
        return 0.0, 0, 0


def _should_reindex(path: Path, checkpoint: Optional[dict]) -> bool:
    """Return True if the file should be reindexed from scratch (truncated/rotated)."""
    if checkpoint is None:
        return True  # never indexed
    mtime, size, inode = _file_fingerprint(path)
    # If file was truncated (smaller than checkpoint) or replaced (different inode), reindex from 0
    if size < checkpoint["offset"]:
        return True
    if inode != 0 and checkpoint["inode"] != 0 and inode != checkpoint["inode"]:
        return True
    return False


# ---------------------------------------------------------------------------
# Discovery
# ---------------------------------------------------------------------------

def discover_codex_files(chitin_dir: Path) -> list[Path]:
    """Find all codex-events-*.jsonl files in the chitin state dir."""
    if not chitin_dir.exists():
        return []
    return sorted(
        f for f in chitin_dir.iterdir()
        if f.is_file() and _CODEX_EVENT_RE.match(f.name)
    )


def discover_chain_events_files(chitin_dir: Path) -> list[Path]:
    """Find all events-*.jsonl files (canonical chain format) in the chitin state dir."""
    if not chitin_dir.exists():
        return []
    return sorted(
        f for f in chitin_dir.iterdir()
        if f.is_file() and _CHAIN_EVENT_RE.match(f.name)
    )


def discover_claude_code_files(claude_dir: Path) -> list[Path]:
    """Find all Claude Code session JSONL files under ~/.claude/projects/ recursively."""
    if not claude_dir.exists():
        return []
    results = []
    for dirpath, _, filenames in os.walk(claude_dir):
        for fname in filenames:
            if _CC_SESSION_RE.match(fname):
                results.append(Path(dirpath) / fname)
    return sorted(results)


# ---------------------------------------------------------------------------
# Bulk indexing
# ---------------------------------------------------------------------------

def bulk_index(conn: sqlite3.Connection, chitin_dir: Path, claude_dir: Path) -> dict:
    """Perform initial bulk indexing of all existing files.
    
    Returns stats dict with counts.
    """
    stats = {
        "codex_files": 0, "codex_inserted": 0, "codex_skipped": 0,
        "chain_files": 0, "chain_inserted": 0, "chain_skipped": 0,
        "claude_code_files": 0, "claude_code_inserted": 0, "claude_code_skipped": 0,
    }

    # Index codex-events files
    for f in discover_codex_files(chitin_dir):
        source_key = _source_file_key("codex", f)
        checkpoint = _get_checkpoint(conn, source_key)
        offset = 0 if _should_reindex(f, checkpoint) else checkpoint["offset"] if checkpoint else 0
        
        ins, skip, new_off = index_codex_file(conn, f, offset)
        stats["codex_files"] += 1
        stats["codex_inserted"] += ins
        stats["codex_skipped"] += skip
        
        mtime, size, inode = _file_fingerprint(f)
        _save_checkpoint(conn, source_key, str(f), new_off, mtime, size, inode)

    # Index canonical events-*.jsonl files (use codex parser since format is compatible)
    for f in discover_chain_events_files(chitin_dir):
        source_key = _source_file_key("codex", f)
        checkpoint = _get_checkpoint(conn, source_key)
        offset = 0 if _should_reindex(f, checkpoint) else checkpoint["offset"] if checkpoint else 0
        
        ins, skip, new_off = index_codex_file(conn, f, offset)
        stats["chain_files"] += 1
        stats["chain_inserted"] += ins
        stats["chain_skipped"] += skip
        
        mtime, size, inode = _file_fingerprint(f)
        _save_checkpoint(conn, source_key, str(f), new_off, mtime, size, inode)

    # Index Claude Code session files
    for f in discover_claude_code_files(claude_dir):
        source_key = _source_file_key("claude-code", f)
        checkpoint = _get_checkpoint(conn, source_key)
        offset = 0 if _should_reindex(f, checkpoint) else checkpoint["offset"] if checkpoint else 0

        ins, skip, new_off = index_claude_code_file(conn, f, offset)
        stats["claude_code_files"] += 1
        stats["claude_code_inserted"] += ins
        stats["claude_code_skipped"] += skip

        mtime, size, inode = _file_fingerprint(f)
        _save_checkpoint(conn, source_key, str(f), new_off, mtime, size, inode)

    return stats


# ---------------------------------------------------------------------------
# Watch daemon
# ---------------------------------------------------------------------------

def watch_loop(conn: sqlite3.Connection, chitin_dir: Path, claude_dir: Path,
               poll_seconds: float = 5.0) -> None:
    """Continuously poll for new/modified files and index new lines.
    
    Handles three edge cases:
    * New files: discovered each tick, no checkpoint → full scan.
    * Appends: checkpoint offset < file size → incremental scan.
    * Truncation/rotation: file size < checkpoint offset → reindex from 0.
    * Deletion: file gone → remove checkpoint entry.
    """
    while True:
        try:
            _poll_once(conn, chitin_dir, claude_dir)
        except Exception as e:
            # Log but don't die — transient errors should self-heal
            import sys
            print(f"[session_indexer] poll error: {e}", file=sys.stderr)
        time.sleep(poll_seconds)


def _poll_once(conn: sqlite3.Connection, chitin_dir: Path, claude_dir: Path) -> None:
    """Single poll cycle: scan all known directories for changes."""
    
    # --- codex-events and events files in chitin_dir ---
    all_chitin = discover_codex_files(chitin_dir) + discover_chain_events_files(chitin_dir)
    seen_keys = set()
    
    for f in all_chitin:
        source_key = _source_file_key("codex", f)
        seen_keys.add(source_key)
        checkpoint = _get_checkpoint(conn, source_key)
        
        if _should_reindex(f, checkpoint):
            offset = 0
        elif checkpoint is not None:
            offset = checkpoint["offset"]
            # Check if file has grown
            mtime, size, _ = _file_fingerprint(f)
            if size <= checkpoint["offset"] and mtime <= checkpoint["file_mtime"]:
                continue  # no new data
        else:
            offset = 0
        
        ins, skip, new_off = index_codex_file(conn, f, offset)
        mtime, size, inode = _file_fingerprint(f)
        _save_checkpoint(conn, source_key, str(f), new_off, mtime, size, inode)

    # --- Claude Code session files ---
    all_cc = discover_claude_code_files(claude_dir)
    
    for f in all_cc:
        source_key = _source_file_key("claude-code", f)
        seen_keys.add(source_key)
        checkpoint = _get_checkpoint(conn, source_key)
        
        if _should_reindex(f, checkpoint):
            offset = 0
        elif checkpoint is not None:
            offset = checkpoint["offset"]
            mtime, size, _ = _file_fingerprint(f)
            if size <= checkpoint["offset"] and mtime <= checkpoint["file_mtime"]:
                continue
        else:
            offset = 0
        
        ins, skip, new_off = index_claude_code_file(conn, f, offset)
        mtime, size, inode = _file_fingerprint(f)
        _save_checkpoint(conn, source_key, str(f), new_off, mtime, size, inode)

    # Clean up checkpoints for deleted files
    all_keys = {row[0] for row in _get_all_checkpoint_keys(conn)}
    stale = all_keys - seen_keys
    for key in stale:
        conn.execute("DELETE FROM session_index_checkpoints WHERE source_key = ?", (key,))
    conn.commit()


def _get_all_checkpoint_keys(conn: sqlite3.Connection) -> list[tuple[str, str]]:
    """Get all (source_key, file_path) pairs from checkpoints."""
    rows = conn.execute("SELECT source_key, file_path FROM session_index_checkpoints").fetchall()
    return [(row["source_key"], row["file_path"]) for row in rows]


# ---------------------------------------------------------------------------
# CLI entry point
# ---------------------------------------------------------------------------

def main() -> None:
    """CLI: run the session indexer as a daemon or one-shot."""
    import argparse
    
    parser = argparse.ArgumentParser(
        description="Index Codex and Claude Code sessions into chain_index.sqlite"
    )
    parser.add_argument("--chitin-dir", default=None,
                        help="Chitin state directory (default: ~/.chitin)")
    parser.add_argument("--claude-dir", default=None,
                        help="Claude Code projects directory (default: ~/.claude/projects)")
    parser.add_argument("--db", default=None,
                        help="Path to chain_index.sqlite (default: <chitin-dir>/chain_index.sqlite)")
    parser.add_argument("--once", action="store_true",
                        help="Run bulk index once and exit (no watching)")
    parser.add_argument("--poll-interval", type=float, default=5.0,
                        help="Polling interval in seconds (default: 5.0)")
    parser.add_argument("--verbose", action="store_true",
                        help="Print progress to stdout")
    args = parser.parse_args()

    if args.chitin_dir:
        chitin_dir = Path(args.chitin_dir)
    else:
        chitin_dir = Path(os.environ.get("CHITIN_HOME", str(Path.home() / ".chitin")))
    
    if args.claude_dir:
        claude_dir = Path(args.claude_dir)
    else:
        claude_dir = Path.home() / ".claude" / "projects"
    
    if args.db:
        db_path = Path(args.db)
    else:
        db_path = chitin_dir / "chain_index.sqlite"

    conn = init_session_db(db_path)
    try:
        if args.once:
            stats = bulk_index(conn, chitin_dir, claude_dir)
            if args.verbose:
                print(f"Bulk index complete: {stats}")
        else:
            if args.verbose:
                print(f"Starting session indexer: chitin_dir={chitin_dir}, claude_dir={claude_dir}")
                print(f"DB: {db_path}, poll interval: {args.poll_interval}s")
            # Initial bulk scan
            stats = bulk_index(conn, chitin_dir, claude_dir)
            if args.verbose:
                print(f"Initial bulk index: {stats}")
            # Then watch
            watch_loop(conn, chitin_dir, claude_dir, poll_seconds=args.poll_interval)
    finally:
        conn.close()


if __name__ == "__main__":
    main()