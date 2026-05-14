"""Read-only log ingester for hermes + openclaw logs.

Tails `~/.hermes/logs/*.log` and `~/.openclaw/logs/*.log`, parses each
line with a structured-prefix regex, and emits canonical events into
the cross-source index.

Invariants:
    - Strictly read-only. We never write the source log files.
    - Idempotent on dedup_key = "{source}:{filename}:{line_offset}".
    - Inode-rotation tolerant: when a tracked file's inode changes
      (logrotate, truncation, file replaced), we close the old handle
      and reopen the new one at offset 0 on the next poll.
    - Truncated last line: only commit a line when it ends in '\\n'.
    - Bad input never aborts (I5): parse failures land in an
      `unparsed` bucket counter; pipeline keeps going.

Boundaries tested:
    - Log rotation mid-tail (inode change → reopen).
    - Truncated last line (no \\n → defer until newline arrives).
    - Empty log file → succeeds with zero rows.
    - Missing source directory → returns zero, no crash.
"""
from __future__ import annotations

import hashlib
import json
import re
import sqlite3
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterable, Optional

from argus.cross_source_db import (
    get_watermark,
    init_cross_source_db,
    set_watermark,
)


# Python-style logger prefix: "YYYY-MM-DD HH:MM:SS[,ms] LEVEL logger: msg"
# Used by hermes (`agent.log`, `gateway.log`, ...).
_PY_LOG_RE = re.compile(
    r"^(?P<ts>\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:[,.]\d+)?)\s+"
    r"(?P<level>[A-Z]+)\s+"
    r"(?P<logger>[A-Za-z0-9_.\-]+)\s*:\s*"
    r"(?P<msg>.*)$"
)

# Bare-prefix log: "YYYY-MM-DD HH:MM:SS msg"  (openclaw-style).
_BARE_LOG_RE = re.compile(
    r"^(?P<ts>\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s+(?P<msg>.*)$"
)


def _parse_local_ts(ts: str) -> Optional[int]:
    """Parse a local-time ts like '2026-05-13 21:13:13[,078]' into unix seconds."""
    from datetime import datetime
    s = ts.replace(",", ".")
    try:
        dt = datetime.fromisoformat(s)
        return int(dt.timestamp())
    except (ValueError, TypeError):
        return None


@dataclass(frozen=True)
class ParsedLine:
    ts_unix: int
    level: Optional[str]
    logger: Optional[str]
    msg: str


def parse_log_line(line: str) -> Optional[ParsedLine]:
    """Match a line against known prefix formats. Returns None on no match."""
    line = line.rstrip("\n").rstrip("\r")
    if not line:
        return None
    m = _PY_LOG_RE.match(line)
    if m:
        ts_unix = _parse_local_ts(m.group("ts"))
        if ts_unix is None:
            return None
        return ParsedLine(ts_unix=ts_unix, level=m.group("level"),
                          logger=m.group("logger"), msg=m.group("msg"))
    m = _BARE_LOG_RE.match(line)
    if m:
        ts_unix = _parse_local_ts(m.group("ts"))
        if ts_unix is None:
            return None
        return ParsedLine(ts_unix=ts_unix, level=None, logger=None,
                          msg=m.group("msg"))
    return None


def _dedup_key(source: str, filename: str, line: str) -> str:
    """Dedup by line content: the same parsed line in the same file/source
    is the same event regardless of rotation. New content at offset 0 of a
    rotated file does NOT collide with an earlier line at offset 0.
    """
    raw = f"{source}:{filename}:{line}".encode()
    return hashlib.sha256(raw).hexdigest()[:24]


def _classify_kind(source: str, parsed: ParsedLine) -> str:
    """Map a parsed log line to a canonical event kind.

    Heuristic, regex-driven. The qwen-based extractor (Slice 3 §
    pattern-matched events) is deferred to a follow-up — the rule
    table here covers the operator-facing events that matter today
    and avoids putting an LLM call on every line of the log.
    """
    msg = parsed.msg.lower()
    if source == "hermes":
        if "standup" in msg:
            return "hermes_standup"
        if parsed.level == "ERROR" or "error" in msg:
            return "hermes_error"
        if parsed.logger and "discord" in parsed.logger:
            return "hermes_discord"
        return "hermes_log"
    if source == "openclaw":
        if "dispatch" in msg and ("failed" in msg or "error" in msg):
            return "openclaw_dispatch_fail"
        if "dispatch" in msg or "spawn_worker" in msg:
            return "openclaw_dispatch"
        if "tick" in msg and "queue" in msg:
            return "openclaw_tick"
        return "openclaw_log"
    return "log_line"


@dataclass
class TailState:
    """Per-file tail state — survives across polls in the same process."""
    inode: int
    offset: int
    pending: str = ""  # buffer for partial final line


def _file_inode(path: Path) -> Optional[int]:
    try:
        return path.stat().st_ino
    except (FileNotFoundError, PermissionError, OSError):
        return None


def _read_new_lines(path: Path, state: TailState) -> tuple[list[tuple[int, str]], int]:
    """Read new complete lines from `path` starting at `state.offset`.

    Returns (list of (line_offset, line_text), new_offset). Partial
    final lines are buffered in state.pending and returned on the
    next poll once a '\\n' arrives.
    """
    try:
        with path.open("r", errors="replace") as f:
            f.seek(state.offset)
            chunk = f.read()
            end_offset = f.tell()
    except FileNotFoundError:
        return [], state.offset

    buf = state.pending + chunk
    lines = buf.split("\n")
    # Last element is the partial line (or empty if chunk ended in '\\n').
    state.pending = lines[-1]
    complete = lines[:-1]

    out: list[tuple[int, str]] = []
    # We compute line offsets relative to the start of the file. We can
    # only approximate (chunk-relative) — for dedup we just need stable
    # uniqueness per (file, line). Use the cumulative byte offset.
    cursor = state.offset - len(state.pending) - sum(len(l) + 1 for l in complete)
    if cursor < 0:
        cursor = state.offset
    cum = cursor
    for line in complete:
        out.append((cum, line))
        cum += len(line) + 1

    return out, end_offset


def ingest_log_file(
    path: Path,
    source: str,
    xs_conn: sqlite3.Connection,
    state: Optional[TailState] = None,
) -> tuple[int, int, int, TailState]:
    """Ingest new lines from `path` into the cross-source index.

    Returns (inserted, skipped, unparsed, new_state).
    """
    inode = _file_inode(path)
    if inode is None:
        # File is gone. Reset state so a re-created file is treated as fresh.
        return 0, 0, 0, TailState(inode=0, offset=0, pending="")

    # Detect rotation: inode change OR file shorter than recorded offset.
    if state is None or state.inode != inode or _file_size(path) < state.offset:
        state = TailState(inode=inode, offset=0, pending="")

    lines, new_offset = _read_new_lines(path, state)
    inserted = 0
    skipped = 0
    unparsed = 0
    filename = path.name

    for _line_offset, line in lines:
        parsed = parse_log_line(line)
        if parsed is None:
            if line.strip():
                unparsed += 1
            continue
        kind = _classify_kind(source, parsed)
        dedup = _dedup_key(source, filename, line)
        payload = json.dumps({
            "file": filename,
            "level": parsed.level,
            "logger": parsed.logger,
            "msg_preview": parsed.msg[:240],
        })
        try:
            xs_conn.execute(
                """
                INSERT INTO cross_source_events
                  (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (source, kind, parsed.ts_unix, filename, parsed.logger, payload, dedup),
            )
            inserted += 1
        except sqlite3.IntegrityError:
            skipped += 1
    xs_conn.commit()

    state.inode = inode
    state.offset = new_offset
    return inserted, skipped, unparsed, state


def _file_size(path: Path) -> int:
    try:
        return path.stat().st_size
    except (FileNotFoundError, OSError):
        return 0


def discover_logs(log_root: Path) -> list[Path]:
    """Return all *.log files directly under log_root (non-recursive)."""
    if not log_root.exists():
        return []
    return sorted(p for p in log_root.glob("*.log") if p.is_file())


def ingest_logs(
    log_root: Path,
    source: str,
    xs_db: Path,
    state_by_path: Optional[dict[Path, TailState]] = None,
) -> tuple[dict, dict[Path, TailState]]:
    """Top-level entrypoint: ingest every *.log under log_root once.

    Returns (summary_dict, updated_state_by_path).
    """
    state_by_path = state_by_path or {}
    xs_conn = init_cross_source_db(xs_db)
    inserted = 0
    skipped = 0
    unparsed = 0
    files: list[str] = []
    try:
        for path in discover_logs(log_root):
            files.append(path.name)
            i, s, u, new_state = ingest_log_file(
                path, source, xs_conn, state=state_by_path.get(path)
            )
            inserted += i
            skipped += s
            unparsed += u
            state_by_path[path] = new_state
    finally:
        xs_conn.close()
    summary = {
        "source": source,
        "files": files,
        "inserted": inserted,
        "skipped": skipped,
        "unparsed": unparsed,
    }
    return summary, state_by_path
