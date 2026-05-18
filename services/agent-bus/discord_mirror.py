#!/usr/bin/env python3
"""agent-bus ↔ Discord bidirectional mirror.

Two directions:

  outbound: post a bus message to a Discord webhook URL. No bot token
            required; the URL itself authenticates. Operator provides the
            URL per-channel via env (or via a bus_mirror_link tool later).

  inbound:  poll a Discord channel and ingest new messages into a bus
            thread. Requires a bot token + the channel id.

Designed for cron-style invocation:
  discord_mirror.py poll  — one-shot ingest of new messages
  discord_mirror.py push <thread_id> [--after msg_id]
                          — post bus thread messages to Discord

Persistence:
  Last-seen Discord message id per channel lives in the `discord_cursors`
  table (created on first run if absent — keeps Phase 4 schema-additive).

Env:
  DISCORD_WEBHOOK_URL          # outbound (per-channel)
  DISCORD_BOT_TOKEN            # inbound
  DISCORD_MIRROR_CHANNEL_ID    # inbound channel id
  DISCORD_MIRROR_THREAD_TITLE  # bus thread title for inbound (default: "Discord mirror — <channel>")
  AGENT_BUS_DB                 # override DB path (test hook)

All HTTP via stdlib urllib so the service stays zero-dep.
"""
from __future__ import annotations

import argparse
import json
import os
import sqlite3
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

DEFAULT_DB = Path.home() / ".chitin" / "agent-bus" / "bus.db"
DISCORD_API = "https://discord.com/api/v10"


def db_path() -> Path:
    return Path(os.environ.get("AGENT_BUS_DB") or str(DEFAULT_DB))


def ensure_cursor_table(conn: sqlite3.Connection) -> None:
    """Additive migration. Runs every connect — cheap if already present."""
    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS discord_cursors (
          channel_id     TEXT PRIMARY KEY,
          last_message_id TEXT NOT NULL,
          updated_at     INTEGER NOT NULL
        )
        """
    )
    conn.commit()


def get_cursor(conn: sqlite3.Connection, channel_id: str) -> str | None:
    row = conn.execute(
        "SELECT last_message_id FROM discord_cursors WHERE channel_id=?",
        (channel_id,),
    ).fetchone()
    return row[0] if row else None


def set_cursor(conn: sqlite3.Connection, channel_id: str, message_id: str) -> None:
    conn.execute(
        "INSERT INTO discord_cursors(channel_id, last_message_id, updated_at) "
        "VALUES(?,?,?) "
        "ON CONFLICT(channel_id) DO UPDATE SET "
        "  last_message_id=excluded.last_message_id, "
        "  updated_at=excluded.updated_at",
        (channel_id, message_id, int(time.time())),
    )
    conn.commit()


# ---------------------------------------------------------------------------
# HTTP — small wrapper so tests can patch one symbol.
# ---------------------------------------------------------------------------

def http_request(url: str, *, method: str = "GET", headers: dict | None = None,
                 body: bytes | None = None, timeout: float = 10.0) -> tuple[int, bytes]:
    """Single chokepoint for all network IO; mockable in tests."""
    req = urllib.request.Request(url, data=body, method=method,
                                 headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


# ---------------------------------------------------------------------------
# Outbound: post to a Discord webhook URL.
# ---------------------------------------------------------------------------

def post_to_discord_webhook(webhook_url: str, *, content: str,
                            username: str | None = None) -> dict:
    """POST to a Discord incoming webhook. Discord caps content at 2000 chars;
    split politely if exceeded.
    """
    if len(content) > 2000:
        content = content[:1990] + "\n(…continued)"
    payload: dict = {"content": content}
    if username:
        payload["username"] = username
    body = json.dumps(payload).encode("utf-8")
    status, resp = http_request(
        webhook_url, method="POST",
        headers={"Content-Type": "application/json"},
        body=body,
    )
    if status >= 400:
        raise RuntimeError(f"discord webhook POST failed: {status} {resp[:200]!r}")
    return {"status": status, "bytes": len(body)}


# ---------------------------------------------------------------------------
# Inbound: poll a Discord channel via bot token, ingest new messages.
# ---------------------------------------------------------------------------

def fetch_new_messages(channel_id: str, *, token: str,
                       after: str | None = None, limit: int = 50) -> list[dict]:
    """GET /channels/{id}/messages?after={cursor}. Discord returns newest
    first; we flip to chronological so the bus stores them in order.
    """
    qs = f"limit={limit}"
    if after:
        qs += f"&after={after}"
    url = f"{DISCORD_API}/channels/{channel_id}/messages?{qs}"
    status, resp = http_request(
        url, headers={"Authorization": f"Bot {token}",
                      "User-Agent": "agent-bus-discord-mirror/0.1"},
    )
    if status >= 400:
        raise RuntimeError(f"discord GET messages failed: {status} {resp[:200]!r}")
    msgs = json.loads(resp)
    msgs.sort(key=lambda m: int(m["id"]))  # chronological by snowflake
    return msgs


def ensure_mirror_thread(conn: sqlite3.Connection, *, channel_id: str,
                         title: str) -> int:
    """Find or create the bus thread that mirrors this Discord channel."""
    row = conn.execute(
        "SELECT id FROM threads WHERE discord_thread_id=?",
        (channel_id,),
    ).fetchone()
    if row:
        return row[0]
    now = int(time.time())
    cur = conn.cursor()
    cur.execute(
        "INSERT INTO threads(board, title, author, audience, "
        "discord_thread_id, created_at, updated_at) "
        "VALUES(?,?,?,?,?,?,?)",
        ("chitin", title, "discord-mirror", None, channel_id, now, now),
    )
    conn.commit()
    return cur.lastrowid


def ingest_messages(conn: sqlite3.Connection, thread_id: int,
                    messages: list[dict]) -> int:
    """Append Discord messages into the bus thread. Idempotent via
    discord_message_id uniqueness check.
    """
    count = 0
    for m in messages:
        # Skip if already ingested
        already = conn.execute(
            "SELECT 1 FROM messages WHERE discord_message_id=? LIMIT 1",
            (m["id"],),
        ).fetchone()
        if already:
            continue
        author = (m.get("author") or {}).get("username", "discord")
        body = m.get("content") or ""
        # If a Discord message is empty (image-only / sticker), surface a stub
        if not body:
            atts = m.get("attachments") or []
            embs = m.get("embeds") or []
            body = f"(attachment x{len(atts)}, embed x{len(embs)})"
        # Discord ts ('timestamp' is ISO; snowflake encodes epoch ms in id)
        snowflake = int(m["id"])
        epoch = ((snowflake >> 22) + 1420070400000) // 1000
        conn.execute(
            "INSERT INTO messages(thread_id, author, body, kind, "
            "discord_message_id, created_at) VALUES(?,?,?,?,?,?)",
            (thread_id, author, body, "message", m["id"], epoch),
        )
        count += 1
    if count:
        conn.execute(
            "UPDATE threads SET updated_at=strftime('%s','now') WHERE id=?",
            (thread_id,),
        )
        conn.commit()
    return count


def poll_once(conn: sqlite3.Connection, *, channel_id: str, token: str,
              thread_title: str) -> dict:
    """One-shot poll: fetch new messages, append to mirror thread, advance cursor."""
    cursor = get_cursor(conn, channel_id)
    msgs = fetch_new_messages(channel_id, token=token, after=cursor)
    if not msgs:
        return {"fetched": 0, "ingested": 0, "cursor": cursor}
    thread_id = ensure_mirror_thread(conn, channel_id=channel_id,
                                     title=thread_title)
    ingested = ingest_messages(conn, thread_id, msgs)
    new_cursor = max(msgs, key=lambda m: int(m["id"]))["id"]
    set_cursor(conn, channel_id, new_cursor)
    return {"fetched": len(msgs), "ingested": ingested,
            "cursor": new_cursor, "thread_id": thread_id}


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

def cmd_poll(args) -> int:
    token = os.environ.get("DISCORD_BOT_TOKEN", "")
    channel = args.channel or os.environ.get("DISCORD_MIRROR_CHANNEL_ID", "")
    if not token or not channel:
        print("error: DISCORD_BOT_TOKEN and DISCORD_MIRROR_CHANNEL_ID required",
              file=sys.stderr)
        return 2
    title = (args.title
             or os.environ.get("DISCORD_MIRROR_THREAD_TITLE")
             or f"Discord mirror — channel {channel}")
    conn = sqlite3.connect(str(db_path()))
    ensure_cursor_table(conn)
    result = poll_once(conn, channel_id=channel, token=token, thread_title=title)
    print(json.dumps(result))
    return 0


def cmd_poll_all(args) -> int:
    """Spec 023 R2: iterate every threads row with a discord_thread_id
    and poll each. Cron-friendly entry point; replaces per-channel
    invocations for the inbound-poll job.

    Concurrency safety (R6): a single in-flight lock file at
    ~/.hermes/.cache/discord-poll-all.lock prevents the next cron tick
    from starting a second poll while this one is still running. Lock
    is auto-released on normal exit + on process death (PID check).
    """
    # spec: 023-agent-bus-bidirectional-liveness
    import fcntl
    import errno

    token = os.environ.get("DISCORD_BOT_TOKEN", "")
    if not token:
        print("error: DISCORD_BOT_TOKEN required", file=sys.stderr)
        return 2

    lock_path = Path.home() / ".hermes" / ".cache" / "discord-poll-all.lock"
    lock_path.parent.mkdir(parents=True, exist_ok=True)
    lock_fp = open(lock_path, "w")
    try:
        fcntl.flock(lock_fp.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError as e:
        if e.errno in (errno.EWOULDBLOCK, errno.EAGAIN):
            print(json.dumps({"skipped": "previous-poll-still-running"}))
            return 0
        raise
    lock_fp.write(str(os.getpid()))
    lock_fp.flush()

    try:
        conn = sqlite3.connect(str(db_path()))
        conn.row_factory = sqlite3.Row
        ensure_cursor_table(conn)
        rows = conn.execute(
            "SELECT id, title, discord_thread_id FROM threads "
            "WHERE discord_thread_id IS NOT NULL AND discord_thread_id != ''"
        ).fetchall()

        results = []
        for row in rows:
            try:
                r = poll_once(
                    conn,
                    channel_id=row["discord_thread_id"],
                    token=token,
                    thread_title=row["title"],
                )
                results.append({"thread_id": row["id"], **r})
            except Exception as exc:
                results.append({
                    "thread_id": row["id"],
                    "channel_id": row["discord_thread_id"],
                    "error": f"{type(exc).__name__}: {exc}",
                })
        print(json.dumps({"threads": len(rows), "results": results}))
        return 0
    finally:
        try:
            fcntl.flock(lock_fp.fileno(), fcntl.LOCK_UN)
        except Exception:
            pass
        lock_fp.close()


def cmd_push(args) -> int:
    webhook = args.webhook or os.environ.get("DISCORD_WEBHOOK_URL", "")
    if not webhook:
        print("error: --webhook or DISCORD_WEBHOOK_URL required", file=sys.stderr)
        return 2
    conn = sqlite3.connect(str(db_path()))
    conn.row_factory = sqlite3.Row
    msgs = conn.execute(
        "SELECT id, author, body FROM messages "
        "WHERE thread_id=? AND id > ? ORDER BY id",
        (args.thread_id, args.after or 0),
    ).fetchall()
    posted = 0
    for m in msgs:
        content = f"**{m['author']}**: {m['body']}"
        post_to_discord_webhook(webhook, content=content)
        posted += 1
    print(json.dumps({"posted": posted, "last_id": msgs[-1]["id"] if msgs else None}))
    return 0


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)

    p_poll = sub.add_parser("poll", help="Ingest new Discord messages")
    p_poll.add_argument("--channel", help="Channel id (else env)")
    p_poll.add_argument("--title", help="Bus mirror thread title")
    p_poll.set_defaults(func=cmd_poll)

    # spec: 023-agent-bus-bidirectional-liveness
    p_poll_all = sub.add_parser(
        "poll-all",
        help="Poll every threads row with a discord_thread_id (spec 023 R2)",
    )
    p_poll_all.set_defaults(func=cmd_poll_all)

    p_push = sub.add_parser("push", help="Post bus messages to Discord webhook")
    p_push.add_argument("thread_id", type=int)
    p_push.add_argument("--after", type=int, help="Only messages > this msg id")
    p_push.add_argument("--webhook", help="Webhook URL (else env)")
    p_push.set_defaults(func=cmd_push)

    args = ap.parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
