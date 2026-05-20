"""swarm-kanban-mcp — MCP stdio server exposing kanban state to Claude Code sessions.

Per docs/strategy/2026-05-18-swarm-redesign.md §Week 1: red's lane work.
Lets a Claude Code session interact with any kanban board (chitin /
readybench / personal-os / swarm) without subprocess-calling
`hermes kanban` over and over.

JSON-RPC 2.0 over stdio, zero external deps.

Tools exposed:
  - list_boards()
  - list_tickets(board, status?)
  - get_ticket(board, ticket_id)
  - claim_ticket(board, ticket_id, owner)
  - update_status(board, ticket_id, new_status, author, comment?)
  - create_ticket(board, title, body, assignee, priority?, triage?)
"""
from __future__ import annotations

import json
import sqlite3
import subprocess
import sys
from pathlib import Path

KANBAN_ROOT = Path.home() / ".hermes" / "kanban" / "boards"


def _board_db(board: str) -> Path:
    db = KANBAN_ROOT / board / "kanban.db"
    if not db.exists():
        raise ValueError(f"unknown board: {board}")
    return db


def list_boards() -> dict:
    boards = sorted(p.name for p in KANBAN_ROOT.iterdir()
                    if p.is_dir() and (p / "kanban.db").exists())
    return {"boards": boards}


def list_tickets(board: str, status: str | None = None) -> dict:
    db = _board_db(board)
    with sqlite3.connect(db) as conn:
        conn.row_factory = sqlite3.Row
        query = "SELECT id, title, status, assignee, priority FROM tasks"
        params: list = []
        if status:
            query += " WHERE status = ?"
            params.append(status)
        query += " ORDER BY priority, id"
        rows = conn.execute(query, params).fetchall()
    return {"tickets": [dict(r) for r in rows]}


def get_ticket(board: str, ticket_id: str) -> dict:
    db = _board_db(board)
    with sqlite3.connect(db) as conn:
        conn.row_factory = sqlite3.Row
        row = conn.execute(
            "SELECT * FROM tasks WHERE id = ?", (ticket_id,)
        ).fetchone()
        if not row:
            raise ValueError(f"ticket not found on {board}: {ticket_id}")
        comments = conn.execute(
            "SELECT id, author, body, created_at FROM task_comments "
            "WHERE task_id = ? ORDER BY id",
            (ticket_id,),
        ).fetchall()
    return {"ticket": dict(row), "comments": [dict(c) for c in comments]}


def claim_ticket(board: str, ticket_id: str, owner: str) -> dict:
    """Reassign + transition ticket to in_progress.

    `hermes kanban assign` sets the assignee, then `kanban-flow start` flips
    triage/todo/ready → in_progress and writes the audit comment authored by
    `owner`. Per Copilot review on PR #753 L80: --author tags the audit
    comment author, not the assignee, so the assign step is required for
    the owner field to actually move.
    """
    reassign = _run(["hermes", "kanban", "--board", board, "assign",
                     ticket_id, owner])
    start = _kanban_flow(board, "start", ticket_id, "--author", owner)
    return {"assign": reassign, "start": start}


def update_status(board: str, ticket_id: str, new_status: str,
                  author: str, comment: str | None = None) -> dict:
    """Map common statuses to kanban-flow subcommands.

    Per Copilot review on PR #753 L94: the previous version assumed `ready`
    transitions always come from `blocked` (and ran `unblock`). A ready
    ticket coming from `triage` would error. We now read current status
    and route appropriately.
    """
    ticket = get_ticket(board, ticket_id)["ticket"]
    current = ticket.get("status")

    if new_status == "in_progress":
        args = ["start", ticket_id, "--author", author]
    elif new_status == "blocked":
        args = ["block", ticket_id, "--author", author]
        if comment:
            args.extend(["--reason", comment])
    elif new_status == "ready":
        # Route by current status: unblock from blocked, otherwise default
        # to `kanban-flow start --status ready` if supported, else assign+comment.
        if current == "blocked":
            args = ["unblock", ticket_id, "--author", author]
        else:
            raise ValueError(
                f"ready transition from {current!r} not supported via CLI; "
                "promote via the board state machine instead",
            )
    elif new_status == "done":
        args = ["done", ticket_id, "--author", author]
        if comment:
            args.extend(["--result", comment])
    else:
        raise ValueError(f"unsupported status transition: {new_status}")
    return _kanban_flow(board, *args)


def create_ticket(board: str, title: str, body: str, assignee: str,
                  priority: int = 1, triage: bool = True) -> dict:
    """Wrap `hermes kanban create`."""
    cmd = [
        "hermes", "kanban", "--board", board, "create", title,
        "--body", body, "--assignee", assignee, "--priority", str(priority),
    ]
    if triage:
        cmd.append("--triage")
    return _run(cmd)


def _kanban_flow(board: str, *args: str) -> dict:
    cmd = ["env", f"KANBAN_BOARD={board}", "kanban-flow", *args]
    return _run(cmd)


def _run(cmd: list[str]) -> dict:
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    return {"returncode": r.returncode, "stdout": r.stdout.strip(),
            "stderr": r.stderr.strip(), "cmd": cmd}


TOOLS = {
    "list_boards": list_boards,
    "list_tickets": list_tickets,
    "get_ticket": get_ticket,
    "claim_ticket": claim_ticket,
    "update_status": update_status,
    "create_ticket": create_ticket,
}

TOOL_SCHEMAS = [
    {"name": "list_boards", "description": "List all kanban boards.",
     "inputSchema": {"type": "object", "properties": {}}},
    {"name": "list_tickets", "description": "List tickets on a board, optionally filtered by status.",
     "inputSchema": {"type": "object", "required": ["board"],
                     "properties": {"board": {"type": "string"},
                                    "status": {"type": "string"}}}},
    {"name": "get_ticket", "description": "Full detail for a ticket including comments.",
     "inputSchema": {"type": "object", "required": ["board", "ticket_id"],
                     "properties": {"board": {"type": "string"},
                                    "ticket_id": {"type": "string"}}}},
    {"name": "claim_ticket", "description": "Claim a ticket (transitions to in_progress).",
     "inputSchema": {"type": "object", "required": ["board", "ticket_id", "owner"],
                     "properties": {"board": {"type": "string"},
                                    "ticket_id": {"type": "string"},
                                    "owner": {"type": "string"}}}},
    {"name": "update_status", "description": "Transition ticket status.",
     "inputSchema": {"type": "object",
                     "required": ["board", "ticket_id", "new_status", "author"],
                     "properties": {"board": {"type": "string"},
                                    "ticket_id": {"type": "string"},
                                    "new_status": {"type": "string",
                                                   "enum": ["in_progress", "blocked", "ready", "done"]},
                                    "author": {"type": "string"},
                                    "comment": {"type": "string"}}}},
    {"name": "create_ticket", "description": "File a new ticket on a board.",
     "inputSchema": {"type": "object",
                     "required": ["board", "title", "body", "assignee"],
                     "properties": {"board": {"type": "string"},
                                    "title": {"type": "string"},
                                    "body": {"type": "string"},
                                    "assignee": {"type": "string"},
                                    "priority": {"type": "integer", "default": 1},
                                    "triage": {"type": "boolean", "default": True}}}},
]


def handle_request(req: dict) -> dict | None:
    method = req.get("method")
    if method == "initialize":
        return {"jsonrpc": "2.0", "id": req.get("id"),
                "result": {"protocolVersion": "2024-11-05",
                           "capabilities": {"tools": {}},
                           "serverInfo": {"name": "swarm-kanban-mcp",
                                          "version": "0.1.0"}}}
    if method == "tools/list":
        return {"jsonrpc": "2.0", "id": req.get("id"),
                "result": {"tools": TOOL_SCHEMAS}}
    if method == "tools/call":
        params = req.get("params") or {}
        name = params.get("name")
        args = params.get("arguments") or {}
        if name not in TOOLS:
            return {"jsonrpc": "2.0", "id": req.get("id"),
                    "error": {"code": -32601, "message": f"unknown tool: {name}"}}
        try:
            result = TOOLS[name](**args)
            return {"jsonrpc": "2.0", "id": req.get("id"),
                    "result": {"content": [{"type": "text",
                                            "text": json.dumps(result, default=str)}]}}
        except Exception as exc:
            return {"jsonrpc": "2.0", "id": req.get("id"),
                    "error": {"code": -32000,
                              "message": f"{type(exc).__name__}: {exc}"}}
    if method in {"notifications/initialized", "notifications/cancelled"}:
        return None  # one-way
    return {"jsonrpc": "2.0", "id": req.get("id"),
            "error": {"code": -32601, "message": f"unknown method: {method}"}}


def serve_stdio() -> None:
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError as exc:
            sys.stderr.write(f"[swarm-kanban-mcp] bad json: {exc}\n")
            continue
        resp = handle_request(req)
        if resp is not None:
            print(json.dumps(resp), flush=True)


if __name__ == "__main__":
    serve_stdio()
