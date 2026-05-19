#!/usr/bin/env python3
"""Idempotent seeder for the discord_routes table.

Operator runs once per box (or whenever the canonical mapping changes).
The seed below matches the post-2026-05-19 agreement:

  - board=chitin       → #swarm channel
  - audience=clawta    → #clawta channel
  - audience=hermes    → #hermes channel
  - global default '*' → #swarm channel

The script:
  - reads DISCORD_WEBHOOK_URL_<channel_id> env vars / ~/.hermes/.env to
    discover which channel IDs have webhooks configured;
  - inserts (or updates) the matching routes;
  - is idempotent: re-runs replace the same rows by primary key.

Run:
  python3 services/agent-bus/seed_discord_routes.py --apply
  python3 services/agent-bus/seed_discord_routes.py            # dry-run

Output is JSON for easy machine consumption.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path


# Canonical mapping. Channel IDs are public Discord identifiers — safe to
# commit; the SECRETS are the webhook URLs which live in ~/.hermes/.env.
SWARM_CHANNEL_ID = "1505613628286701588"     # #swarm
CLAWTA_CHANNEL_ID = "1503439202719760405"    # #clawta
HERMES_CHANNEL_ID = "1503438297597350062"    # #hermes


CANONICAL_SEED = [
    # (scope, key, channel_id, priority)
    ("board", "chitin", SWARM_CHANNEL_ID, 100),
    ("audience", "clawta", CLAWTA_CHANNEL_ID, 100),
    ("audience", "hermes", HERMES_CHANNEL_ID, 100),
    ("global", "*", SWARM_CHANNEL_ID, 100),
]


def _load_dotenv(path: Path) -> dict[str, str]:
    if not path.is_file():
        return {}
    out: dict[str, str] = {}
    for line in path.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, _, v = line.partition("=")
        out[k.strip()] = v.strip().strip('"').strip("'")
    return out


def _has_webhook(channel_id: str, env: dict[str, str]) -> bool:
    key = f"DISCORD_WEBHOOK_URL_{channel_id}"
    return bool(os.environ.get(key) or env.get(key))


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--apply", action="store_true",
                   help="apply changes (default is dry-run)")
    p.add_argument("--env-file", default=str(Path.home() / ".hermes" / ".env"),
                   help="dotenv path to check webhook coverage (~/.hermes/.env default)")
    p.add_argument("--db", default=None,
                   help="agent-bus DB path (default: $AGENT_BUS_DB or ~/.chitin/agent-bus/bus.db)")
    args = p.parse_args(argv)

    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import db as bus_db          # noqa: E402
    import discord_routes        # noqa: E402

    db_path = Path(args.db) if args.db else bus_db.db_path()
    file_env = _load_dotenv(Path(args.env_file).expanduser())

    summary = {
        "db": str(db_path),
        "env_file": args.env_file,
        "applied": args.apply,
        "routes": [],
        "warnings": [],
    }

    conn = bus_db.connect(db_path)
    try:
        existing = {(r.scope, r.key): r for r in discord_routes.list_routes(conn)}

        for scope, key, channel_id, priority in CANONICAL_SEED:
            cur = existing.get((scope, key))
            action = "create"
            if cur is not None:
                if (cur.channel_id == channel_id and cur.priority == priority):
                    action = "no-change"
                else:
                    action = "update"

            if not _has_webhook(channel_id, file_env):
                summary["warnings"].append(
                    f"webhook NOT configured for channel {channel_id} "
                    f"(missing DISCORD_WEBHOOK_URL_{channel_id} in env/dotenv); "
                    f"route will be inserted but pushes will fall back to bot token "
                    f"(or fail silently if bot token missing)"
                )

            if args.apply and action != "no-change":
                discord_routes.set_route(
                    conn, scope=scope, key=key,
                    channel_id=channel_id, priority=priority,
                )

            summary["routes"].append({
                "scope": scope, "key": key, "channel_id": channel_id,
                "priority": priority, "action": action,
            })
    finally:
        conn.close()

    print(json.dumps(summary, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
