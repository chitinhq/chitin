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
          shape_bucket TEXT NOT NULL DEFAULT '',
          selection_mode TEXT NOT NULL DEFAULT 'exploitation',
          reasoning TEXT NOT NULL,
          outcome TEXT NOT NULL DEFAULT 'pending',
          failure_kind TEXT NOT NULL DEFAULT '',
          outcome_ts TEXT,
          ts TEXT NOT NULL
        )
        """
    )
    columns = {
        row[1] for row in conn.execute("PRAGMA table_info(clawta_decisions)")
    }
    if "shape_bucket" not in columns:
        conn.execute(
            """
            ALTER TABLE clawta_decisions
            ADD COLUMN shape_bucket TEXT NOT NULL DEFAULT ''
            """
        )
    if "selection_mode" not in columns:
        conn.execute(
            """
            ALTER TABLE clawta_decisions
            ADD COLUMN selection_mode TEXT NOT NULL DEFAULT 'exploitation'
            """
        )
    if "outcome" not in columns:
        conn.execute(
            """
            ALTER TABLE clawta_decisions
            ADD COLUMN outcome TEXT NOT NULL DEFAULT 'pending'
            """
        )
    if "failure_kind" not in columns:
        conn.execute(
            """
            ALTER TABLE clawta_decisions
            ADD COLUMN failure_kind TEXT NOT NULL DEFAULT ''
            """
        )
    if "outcome_ts" not in columns:
        conn.execute(
            """
            ALTER TABLE clawta_decisions
            ADD COLUMN outcome_ts TEXT
            """
        )
    conn.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_clawta_decisions_ticket_ts
        ON clawta_decisions(ticket_id, ts DESC)
        """
    )
    conn.execute(
        """
        CREATE INDEX IF NOT EXISTS idx_clawta_decisions_shape_driver_outcome
        ON clawta_decisions(shape_bucket, driver, outcome, ts DESC)
        """
    )
    conn.commit()
    return conn


def normalize_reasoning(raw: str) -> str:
    text = " ".join(raw.strip().split())
    return text or "No routing reasoning was returned by Clawta."


def emit_chain(
    ticket_id: str, driver: str, model: str, selection_mode: str, reasoning: str, ts: str
) -> None:
    chitin_dir = Path(os.environ.get("CHITIN_HOME", Path.home() / ".chitin")).expanduser()
    chain_id = f"clawta-routing-{ticket_id}"
    payload = {
        "ticket_id": ticket_id,
        "driver": driver,
        "model": model,
        "selection_mode": selection_mode,
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
            "selection_mode": selection_mode,
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
    selection_mode = args.selection_mode
    ts = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    conn = connect(Path(args.db).expanduser())
    with conn:
        conn.execute(
            """
            INSERT INTO clawta_decisions(
                ticket_id, driver, model, shape_bucket, selection_mode, reasoning, outcome, ts
            )
            VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
            """,
            (
                args.ticket_id,
                args.driver,
                args.model,
                args.shape_bucket,
                selection_mode,
                reasoning,
                ts,
            ),
        )
    conn.close()
    if not args.no_chain:
        emit_chain(args.ticket_id, args.driver, args.model, selection_mode, reasoning, ts)
    print(
        f"Routing ({selection_mode}): {args.driver}/{args.model} chosen because {reasoning}"
    )
    return 0


def mark_outcome(args: argparse.Namespace) -> int:
    conn = connect(Path(args.db).expanduser())
    where = "ticket_id = ?"
    params: list[str] = [args.ticket_id]
    if args.driver:
        where += " AND driver = ?"
        params.append(args.driver)
    row = conn.execute(
        f"""
        SELECT id
        FROM clawta_decisions
        WHERE {where}
        ORDER BY ts DESC, id DESC
        LIMIT 1
        """,
        params,
    ).fetchone()
    if row is None:
        conn.close()
        print(f"No routing decision found for {args.ticket_id}.", file=sys.stderr)
        return 1

    outcome_ts = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    with conn:
        conn.execute(
            """
            UPDATE clawta_decisions
            SET outcome = ?, failure_kind = ?, outcome_ts = ?
            WHERE id = ?
            """,
            (args.outcome, args.failure_kind, outcome_ts, row[0]),
        )
    conn.close()
    print(
        f"Routing outcome for {args.ticket_id}: outcome={args.outcome} "
        f"failure_kind={args.failure_kind or 'none'}"
    )
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
        SELECT ticket_id, driver, model, shape_bucket, selection_mode, reasoning,
               outcome, failure_kind, outcome_ts, ts
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
        "shape_bucket": row[3],
        "selection_mode": row[4],
        "reasoning": row[5],
        "outcome": row[6],
        "failure_kind": row[7],
        "outcome_ts": row[8],
        "ts": row[9],
    }
    if args.json:
        print(json.dumps(data, sort_keys=True))
    else:
        print(
            f"{data['ticket_id']} was dispatched to {data['driver']}/{data['model']} "
            f"via {data['selection_mode']} because {data['reasoning']}"
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
    record_parser.add_argument("--shape-bucket", default="")
    record_parser.add_argument(
        "--selection-mode",
        choices=("exploration", "exploitation"),
        default="exploitation",
    )
    record_parser.add_argument("--no-chain", action="store_true")
    record_parser.set_defaults(func=record)

    outcome_parser = sub.add_parser("mark-outcome", help="annotate the latest routing decision outcome")
    outcome_parser.add_argument("--db", default=str(default_db_path()))
    outcome_parser.add_argument("--ticket-id", required=True)
    outcome_parser.add_argument("--driver", default="")
    outcome_parser.add_argument(
        "--outcome",
        choices=("pending", "success", "failure"),
        required=True,
    )
    outcome_parser.add_argument(
        "--failure-kind",
        default="",
        choices=("", "empty_branch", "gh_pr_create_fail", "ci_fail", "request_changes_timeout"),
    )
    outcome_parser.set_defaults(func=mark_outcome)

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
