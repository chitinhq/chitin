#!/usr/bin/env python3
"""One-shot backfill for "Discord-dark" threads (mem 3695).

Background: before this PR landed, every thread created via
``bus_post_thread`` had ``discord_thread_id=NULL`` and never appeared
in Discord. Replies to those threads were therefore also Discord-dark
because ``bus_reply`` reads ``discord_thread_id`` and silently no-ops
on NULL.

This script applies the deterministic resolver to existing dark
threads so future replies on them DO land in Discord:

  for each thread with discord_thread_id IS NULL:
      ch = resolve_channel(board=thread.board, audience=thread.audience)
      if ch:
          UPDATE threads SET discord_thread_id = ch WHERE id = thread.id

It does NOT replay historical messages to Discord — that would spam
channels with hours of backfilled content. It only links the thread
forward.

Run:
  python3 services/agent-bus/backfill_discord_routes.py            # dry-run
  python3 services/agent-bus/backfill_discord_routes.py --apply    # commit
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--apply", action="store_true",
                   help="commit UPDATEs (default: dry-run)")
    p.add_argument("--db", default=None,
                   help="agent-bus DB path (default: $AGENT_BUS_DB or ~/.chitin/agent-bus/bus.db)")
    args = p.parse_args(argv)

    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import db as bus_db          # noqa: E402
    import discord_routes        # noqa: E402

    db_path = Path(args.db) if args.db else bus_db.db_path()
    conn = bus_db.connect(db_path)

    summary = {
        "db": str(db_path),
        "applied": args.apply,
        "linked": [],
        "skipped_unroutable": [],
        "skipped_muted": [],
    }

    try:
        dark = conn.execute(
            "SELECT id, board, audience, title "
            "FROM threads "
            "WHERE discord_thread_id IS NULL OR discord_thread_id = '' "
            "ORDER BY id"
        ).fetchall()

        for thread in dark:
            try:
                ch = discord_routes.resolve_channel(
                    conn, board=thread["board"], audience=thread["audience"],
                )
            except discord_routes.UnroutableError as exc:
                summary["skipped_unroutable"].append({
                    "thread_id": thread["id"],
                    "board": thread["board"],
                    "audience": thread["audience"],
                    "title": thread["title"][:80],
                    "reason": str(exc).splitlines()[0],
                })
                continue

            if ch is None:
                summary["skipped_muted"].append({
                    "thread_id": thread["id"],
                    "board": thread["board"],
                    "audience": thread["audience"],
                    "title": thread["title"][:80],
                })
                continue

            if args.apply:
                conn.execute(
                    "UPDATE threads SET discord_thread_id=? WHERE id=?",
                    (ch, thread["id"]),
                )
            summary["linked"].append({
                "thread_id": thread["id"],
                "channel_id": ch,
                "board": thread["board"],
                "audience": thread["audience"],
                "title": thread["title"][:80],
            })

        if args.apply:
            conn.commit()
    finally:
        conn.close()

    print(json.dumps(summary, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
