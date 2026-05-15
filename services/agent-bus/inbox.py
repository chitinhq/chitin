#!/usr/bin/env python3
"""Print the agent-bus inbox in markdown — meant for Claude Code SessionStart.

Usage:
  python3 inbox.py [--agent red] [--limit 10] [--mark-read]

Behavior:
- Silent (zero output, exit 0) when the DB doesn't exist or the inbox is
  empty. SessionStart context shouldn't add noise when there's nothing to say.
- Markdown output suitable for direct injection into the system context.
- `--mark-read` is OFF by default — listing the inbox doesn't auto-ack
  (we want the human to explicitly clear after reading).

Wire into `~/.claude/settings.json`:

  {
    "hooks": {
      "SessionStart": [{
        "hooks": [{
          "type": "command",
          "command": "python3 /home/red/workspace/chitin/services/agent-bus/inbox.py --agent red"
        }]
      }]
    }
  }
"""
from __future__ import annotations

import argparse
import os
import sqlite3
import sys
import time
from pathlib import Path


DEFAULT_DB = Path.home() / ".chitin" / "agent-bus" / "bus.db"


def fmt_age(epoch: int) -> str:
    delta = max(0, int(time.time()) - int(epoch))
    if delta < 60:    return f"{delta}s ago"
    if delta < 3600:  return f"{delta // 60}m ago"
    if delta < 86400: return f"{delta // 3600}h ago"
    return f"{delta // 86400}d ago"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--agent", default=os.environ.get("AGENT_BUS_AGENT", "red"))
    ap.add_argument("--limit", type=int, default=10)
    ap.add_argument("--mark-read", action="store_true")
    args = ap.parse_args()

    db_path = Path(os.environ.get("AGENT_BUS_DB") or str(DEFAULT_DB))
    if not db_path.exists():
        return 0  # nothing to say

    conn = sqlite3.connect(str(db_path))
    conn.row_factory = sqlite3.Row

    # Pull recent unread messages addressed to the agent.
    # Audience match: NULL=public, '*'=broadcast, CSV membership.
    rows = conn.execute(
        """
        SELECT m.id, m.thread_id, m.author, m.audience, m.body, m.kind,
               m.ack_required, m.created_at,
               t.title AS thread_title, t.board AS thread_board
        FROM messages m
        JOIN threads t ON t.id = m.thread_id
        WHERE m.author != ?
          AND NOT EXISTS (SELECT 1 FROM reads r WHERE r.message_id = m.id AND r.agent_id = ?)
        ORDER BY m.created_at DESC
        LIMIT ?
        """,
        (args.agent, args.agent, args.limit * 4),
    ).fetchall()

    inbox: list[sqlite3.Row] = []
    for r in rows:
        if r["audience"]:
            members = {m.strip() for m in r["audience"].split(",") if m.strip()}
            if args.agent not in members and "*" not in members:
                continue
        inbox.append(r)
        if len(inbox) >= args.limit:
            break

    if not inbox:
        return 0

    out = [f"## agent-bus inbox — {len(inbox)} unread for `{args.agent}`", ""]
    for m in inbox:
        board = f" `{m['thread_board']}`" if m["thread_board"] else ""
        kind = f" `{m['kind']}`" if m["kind"] != "message" else ""
        ack = " **(ack required)**" if m["ack_required"] else ""
        out.append(
            f"- **{m['thread_title']}**{board}{kind}{ack} "
            f"— from `{m['author']}`, {fmt_age(m['created_at'])} "
            f"(thread {m['thread_id']}, msg {m['id']})"
        )
        # First line of the body, truncated for the summary.
        first_line = (m["body"] or "").splitlines()[0] if m["body"] else ""
        if first_line:
            snippet = first_line[:140] + ("…" if len(first_line) > 140 else "")
            out.append(f"  > {snippet}")
    out.append("")
    out.append("Read full thread: `bus_read_thread(thread_id=N)` · "
               "Ack: `bus_mark_read(agent_id='" + args.agent + "', message_id=N)`")
    print("\n".join(out))

    if args.mark_read:
        now = int(time.time())
        conn.executemany(
            "INSERT OR IGNORE INTO reads(message_id, agent_id, read_at) VALUES(?,?,?)",
            [(m["id"], args.agent, now) for m in inbox],
        )
        conn.commit()

    return 0


if __name__ == "__main__":
    sys.exit(main())
