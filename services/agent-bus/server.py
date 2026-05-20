#!/usr/bin/env python3
"""agent-bus MCP stdio server.

Implements the Anthropic Model Context Protocol (MCP) over stdio using
plain JSON-RPC 2.0 — no external dependencies. Agents add this server
via `claude mcp add agent-bus python3 services/agent-bus/server.py` and
get the 7 bus tools as ordinary tool calls.

Protocol notes (from the public MCP spec, 2026 stable):
  - JSON-RPC 2.0 over stdio (one JSON object per line on stdout/stdin).
  - Required methods: `initialize`, `tools/list`, `tools/call`.
  - Optional: `ping`, `notifications/initialized`.
  - Errors use JSON-RPC error envelope (-32600 invalid request, -32601
    method not found, -32602 invalid params, -32603 internal error).
  - Tool results return `{content: [{type: "text", text: <json>}]}` where
    `text` is a stringified JSON payload — every consumer parses it.

Tools (see `.specify/specs/001-agent-bus/spec.md`):
  bus_post_thread, bus_reply, bus_list_threads, bus_read_thread,
  bus_inbox, bus_mark_read, bus_attach
"""
from __future__ import annotations

import json
import sys
import time
from typing import Any, Callable

from db import connect
import discord_push
import discord_routes


PROTOCOL_VERSION = "2025-06-18"
SERVER_NAME = "agent-bus"
SERVER_VERSION = "0.1.0"


# ---------------------------------------------------------------------------
# Tool implementations. Each returns a JSON-serialisable dict; the dispatcher
# wraps it in the MCP content envelope. All writes use a single transaction
# so a process kill mid-call leaves the DB consistent (FR-005).
# ---------------------------------------------------------------------------

def _now() -> int:
    return int(time.time())


# Canonical Discord display names per agent. Mentions in #<channel> must
# match the bot's display name with exact case or Discord won't fire the
# notification. The bus normalizes any case variant to the canonical form
# before write + push so callers never have to remember (e.g. `@clawta` →
# `@Clawta`). Operator-confirmed canonicals (2026-05-18).
#
# To add a new agent: append a lowercase → canonical-case entry below,
# then `claude mcp restart agent-bus` (or kill + let the supervisor
# respawn) so the running server picks up the new mapping. The regex
# _MENTION_RE is rebuilt from this dict at module load, so no other
# edits are required.
_CANONICAL_MENTIONS: dict[str, str] = {
    "clawta": "Clawta",
    "ares": "Ares",
    "hermes": "hermes",
    "icarus": "icarus",
    "red": "red",
    "copilot": "copilot",
}

# Pre-compile one regex matching any `@<name>` token (case-insensitive)
# whose suffix is one of the canonical agents. \b at both ends avoids
# matching `@clawta-poller` or `email@clawta.com`.
import re as _re  # local alias to keep top-of-file imports terse
_MENTION_RE = _re.compile(
    r"(?<![A-Za-z0-9_])@(" + "|".join(_CANONICAL_MENTIONS) + r")(?![A-Za-z0-9_-])",
    _re.IGNORECASE,
)


def _canonicalize_mentions(body: str) -> str:
    """Rewrite @<agent> tokens to their canonical case so Discord pings fire.

    Invariant: for every agent in _CANONICAL_MENTIONS, any case variant
    of @<name> in `body` is rewritten to the canonical case. Other text
    (URLs, identifiers, partial words) is left untouched per the
    word-boundary lookarounds in _MENTION_RE.
    """
    def _replace(m: _re.Match) -> str:
        return "@" + _CANONICAL_MENTIONS[m.group(1).lower()]
    return _MENTION_RE.sub(_replace, body)


def _touch_agent(conn, agent_id: str) -> None:
    """Self-register the caller (lazy create + last-seen bump)."""
    now = _now()
    conn.execute(
        "INSERT INTO agents(id, last_seen_at) VALUES(?, ?) "
        "ON CONFLICT(id) DO UPDATE SET last_seen_at=excluded.last_seen_at",
        (agent_id, now),
    )


def bus_post_thread(conn, *, author: str, title: str, body: str,
                    board: str | None = None, task_id: str | None = None,
                    audience: str | None = None) -> dict:
    """Create a top-level thread + its first message in one transaction.

    Discord routing: the deterministic resolver (discord_routes) decides
    which channel the new thread mirrors. The resolved channel id is
    stamped onto ``threads.discord_thread_id`` at INSERT time so future
    replies via ``bus_reply`` automatically push to the same channel.

    Routing precedence: audience > board > global default (see
    discord_routes.resolve_channel docstring).

    If no route resolves AND no global default is configured, the
    thread is created with ``discord_thread_id=NULL`` and a warning is
    emitted to stderr. The bus row is the committed ground truth; a
    missing Discord route is a config gap, not a thread-creation
    blocker. Operators surface these via discord-push-failures.jsonl.
    """
    body = _canonicalize_mentions(body)
    now = _now()
    cur = conn.cursor()
    _touch_agent(conn, author)

    channel_id = _resolve_route_or_warn(
        conn, board=board, audience=audience, where="bus_post_thread",
    )
    cur.execute(
        "INSERT INTO threads(board, task_id, title, author, audience, "
        "discord_thread_id, created_at, updated_at) "
        "VALUES(?,?,?,?,?,?,?,?)",
        (board, task_id, title, author, audience, channel_id, now, now),
    )
    thread_id = cur.lastrowid
    cur.execute(
        "INSERT INTO messages(thread_id, author, audience, body, kind, created_at) "
        "VALUES(?,?,?,?,?,?)",
        (thread_id, author, audience, body, "message", now),
    )
    message_id = cur.lastrowid
    conn.commit()
    # Push the first message immediately if a channel was resolved.
    # Per pos-002 AC5: stamp the returned snowflake onto messages so the
    # inbound mirror doesn't re-import it as a duplicate.
    snowflake = discord_push.try_push(
        channel_id=channel_id, author=author, body=body,
    ) if channel_id else None
    if snowflake:
        try:
            conn.execute(
                "UPDATE messages SET discord_message_id=? WHERE id=?",
                (snowflake, message_id),
            )
            conn.commit()
        except Exception as exc:
            print(
                f"[bus_post_thread] WARN: failed to stamp snowflake "
                f"{snowflake} onto bus msg {message_id}: {exc}",
                file=sys.stderr,
            )
    return {"thread_id": thread_id, "message_id": message_id,
            "discord_message_id": snowflake, "discord_channel_id": channel_id,
            "created_at": now}


def _resolve_route_or_warn(conn, *, board: str | None, audience: str | None,
                           where: str) -> str | None:
    """Resolve a Discord channel for the (board, audience) pair.

    Catches UnroutableError and converts to a stderr warning + None so
    the bus write itself never fails on a missing route. The warning
    surface (stderr + failure JSONL via discord_push) lets operators
    notice and add the route.
    """
    try:
        return discord_routes.resolve_channel(
            conn, board=board, audience=audience,
        )
    except discord_routes.UnroutableError as exc:
        print(
            f"[{where}] no Discord route for board={board!r} "
            f"audience={audience!r}: {exc}",
            file=sys.stderr,
        )
        return None


def bus_reply(conn, *, author: str, thread_id: int, body: str,
              parent_id: int | None = None, audience: str | None = None,
              kind: str = "message", ack_required: bool = False) -> dict:
    """Reply to an existing thread. parent_id must belong to the same thread."""
    body = _canonicalize_mentions(body)
    now = _now()
    cur = conn.cursor()
    thread = cur.execute(
        "SELECT id FROM threads WHERE id=?", (thread_id,)
    ).fetchone()
    if not thread:
        raise ValueError(f"thread {thread_id} not found")
    if parent_id is not None:
        parent = cur.execute(
            "SELECT thread_id FROM messages WHERE id=?", (parent_id,)
        ).fetchone()
        if not parent:
            raise ValueError(f"parent message {parent_id} not found")
        if parent["thread_id"] != thread_id:
            raise ValueError(
                f"parent {parent_id} belongs to thread {parent['thread_id']}, "
                f"not {thread_id}"
            )
    if kind not in {"message", "directive", "ack", "system"}:
        raise ValueError(f"invalid kind {kind!r}")
    _touch_agent(conn, author)
    cur.execute(
        "INSERT INTO messages(thread_id, parent_id, author, audience, body, "
        "kind, ack_required, created_at) VALUES(?,?,?,?,?,?,?,?)",
        (thread_id, parent_id, author, audience, body, kind,
         1 if ack_required else 0, now),
    )
    message_id = cur.lastrowid
    cur.execute(
        "UPDATE threads SET updated_at=? WHERE id=?", (now, thread_id)
    )
    # Look up the Discord channel id BEFORE commit so a slow Discord POST
    # doesn't hold the SQLite write lock.
    channel_row = cur.execute(
        "SELECT discord_thread_id FROM threads WHERE id=?", (thread_id,)
    ).fetchone()
    conn.commit()
    channel_id = channel_row["discord_thread_id"] if channel_row else None
    # Per pos-002 AC5: stamp the returned Discord snowflake back onto
    # the bus row BEFORE the next inbound mirror poll runs. Without
    # this stamp, the inbound poll re-imports the message as a
    # duplicate (canonical 3419→3420 echo bug).
    snowflake = discord_push.try_push(
        channel_id=channel_id, author=author, body=body
    )
    if snowflake:
        try:
            conn.execute(
                "UPDATE messages SET discord_message_id=? WHERE id=?",
                (snowflake, message_id),
            )
            conn.commit()
        except Exception as exc:
            # Stamp failure is non-fatal — the message is already on
            # both Discord and the bus; it'll just produce a duplicate
            # on the next inbound poll. Log loudly so this surfaces.
            print(
                f"[bus_reply] WARN: failed to stamp snowflake "
                f"{snowflake} onto bus msg {message_id}: {exc}",
                file=__import__("sys").stderr,
            )
    return {"message_id": message_id, "created_at": now,
            "discord_message_id": snowflake}


def bus_list_threads(conn, *, board: str | None = None, status: str | None = None,
                     audience: str | None = None, since: int | None = None,
                     limit: int = 50) -> dict:
    """List threads with optional filters. audience match is membership-based:
    a thread with audience NULL matches any caller; a thread with
    audience='red,hermes' matches caller='red' or 'hermes'.
    """
    where = ["1=1"]
    args: list[Any] = []
    if board is not None:
        where.append("board = ?"); args.append(board)
    if status is not None:
        where.append("status = ?"); args.append(status)
    if since is not None:
        where.append("updated_at >= ?"); args.append(since)
    rows = conn.execute(
        f"SELECT id, board, task_id, title, author, audience, status, "
        f"discord_thread_id, created_at, updated_at FROM threads "
        f"WHERE {' AND '.join(where)} ORDER BY updated_at DESC LIMIT ?",
        (*args, int(limit)),
    ).fetchall()
    threads = [dict(r) for r in rows]
    if audience is not None:
        # Filter in Python since CSV-membership in SQL is awkward.
        def matches(t: dict) -> bool:
            if not t["audience"]:
                return True
            members = {m.strip() for m in t["audience"].split(",") if m.strip()}
            return audience in members
        threads = [t for t in threads if matches(t)]
    return {"threads": threads}


def bus_read_thread(conn, *, thread_id: int) -> dict:
    """Return the thread row + all its messages + attachments."""
    t = conn.execute(
        "SELECT id, board, task_id, title, author, audience, status, "
        "discord_thread_id, created_at, updated_at FROM threads WHERE id=?",
        (thread_id,),
    ).fetchone()
    if not t:
        raise ValueError(f"thread {thread_id} not found")
    msgs = conn.execute(
        "SELECT id, parent_id, author, audience, body, kind, "
        "discord_message_id, ack_required, created_at FROM messages "
        "WHERE thread_id=? ORDER BY created_at, id",
        (thread_id,),
    ).fetchall()
    atts = conn.execute(
        "SELECT id, kind, ref, display, created_at FROM attachments "
        "WHERE thread_id=? ORDER BY created_at, id",
        (thread_id,),
    ).fetchall()
    return {
        "thread": dict(t),
        "messages": [dict(m) for m in msgs],
        "attachments": [dict(a) for a in atts],
    }


def bus_inbox(conn, *, agent_id: str, unread_only: bool = True,
              limit: int = 50) -> dict:
    """Messages addressed to agent_id (audience NULL = public, or member of CSV)
    that the agent hasn't read yet (when unread_only).
    """
    _touch_agent(conn, agent_id)
    candidates = conn.execute(
        "SELECT m.id, m.thread_id, m.parent_id, m.author, m.audience, m.body, "
        "m.kind, m.ack_required, m.created_at, t.title AS thread_title, "
        "t.board AS thread_board FROM messages m "
        "JOIN threads t ON t.id = m.thread_id "
        "ORDER BY m.created_at DESC LIMIT ?",
        (int(limit) * 4,),  # over-fetch since we filter Python-side
    ).fetchall()
    if unread_only:
        read_ids = {
            row["message_id"] for row in conn.execute(
                "SELECT message_id FROM reads WHERE agent_id=?", (agent_id,)
            ).fetchall()
        }
    else:
        read_ids = set()
    out: list[dict] = []
    for r in candidates:
        d = dict(r)
        # Audience match: NULL = public; CSV = membership; addressed to self
        # or to "*" both count.
        if d["audience"]:
            members = {m.strip() for m in d["audience"].split(",") if m.strip()}
            if agent_id not in members and "*" not in members:
                continue
        # Don't surface the agent's own posts in their inbox.
        if d["author"] == agent_id:
            continue
        if unread_only and d["id"] in read_ids:
            continue
        out.append(d)
        if len(out) >= int(limit):
            break
    return {"messages": out, "count": len(out)}


def bus_mark_read(conn, *, agent_id: str, message_id: int) -> dict:
    """Idempotent ack: insert-or-ignore the read receipt."""
    now = _now()
    _touch_agent(conn, agent_id)
    conn.execute(
        "INSERT OR IGNORE INTO reads(message_id, agent_id, read_at) VALUES(?,?,?)",
        (message_id, agent_id, now),
    )
    conn.commit()
    return {"ok": True, "read_at": now}


def bus_attach(conn, *, thread_id: int, kind: str, ref: str,
               display: str | None = None) -> dict:
    """Add a typed attachment to a thread."""
    if kind not in {"spec", "pr", "task", "discord", "url", "file"}:
        raise ValueError(f"invalid attachment kind {kind!r}")
    t = conn.execute("SELECT id FROM threads WHERE id=?", (thread_id,)).fetchone()
    if not t:
        raise ValueError(f"thread {thread_id} not found")
    now = _now()
    cur = conn.cursor()
    cur.execute(
        "INSERT INTO attachments(thread_id, kind, ref, display, created_at) "
        "VALUES(?,?,?,?,?)",
        (thread_id, kind, ref, display, now),
    )
    conn.commit()
    return {"attachment_id": cur.lastrowid, "created_at": now}


# ---------------------------------------------------------------------------
# Discord route CRUD tools. Operators manage (audience, board, global)
# routes through these — no raw SQL.
# ---------------------------------------------------------------------------


def bus_routes_set(conn, *, scope: str, key: str,
                   channel_id: str | None = None,
                   priority: int = 100) -> dict:
    """Upsert a Discord route. channel_id=null means 'explicit mute'."""
    route = discord_routes.set_route(
        conn, scope=scope, key=key, channel_id=channel_id, priority=priority,
    )
    return {
        "scope": route.scope, "key": route.key,
        "channel_id": route.channel_id, "priority": route.priority,
        "updated_at": route.updated_at,
    }


def bus_routes_unset(conn, *, scope: str, key: str) -> dict:
    removed = discord_routes.unset_route(conn, scope=scope, key=key)
    return {"removed": removed}


def bus_routes_list(conn) -> dict:
    rows = discord_routes.list_routes(conn)
    return {"routes": [
        {"scope": r.scope, "key": r.key, "channel_id": r.channel_id,
         "priority": r.priority, "updated_at": r.updated_at}
        for r in rows
    ]}


def bus_routes_resolve(conn, *, board: str | None = None,
                       audience: str | None = None) -> dict:
    """Diagnostic: show what resolve_channel would return for (board, audience).
    Returns ``{channel_id, routed}`` — routed is False if UnroutableError."""
    try:
        ch = discord_routes.resolve_channel(conn, board=board, audience=audience)
        return {"channel_id": ch, "routed": True,
                "muted": ch is None}
    except discord_routes.UnroutableError as e:
        return {"channel_id": None, "routed": False, "error": str(e)}


# ---------------------------------------------------------------------------
# Tool catalog (returned by tools/list). JSON Schema for inputs.
# ---------------------------------------------------------------------------

TOOLS: list[dict] = [
    {
        "name": "bus_post_thread",
        "description": "Create a new thread with its first message. Use when starting a new topic.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "author":   {"type": "string", "description": "Agent id of the poster (e.g. red, hermes, claude-code)."},
                "title":    {"type": "string"},
                "body":     {"type": "string", "description": "First message body."},
                "board":    {"type": ["string", "null"], "description": "chitin | readybench | null=global"},
                "task_id":  {"type": ["string", "null"], "description": "Optional kanban ticket id (e.g. t_abc123)."},
                "audience": {"type": ["string", "null"], "description": "Comma-separated agent ids; null=public."},
            },
            "required": ["author", "title", "body"],
        },
    },
    {
        "name": "bus_reply",
        "description": "Reply to an existing thread. Optional parent_id to thread the reply.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "author":       {"type": "string"},
                "thread_id":    {"type": "integer"},
                "body":         {"type": "string"},
                "parent_id":    {"type": ["integer", "null"]},
                "audience":     {"type": ["string", "null"]},
                "kind":         {"type": "string", "enum": ["message", "directive", "ack", "system"]},
                "ack_required": {"type": "boolean"},
            },
            "required": ["author", "thread_id", "body"],
        },
    },
    {
        "name": "bus_list_threads",
        "description": "List recent threads filtered by board/status/audience/since.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "board":    {"type": ["string", "null"]},
                "status":   {"type": ["string", "null"], "enum": [None, "open", "resolved", "archived"]},
                "audience": {"type": ["string", "null"], "description": "Filter to threads visible to this agent id."},
                "since":    {"type": ["integer", "null"], "description": "Epoch seconds — only threads updated >= since."},
                "limit":    {"type": "integer", "default": 50},
            },
        },
    },
    {
        "name": "bus_read_thread",
        "description": "Return the thread, all its messages in created order, and its attachments.",
        "inputSchema": {
            "type": "object",
            "properties": {"thread_id": {"type": "integer"}},
            "required": ["thread_id"],
        },
    },
    {
        "name": "bus_inbox",
        "description": "Messages addressed to agent_id that the agent hasn't read. Use at session start.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "agent_id":    {"type": "string"},
                "unread_only": {"type": "boolean", "default": True},
                "limit":       {"type": "integer", "default": 50},
            },
            "required": ["agent_id"],
        },
    },
    {
        "name": "bus_mark_read",
        "description": "Mark a message as read by agent_id. Idempotent.",
        "inputSchema": {
            "type": "object",
            "properties": {"agent_id": {"type": "string"}, "message_id": {"type": "integer"}},
            "required": ["agent_id", "message_id"],
        },
    },
    {
        "name": "bus_attach",
        "description": "Attach a typed link to a thread (spec | pr | task | discord | url | file).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "thread_id": {"type": "integer"},
                "kind":      {"type": "string", "enum": ["spec", "pr", "task", "discord", "url", "file"]},
                "ref":       {"type": "string"},
                "display":   {"type": ["string", "null"]},
            },
            "required": ["thread_id", "kind", "ref"],
        },
    },
    {
        "name": "bus_routes_set",
        "description": "Upsert a Discord route. scope ∈ {audience,board,global}; key is agent_id/board name/'*'; channel_id=null mutes.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "scope":      {"type": "string", "enum": ["audience", "board", "global"]},
                "key":        {"type": "string"},
                "channel_id": {"type": ["string", "null"]},
                "priority":   {"type": "integer", "default": 100},
            },
            "required": ["scope", "key"],
        },
    },
    {
        "name": "bus_routes_unset",
        "description": "Remove a Discord route. Returns {removed: bool}.",
        "inputSchema": {
            "type": "object",
            "properties": {
                "scope": {"type": "string", "enum": ["audience", "board", "global"]},
                "key":   {"type": "string"},
            },
            "required": ["scope", "key"],
        },
    },
    {
        "name": "bus_routes_list",
        "description": "List all Discord routes ordered by scope/priority.",
        "inputSchema": {"type": "object", "properties": {}},
    },
    {
        "name": "bus_routes_resolve",
        "description": "Diagnostic: show which channel resolve_channel would pick for (board, audience).",
        "inputSchema": {
            "type": "object",
            "properties": {
                "board":    {"type": ["string", "null"]},
                "audience": {"type": ["string", "null"]},
            },
        },
    },
]

TOOL_DISPATCH: dict[str, Callable] = {
    "bus_post_thread":     bus_post_thread,
    "bus_reply":           bus_reply,
    "bus_list_threads":    bus_list_threads,
    "bus_read_thread":     bus_read_thread,
    "bus_inbox":           bus_inbox,
    "bus_mark_read":       bus_mark_read,
    "bus_attach":          bus_attach,
    "bus_routes_set":      bus_routes_set,
    "bus_routes_unset":    bus_routes_unset,
    "bus_routes_list":     bus_routes_list,
    "bus_routes_resolve":  bus_routes_resolve,
}


# ---------------------------------------------------------------------------
# JSON-RPC dispatcher. Single function so tests can drive it without subprocess.
# ---------------------------------------------------------------------------

def handle_request(conn, req: dict) -> dict | None:
    """Handle one JSON-RPC request. Returns the response dict, or None for
    notifications (which carry no id). Errors are returned in JSON-RPC envelope.
    """
    rpc_id = req.get("id")
    method = req.get("method")
    params = req.get("params") or {}
    is_notification = "id" not in req

    def err(code: int, message: str) -> dict:
        return {"jsonrpc": "2.0", "id": rpc_id, "error": {"code": code, "message": message}}

    def ok(result: Any) -> dict:
        return {"jsonrpc": "2.0", "id": rpc_id, "result": result}

    if method == "initialize":
        return ok({
            "protocolVersion": PROTOCOL_VERSION,
            "capabilities": {"tools": {}},
            "serverInfo": {"name": SERVER_NAME, "version": SERVER_VERSION},
        })
    if method == "notifications/initialized":
        return None  # notification — no response
    if method == "ping":
        return ok({})
    if method == "tools/list":
        return ok({"tools": TOOLS})
    if method == "tools/call":
        name = params.get("name")
        args = params.get("arguments") or {}
        fn = TOOL_DISPATCH.get(name)
        if not fn:
            return err(-32601, f"unknown tool: {name}")
        try:
            result = fn(conn, **args)
        except TypeError as e:
            return err(-32602, f"invalid params for {name}: {e}")
        except ValueError as e:
            return err(-32602, str(e))
        except Exception as e:  # pragma: no cover — last-resort guard
            return err(-32603, f"internal error: {e!r}")
        return ok({"content": [{"type": "text", "text": json.dumps(result)}]})

    if is_notification:
        return None
    return err(-32601, f"unknown method: {method}")


def serve_stdio() -> None:  # pragma: no cover (covered via subprocess smoke)
    """Read JSON-RPC requests line-by-line from stdin; write responses to stdout."""
    conn = connect()
    for raw in sys.stdin:
        raw = raw.strip()
        if not raw:
            continue
        try:
            req = json.loads(raw)
        except json.JSONDecodeError:
            sys.stdout.write(json.dumps({
                "jsonrpc": "2.0", "id": None,
                "error": {"code": -32700, "message": "parse error"},
            }) + "\n")
            sys.stdout.flush()
            continue
        resp = handle_request(conn, req)
        if resp is not None:
            sys.stdout.write(json.dumps(resp) + "\n")
            sys.stdout.flush()


if __name__ == "__main__":
    serve_stdio()
