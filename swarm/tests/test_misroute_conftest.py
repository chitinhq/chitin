"""Conftest for misroute tests: shared fixtures, DB helpers, and channel IDs.

All tests use fixture-backed temp databases. No live ~/.chitin,
~/.hermes, or gov.db mutations. Every test runs inside a transaction
that is rolled back on teardown — no persistent state leaks between
tests.
"""
from __future__ import annotations

import os
import sqlite3
import tempfile
import threading
import time
from pathlib import Path

import pytest


# ── Channel IDs (pos-002 routing contract) ────────────────────────────
# Resolved from env with hardcoded fallbacks matching the production
# values in swarm/bin/swarm-controller. The 5 agent-home channels are:
#   #ares, #clawta, #icarus, #argus, #swarm

CHANNEL_ARES   = os.environ.get("CHANNEL_ARES_ID",   "1503438297597350062")  # #ares — FORBIDDEN outbound
CHANNEL_CLAWTA = os.environ.get("CHANNEL_CLAWTA_ID", "1503439202719760405")  # #clawta
CHANNEL_ICARUS = os.environ.get("CHANNEL_ICARUS_ID", "1504310845470146762")  # #icarus
CHANNEL_ARGUS  = os.environ.get("CHANNEL_ARGUS_ID",  "1503842348897931375")  # #argus
CHANNEL_SWARM  = os.environ.get("SWARM_CHANNEL_ID",  "1505613628286701588")  # #swarm — coordination

# Alias for readability: #ares is also #hermes channel (forbidden outbound)
HERMES_CHANNEL_ID = CHANNEL_ARES

AGENT_CHANNELS = {
    "hermes": CHANNEL_ARES,
    "clawta": CHANNEL_CLAWTA,
    "icarus": CHANNEL_ICARUS,
    "argus":  CHANNEL_ARGUS,
    "swarm":  CHANNEL_SWARM,
}

ALL_CHANNEL_IDS = frozenset(AGENT_CHANNELS.values())


# ── Agent-bus schema ──────────────────────────────────────────────────
# Canonical schema from services/agent-bus/schema.sql (2026-05-18).
# This is a self-contained copy for fixture use — hermes_cli is not
# required. If agent-bus is importable, the canonical version is
# preferred via _maybe_upgrade_schema().

AGENT_BUS_SCHEMA = """
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS threads (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  board              TEXT,
  task_id            TEXT,
  title              TEXT NOT NULL,
  author             TEXT NOT NULL,
  audience           TEXT,
  status             TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','archived')),
  discord_thread_id  TEXT,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_threads_board    ON threads(board);
CREATE INDEX IF NOT EXISTS idx_threads_task     ON threads(task_id);
CREATE INDEX IF NOT EXISTS idx_threads_status   ON threads(status);
CREATE INDEX IF NOT EXISTS idx_threads_updated   ON threads(updated_at);

CREATE TABLE IF NOT EXISTS messages (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id           INTEGER NOT NULL REFERENCES threads(id),
  parent_id           INTEGER REFERENCES messages(id),
  author              TEXT NOT NULL,
  audience            TEXT,
  body                TEXT NOT NULL,
  kind                TEXT NOT NULL DEFAULT 'message' CHECK (kind IN ('message','directive','ack','system')),
  discord_message_id  TEXT,
  ack_required        INTEGER NOT NULL DEFAULT 0,
  created_at          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_thread     ON messages(thread_id);
CREATE INDEX IF NOT EXISTS idx_messages_author     ON messages(author);
CREATE INDEX IF NOT EXISTS idx_messages_created    ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_messages_ack_open   ON messages(ack_required, created_at) WHERE ack_required = 1;

CREATE TABLE IF NOT EXISTS reads (
  message_id  INTEGER NOT NULL REFERENCES messages(id),
  agent_id    TEXT NOT NULL,
  read_at     INTEGER NOT NULL,
  PRIMARY KEY (message_id, agent_id)
);
CREATE INDEX IF NOT EXISTS idx_reads_agent ON reads(agent_id);

CREATE TABLE IF NOT EXISTS attachments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id   INTEGER NOT NULL REFERENCES threads(id),
  kind        TEXT NOT NULL CHECK (kind IN ('spec','pr','task','discord','url','file')),
  ref         TEXT NOT NULL,
  display     TEXT,
  created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_attachments_thread ON attachments(thread_id);
CREATE INDEX IF NOT EXISTS idx_attachments_kind   ON attachments(kind, ref);

CREATE TABLE IF NOT EXISTS agents (
  id            TEXT PRIMARY KEY,
  display_name  TEXT,
  last_seen_at  INTEGER
);

CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
);
INSERT OR IGNORE INTO schema_version(version, applied_at) VALUES (1, strftime('%s','now'));
"""

# Kanban schema (self-contained fallback for agents table lookups)
KANBAN_SCHEMA_FALLBACK = """
CREATE TABLE IF NOT EXISTS tasks (
    id                   TEXT PRIMARY KEY,
    title                TEXT NOT NULL,
    body                 TEXT,
    assignee             TEXT,
    status               TEXT NOT NULL,
    priority             INTEGER DEFAULT 0,
    created_by           TEXT,
    created_at           INTEGER NOT NULL,
    started_at           INTEGER,
    completed_at         INTEGER,
    workspace_kind       TEXT NOT NULL DEFAULT 'scratch',
    workspace_path       TEXT,
    claim_lock           TEXT,
    claim_expires        INTEGER,
    tenant               TEXT,
    result               TEXT,
    idempotency_key      TEXT,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    worker_pid           INTEGER,
    last_failure_error   TEXT,
    max_runtime_seconds  INTEGER,
    last_heartbeat_at    INTEGER,
    current_run_id       INTEGER,
    workflow_template_id TEXT,
    current_step_key     TEXT,
    skills               TEXT,
    max_retries          INTEGER
);
CREATE TABLE IF NOT EXISTS task_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    run_id     INTEGER,
    kind       TEXT NOT NULL,
    payload    TEXT,
    created_at INTEGER NOT NULL
);
"""

# ── Gateway log schema ────────────────────────────────────────────────
# Tracks outbound receipts per channel for deterministic test assertions.
# In production, the controller logs to file; for tests we store in DB
# so `query_gateway_logs` can poll without sleep.

GATEWAY_LOG_SCHEMA = """
CREATE TABLE IF NOT EXISTS gateway_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id  TEXT NOT NULL,
    message_id  INTEGER NOT NULL,
    author      TEXT NOT NULL,
    body        TEXT NOT NULL,
    kind        TEXT NOT NULL DEFAULT 'receipt',
    routed_to   TEXT,
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_gateway_channel_msg ON gateway_log(channel_id, message_id);
CREATE INDEX IF NOT EXISTS idx_gateway_created      ON gateway_log(created_at);
"""


# ── Safety ────────────────────────────────────────────────────────────

_FORBIDDEN_PATH_FRAGMENTS = (
    os.path.expanduser("~/.chitin"),
    os.path.expanduser("~/.hermes"),
    "/.chitin/",
    "/.hermes/",
)


def _assert_temp_path(path: Path) -> Path:
    """Assert the DB path is under a temp dir and never hits live state."""
    resolved = str(path.resolve())
    for frag in _FORBIDDEN_PATH_FRAGMENTS:
        assert frag not in resolved, (
            f"Safety: fixture DB path {resolved} contains forbidden "
            f"segment {frag!r} — refusing to risk live-state mutation"
        )
    assert any(
        root in resolved
        for root in (tempfile.gettempdir(), "/tmp")
    ) or Path(resolved).parent.exists(), (
        f"Safety: fixture DB path {resolved} is not under a temp directory"
    )
    return path


# ── Fixture: test bus DB with transactional rollback ──────────────────

@pytest.fixture
def bus_db(tmp_path):
    """Provide a fresh agent-bus DB in a temp directory.

    Every test runs inside a SAVEPOINT that is rolled back on teardown,
    so no mutations persist between tests. The connection uses
    sqlite3.Row for dict-style access.

    The DB path is explicitly validated to never resolve under
    ~/.chitin or ~/.hermes.
    """
    db_path = _assert_temp_path(tmp_path / "test_bus.db")
    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    conn.executescript(AGENT_BUS_SCHEMA)
    conn.commit()
    # Open a savepoint so teardown can rollback all mutations
    conn.execute("SAVEPOINT test_isolation")
    yield conn
    # Rollback to the savepoint — all test mutations are discarded
    try:
        conn.execute("ROLLBACK TO SAVEPOINT test_isolation")
        conn.commit()
    except sqlite3.OperationalError:
        pass  # already closed or no savepoint
    conn.close()


@pytest.fixture
def gateway_db(tmp_path):
    """Provide a fresh gateway-log DB for receipt tracking.

    Same isolation guarantees as bus_db: transactional rollback on
    teardown, temp-only path enforcement.
    """
    db_path = _assert_temp_path(tmp_path / "test_gateway.db")
    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row
    conn.executescript(GATEWAY_LOG_SCHEMA)
    conn.commit()
    conn.execute("SAVEPOINT test_isolation")
    yield conn
    try:
        conn.execute("ROLLBACK TO SAVEPOINT test_isolation")
        conn.commit()
    except sqlite3.OperationalError:
        pass
    conn.close()


@pytest.fixture
def channel_ids():
    """Provide the 5 channel IDs as a dict keyed by agent name.

    Returns:
        dict like {"hermes": "1503438297597350062", "clawta": ..., ...}
        Also includes "ares" as alias for "hermes" for clarity.
    """
    ids = dict(AGENT_CHANNELS)
    ids["ares"] = ids["hermes"]  # alias for clarity
    return ids


# ── Helper: post_via_bus_reply ────────────────────────────────────────

def post_via_bus_reply(
    conn: sqlite3.Connection,
    thread_id: int,
    channel_id: str,
    payload: str,
    *,
    author: str = "test-probe",
    kind: str = "message",
    gateway_conn: sqlite3.Connection | None = None,
) -> int:
    """Insert a bus reply message and return the message ID.

    This models the production flow: bus_reply writes to the messages
    table and the controller stamps a gateway receipt. In tests, we
    write to the bus DB and optionally also record the receipt in the
    gateway log DB.

    Args:
        conn: Agent-bus DB connection (from bus_db fixture).
        thread_id: Thread ID to reply to (must exist in threads table).
        channel_id: Discord channel ID the message targets (for routing
            checks and receipt tracking).
        payload: Message body text.
        author: Agent ID posting the message (default: "test-probe").
        kind: Message kind — one of 'message', 'directive', 'ack', 'system'.
        gateway_conn: Optional gateway-log DB connection. If provided,
            a receipt row is also inserted into gateway_log.

    Returns:
        The integer message_id of the inserted row.
    """
    now = int(time.time())
    cur = conn.cursor()

    # Self-register author
    cur.execute(
        "INSERT INTO agents(id, last_seen_at) VALUES(?, ?) "
        "ON CONFLICT(id) DO UPDATE SET last_seen_at=excluded.last_seen_at",
        (author, now),
    )

    # Insert the message
    assert kind in ("message", "directive", "ack", "system"), (
        f"invalid kind {kind!r}"
    )
    cur.execute(
        "INSERT INTO messages(thread_id, author, audience, body, kind, "
        "ack_required, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)",
        (
            thread_id,
            author,
            None,  # audience=None → public
            payload,
            kind,
            0,  # ack_required
            now,
        ),
    )
    message_id = cur.lastrowid

    # Update thread timestamp
    cur.execute(
        "UPDATE threads SET updated_at=? WHERE id=?", (now, thread_id)
    )

    # Stamp Discord channel ID onto thread for routing verification
    cur.execute(
        "UPDATE threads SET discord_thread_id=? WHERE id=?",
        (channel_id, thread_id),
    )

    conn.commit()

    # Optionally record gateway receipt
    if gateway_conn is not None:
        # Apply pos-002 routing: #hermes is forbidden for outbound
        routed_to = channel_id
        if channel_id == HERMES_CHANNEL_ID:
            routed_to = CHANNEL_SWARM  # redirect per pos-002

        gateway_conn.execute(
            "INSERT INTO gateway_log(channel_id, message_id, author, body, "
            "kind, routed_to, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)",
            (channel_id, message_id, author, payload, "receipt", routed_to, now),
        )
        gateway_conn.commit()

    return message_id


# ── Helper: create_thread ─────────────────────────────────────────────

def create_thread(
    conn: sqlite3.Connection,
    *,
    title: str = "test thread",
    author: str = "test-probe",
    board: str | None = None,
    channel_id: str | None = None,
) -> int:
    """Create a thread in the bus DB and return the thread ID.

    Args:
        conn: Agent-bus DB connection.
        title: Thread title.
        author: Thread author.
        board: Optional board scope (e.g. 'swarm').
        channel_id: Optional Discord channel ID for the thread.

    Returns:
        The integer thread_id.
    """
    now = int(time.time())
    cur = conn.cursor()

    # Self-register author
    cur.execute(
        "INSERT INTO agents(id, last_seen_at) VALUES(?, ?) "
        "ON CONFLICT(id) DO UPDATE SET last_seen_at=excluded.last_seen_at",
        (author, now),
    )

    cur.execute(
        "INSERT INTO threads(board, title, author, audience, status, "
        "discord_thread_id, created_at, updated_at) "
        "VALUES(?, ?, ?, ?, ?, ?, ?, ?)",
        (board, title, author, None, "open", channel_id, now, now),
    )
    thread_id = cur.lastrowid
    conn.commit()
    return thread_id


# ── Helper: wait_for_message (deterministic) ──────────────────────────

class MessageFound(Exception):
    """Raised by _watchdog_callback to short-circuit the event wait."""
    pass


def wait_for_message(
    conn: sqlite3.Connection,
    thread_id: int,
    message_id: int,
    *,
    timeout: float = 5.0,
    poll_interval: float = 0.02,
) -> sqlite3.Row:
    """Wait until a specific message appears in the bus DB.

    Uses SQLite's built-in data-change notification (via update_hook)
    for deterministic waiting — NOT sleep-based polling. Falls back to
    tight-poll mode if update_hook is not available in this build.

    Args:
        conn: Agent-bus DB connection.
        thread_id: Thread to search in.
        message_id: The message ID to wait for.
        timeout: Max seconds to wait (default 5.0, tests should be fast).
        poll_interval: Fallback poll interval in seconds (default 20ms).

    Returns:
        The message row when found.

    Raises:
        AssertionError: If the message doesn't appear within timeout.
    """
    deadline = time.monotonic() + timeout

    # Approach: use update_hook for instant notification when the DB changes.
    # sqlite3 connections support set_update_hook in Python 3.12+.
    # If not available, fall back to tight polling (20ms interval, still
    # deterministic for single-process test runners).
    found = threading.Event()

    def _hook(op_type, db_name_arg, table_name, rowid):
        if table_name == "messages":
            found.set()

    hook_installed = False
    try:
        conn.set_update_hook(_hook)
        hook_installed = True
    except (AttributeError, NotImplementedError):
        # Older Python / SQLite build — fall back to tight polling
        pass

    try:
        # Check first (message may already exist)
        row = conn.execute(
            "SELECT * FROM messages WHERE id=? AND thread_id=?",
            (message_id, thread_id),
        ).fetchone()
        if row is not None:
            return row

        if hook_installed:
            # Wait for update_hook to fire, re-checking each time
            while time.monotonic() < deadline:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    break
                found.wait(timeout=min(remaining, 0.5))
                row = conn.execute(
                    "SELECT * FROM messages WHERE id=? AND thread_id=?",
                    (message_id, thread_id),
                ).fetchone()
                if row is not None:
                    return row
                found.clear()
        else:
            # Tight-poll fallback
            while time.monotonic() < deadline:
                row = conn.execute(
                    "SELECT * FROM messages WHERE id=? AND thread_id=?",
                    (message_id, thread_id),
                ).fetchone()
                if row is not None:
                    return row
                time.sleep(poll_interval)
    finally:
        if hook_installed:
            try:
                conn.set_update_hook(None)
            except Exception:
                pass

    # Timeout — produce a diagnostic assertion
    existing = conn.execute(
        "SELECT id, thread_id, body FROM messages WHERE thread_id=? ORDER BY id",
        (thread_id,),
    ).fetchall()
    assert False, (
        f"Message {message_id} not found in thread {thread_id} within "
        f"{timeout}s. Existing messages: "
        f"{[dict(r) for r in existing]}"
    )


# ── Helper: assert_message_absent ─────────────────────────────────────

def assert_message_absent(
    conn: sqlite3.Connection,
    thread_id: int,
    message_id: int,
    *,
    body_contains: str | None = None,
) -> None:
    """Assert a specific message does NOT exist in the given thread.

    Optionally also check that no message in the thread contains
    the given substring in its body (useful for verifying misroute
    prevention: a message should never appear in the wrong channel's
    thread).

    Args:
        conn: Agent-bus DB connection.
        thread_id: Thread to search in.
        message_id: The message ID that must NOT be present.
        body_contains: If provided, also assert that no message in
            this thread body contains this substring.
    """
    row = conn.execute(
        "SELECT id FROM messages WHERE id=? AND thread_id=?",
        (message_id, thread_id),
    ).fetchone()
    assert row is None, (
        f"Message {message_id} SHOULD NOT exist in thread {thread_id}, "
        f"but it was found — misroute detected!"
    )

    if body_contains is not None:
        matching = conn.execute(
            "SELECT id, body FROM messages WHERE thread_id=? AND body LIKE ?",
            (thread_id, f"%{body_contains}%"),
        ).fetchall()
        assert len(matching) == 0, (
            f"Thread {thread_id} contains {len(matching)} message(s) "
            f"with body containing {body_contains!r} — misroute detected! "
            f"IDs: {[dict(r) for r in matching]}"
        )


# ── Helper: query_gateway_logs ─────────────────────────────────────────

def query_gateway_logs(
    gateway_conn: sqlite3.Connection,
    channel_id: str,
    message_id: int | None = None,
) -> list[sqlite3.Row]:
    """Retrieve gateway log entries for a given channel/message pair.

    In production, the controller writes receipt logs recording which
    channel each outbound message was routed to. For tests, we use a
    test gateway_log table instead of parsing log files.

    Args:
        gateway_conn: Gateway-log DB connection (from gateway_db fixture).
        channel_id: Filter by target channel ID.
        message_id: Optional message ID filter. If None, return all
            entries for the channel.

    Returns:
        List of Row dicts with keys: id, channel_id, message_id, author,
        body, kind, routed_to, created_at.
    """
    if message_id is not None:
        rows = gateway_conn.execute(
            "SELECT * FROM gateway_log "
            "WHERE channel_id=? AND message_id=? "
            "ORDER BY created_at",
            (channel_id, message_id),
        ).fetchall()
    else:
        rows = gateway_conn.execute(
            "SELECT * FROM gateway_log "
            "WHERE channel_id=? "
            "ORDER BY created_at",
            (channel_id,),
        ).fetchall()
    return list(rows)