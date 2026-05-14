"""Read-only kanban poller. Snapshots task lifecycle + comments into the cross-source index.

Sources: `~/.hermes/kanban/boards/<board>/kanban.db` (SQLite, opened ro).

Emits these `kind` values (per Slice 2 spec):
    kanban_ticket_create
    kanban_status_transition
    kanban_comment

Invariants:
    - The poller NEVER writes to the kanban DB. Opens with mode=ro URI.
    - Idempotent on dedup_key = "{board}:{kind}:{event_id_or_hash}".
    - On locked source DB: retry with bounded backoff; never blocks the
      indexer pipeline forever.

Boundaries tested:
    - Kanban DB locked at poll time → retried, then skipped on exhaust.
    - Empty board (no task_events rows) → succeeds with zero inserts.
    - First poll (no watermark) → snapshots full history once, then
      runs incrementally.
"""
from __future__ import annotations

import hashlib
import json
import sqlite3
import time
from pathlib import Path
from typing import Iterable, Optional

from argus.cross_source_db import (
    get_watermark,
    init_cross_source_db,
    set_watermark,
)


_LOCKED_RETRIES = 3
_LOCKED_BACKOFF_S = 0.5


def _open_kanban_ro(db_path: Path) -> sqlite3.Connection:
    """Open a kanban DB in read-only mode. Bare URI, no PRAGMA writes."""
    conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True, timeout=2.0)
    conn.row_factory = sqlite3.Row
    return conn


def _retry_locked(fn, *args, **kwargs):
    """Run fn; retry on `database is locked` up to _LOCKED_RETRIES times."""
    last_err: Optional[Exception] = None
    for attempt in range(_LOCKED_RETRIES + 1):
        try:
            return fn(*args, **kwargs)
        except sqlite3.OperationalError as e:
            if "locked" not in str(e).lower():
                raise
            last_err = e
            if attempt < _LOCKED_RETRIES:
                time.sleep(_LOCKED_BACKOFF_S * (attempt + 1))
    raise last_err  # type: ignore[misc]


def _dedup_key(board: str, kind: str, event_id: object, extra: str = "") -> str:
    """Stable dedup key. event_id is a row id or content hash."""
    raw = f"{board}:{kind}:{event_id}:{extra}".encode()
    return hashlib.sha256(raw).hexdigest()[:24]


def _scan_task_events(conn: sqlite3.Connection, since_ts: int) -> Iterable[dict]:
    """Yield kanban task_events rows with created_at > since_ts."""
    cur = conn.execute(
        """
        SELECT id, task_id, kind, payload, created_at
        FROM task_events
        WHERE created_at > ?
        ORDER BY created_at ASC
        """,
        (since_ts,),
    )
    for row in cur:
        yield dict(row)


def _scan_task_comments(conn: sqlite3.Connection, since_ts: int) -> Iterable[dict]:
    """Yield kanban task_comments rows with created_at > since_ts."""
    cur = conn.execute(
        """
        SELECT id, task_id, author, body, created_at
        FROM task_comments
        WHERE created_at > ?
        ORDER BY created_at ASC
        """,
        (since_ts,),
    )
    for row in cur:
        yield dict(row)


def _normalise_kanban_event(board: str, ev: dict) -> Optional[tuple[str, str, int, str, Optional[str], str, str]]:
    """Map a kanban task_events row to (source, kind, ts_unix, subject, actor, payload_json, dedup_key)."""
    kind_raw = ev.get("kind") or ""
    task_id = ev.get("task_id") or ""
    ts_unix = int(ev.get("created_at") or 0)
    payload = ev.get("payload") or "{}"

    # Map kanban event kinds onto the Argus-canonical names.
    if kind_raw == "status_transition":
        kind = "kanban_status_transition"
    elif kind_raw in ("commented", "comment"):
        kind = "kanban_comment"
    elif kind_raw in ("assigned", "unassigned", "claimed"):
        # Lifecycle events that don't fit a dedicated kind go through a
        # generic kanban_event; cross-source detectors can ignore them.
        kind = "kanban_event"
    else:
        kind = "kanban_event"

    actor: Optional[str] = None
    try:
        meta = json.loads(payload) if isinstance(payload, str) else {}
        actor = meta.get("by") or meta.get("assignee") or meta.get("author")
    except (TypeError, ValueError):
        actor = None

    dedup = _dedup_key(board, kind, ev.get("id"))
    return ("kanban", kind, ts_unix, task_id, actor, payload, dedup)


def _normalise_kanban_comment(board: str, row: dict) -> tuple[str, str, int, str, Optional[str], str, str]:
    """Comment rows have their own table — emit as kanban_comment."""
    ts_unix = int(row.get("created_at") or 0)
    task_id = row.get("task_id") or ""
    author = row.get("author")
    body = row.get("body") or ""
    payload = json.dumps({"author": author, "body_preview": body[:240]})
    dedup = _dedup_key(board, "kanban_comment", row.get("id"), "C")
    return ("kanban", "kanban_comment", ts_unix, task_id, author, payload, dedup)


def ingest_board(
    kanban_db: Path,
    xs_conn: sqlite3.Connection,
    board: Optional[str] = None,
) -> tuple[int, int]:
    """Pull new kanban rows from `kanban_db` into the Argus cross-source index.

    Returns (inserted, skipped_duplicates).
    """
    board = board or kanban_db.parent.name
    scope = board
    watermark = get_watermark(xs_conn, "kanban", scope)

    if not kanban_db.exists():
        return 0, 0

    src = _retry_locked(_open_kanban_ro, kanban_db)
    try:
        events = list(_retry_locked(lambda: list(_scan_task_events(src, watermark))))
        comments = list(_retry_locked(lambda: list(_scan_task_comments(src, watermark))))
    finally:
        src.close()

    inserted = 0
    skipped = 0
    max_ts = watermark

    cursor = xs_conn.cursor()
    for ev in events:
        norm = _normalise_kanban_event(board, ev)
        if norm is None:
            continue
        source, kind, ts_unix, subject, actor, payload_json, dedup = norm
        if ts_unix > max_ts:
            max_ts = ts_unix
        try:
            cursor.execute(
                """
                INSERT INTO cross_source_events
                  (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (source, kind, ts_unix, subject, actor, payload_json, dedup),
            )
            inserted += 1
        except sqlite3.IntegrityError:
            skipped += 1

    for row in comments:
        source, kind, ts_unix, subject, actor, payload_json, dedup = _normalise_kanban_comment(board, row)
        if ts_unix > max_ts:
            max_ts = ts_unix
        try:
            cursor.execute(
                """
                INSERT INTO cross_source_events
                  (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (source, kind, ts_unix, subject, actor, payload_json, dedup),
            )
            inserted += 1
        except sqlite3.IntegrityError:
            skipped += 1

    xs_conn.commit()
    if max_ts > watermark:
        set_watermark(xs_conn, "kanban", scope, max_ts)

    return inserted, skipped


def discover_kanban_dbs(boards_root: Path) -> list[Path]:
    """Return all kanban.db files under `~/.hermes/kanban/boards/`."""
    if not boards_root.exists():
        return []
    return sorted(boards_root.glob("*/kanban.db"))


def ingest_all_kanban(boards_root: Path, xs_db: Path) -> dict:
    """Top-level entrypoint: ingest every discovered kanban.db once.

    Returns {"boards": [...], "inserted": N, "skipped": M}.
    """
    xs_conn = init_cross_source_db(xs_db)
    total_inserted = 0
    total_skipped = 0
    boards_seen: list[str] = []
    try:
        for db_path in discover_kanban_dbs(boards_root):
            board = db_path.parent.name
            try:
                inserted, skipped = ingest_board(db_path, xs_conn, board=board)
                total_inserted += inserted
                total_skipped += skipped
                boards_seen.append(board)
            except sqlite3.OperationalError:
                # Locked beyond retries, or any other operational glitch:
                # surface in the return value, don't crash the indexer.
                boards_seen.append(f"{board}:LOCKED")
    finally:
        xs_conn.close()
    return {
        "boards": boards_seen,
        "inserted": total_inserted,
        "skipped": total_skipped,
    }
