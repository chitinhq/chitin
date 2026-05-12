#!/usr/bin/env python3
"""Record and answer Clawta dispatch routing decisions."""

from __future__ import annotations

import argparse
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path


def default_db_path() -> Path:
    override = os.environ.get("CLAWTA_DECISIONS_DB", "").strip()
    if override:
        return Path(override).expanduser()
    return Path.home() / ".openclaw" / "data" / "clawta_decisions.db"


def connect(db_path: Path) -> sqlite3.Connection:
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(db_path)
    conn.execute(
        """
        CREATE TABLE IF NOT EXISTS clawta_decisions (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          ticket_id TEXT NOT NULL,
          driver TEXT NOT NULL,
          model TEXT NOT NULL,
          reasoning TEXT NOT NULL,
          ts TEXT NOT NULL
        )
        """
    )
    conn.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_clawta_decisions_ticket_ts
        ON clawta_decisions(ticket_id, ts DESC)
        """
    )
    conn.commit()
    return conn


def normalize_reasoning(raw: str) -> str:
    text = " ".join(raw.strip().split())
    return text or "No routing reasoning was returned by Clawta."


def emit_chain(ticket_id: str, driver: str, model: str, reasoning: str, ts: str) -> None:
    chitin_dir = Path(os.environ.get("CHITIN_HOME", Path.home() / ".chitin")).expanduser()
    chain_id = f"clawta-routing-{ticket_id}"
    payload = {
        "ticket_id": ticket_id,
        "driver": driver,
        "model": model,
        "reasoning": reasoning,
        "ts": ts,
    }
    event = {
        "schema_version": "2",
        "run_id": chain_id,
        "session_id": chain_id,
        "surface": "clawta",
        "driver_identity": {
            "user": os.environ.get("USER", ""),
            "machine_id": "",
            "machine_fingerprint": "",
        },
        "agent_instance_id": "clawta",
        "agent_fingerprint": "clawta-routing",
        "event_type": "clawta.routing_decision",
        "chain_id": chain_id,
        "chain_type": "session",
        "ts": ts,
        "labels": {
            "agent": "clawta",
            "driver": driver,
            "model": model,
            "workflow_id": "kanban-dispatch",
        },
        "payload": payload,
    }
    event_file = ""
    try:
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", suffix=".json", delete=False) as f:
            json.dump(event, f)
            event_file = f.name
        subprocess.run(
            ["chitin-kernel", "emit", "--dir", str(chitin_dir), "--event-file", event_file],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
            timeout=10,
        )
    except Exception:
        pass
    finally:
        try:
            if event_file:
                os.unlink(event_file)
        except Exception:
            pass


def record(args: argparse.Namespace) -> int:
    reasoning = normalize_reasoning(sys.stdin.read())
    ts = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    conn = connect(Path(args.db).expanduser())
    with conn:
        conn.execute(
            """
            INSERT INTO clawta_decisions(ticket_id, driver, model, reasoning, ts)
            VALUES (?, ?, ?, ?, ?)
            """,
            (args.ticket_id, args.driver, args.model, reasoning, ts),
        )
    conn.close()
    if not args.no_chain:
        emit_chain(args.ticket_id, args.driver, args.model, reasoning, ts)
    print(f"Routing: {args.driver}/{args.model} chosen because {reasoning}")
    return 0


def latest(args: argparse.Namespace) -> int:
    conn = connect(Path(args.db).expanduser())
    params: list[str] = [args.ticket_id]
    where = "ticket_id = ?"
    if args.driver:
        where += " AND driver = ?"
        params.append(args.driver)
    row = conn.execute(
        f"""
        SELECT ticket_id, driver, model, reasoning, ts
        FROM clawta_decisions
        WHERE {where}
        ORDER BY ts DESC, id DESC
        LIMIT 1
        """,
        params,
    ).fetchone()
    conn.close()
    if row is None:
        print(f"No routing decision found for {args.ticket_id}.", file=sys.stderr)
        return 1
    data = {
        "ticket_id": row[0],
        "driver": row[1],
        "model": row[2],
        "reasoning": row[3],
        "ts": row[4],
    }
    if args.json:
        print(json.dumps(data, sort_keys=True))
    else:
        print(
            f"{data['ticket_id']} was dispatched to {data['driver']}/{data['model']} "
            f"because {data['reasoning']}"
        )
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.set_defaults(func=None)
    sub = parser.add_subparsers(dest="command", required=True)

    record_parser = sub.add_parser("record", help="record a routing decision from stdin")
    record_parser.add_argument("--db", default=str(default_db_path()))
    record_parser.add_argument("--ticket-id", required=True)
    record_parser.add_argument("--driver", required=True)
    record_parser.add_argument("--model", required=True)
    record_parser.add_argument("--no-chain", action="store_true")
    record_parser.set_defaults(func=record)

    latest_parser = sub.add_parser("latest", help="print the latest decision for a ticket")
    latest_parser.add_argument("--db", default=str(default_db_path()))
    latest_parser.add_argument("--ticket-id", required=True)
    latest_parser.add_argument("--driver", default="")
    latest_parser.add_argument("--json", action="store_true")
    latest_parser.set_defaults(func=latest)
    return parser


def main() -> int:
    args = build_parser().parse_args()
    return args.func(args)


if __name__ == "__main__":
    raise SystemExit(main())
