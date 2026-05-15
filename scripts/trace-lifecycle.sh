#!/usr/bin/env bash
set -euo pipefail

DB="${KANBAN_DB:-$HOME/.hermes/kanban/boards/chitin/kanban.db}"
CHITIN_HOME_DIR="${CHITIN_HOME:-$HOME/.chitin}"

die() {
  echo "trace-lifecycle: $*" >&2
  exit 2
}

ticket_id="${1:-}"
[[ -n "$ticket_id" ]] || die "usage: trace-lifecycle.sh <ticket_id>"
[[ "$ticket_id" =~ ^t_[a-f0-9]+$ ]] || die "expected ticket id (t_XXXXXXXX), got: ${ticket_id}"

python3 - "$DB" "$CHITIN_HOME_DIR" "$ticket_id" <<'PY'
from __future__ import annotations

import json
import sqlite3
import sys
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


DB_PATH = Path(sys.argv[1]).expanduser()
CHITIN_HOME = Path(sys.argv[2]).expanduser()
TICKET_ID = sys.argv[3]


@dataclass
class ChainRecord:
    path: Path
    run_key: str
    last_hash: str | None
    events: list[dict[str, Any]]


def parse_iso(ts: str | None) -> datetime | None:
    if not ts:
        return None
    try:
        return datetime.fromisoformat(ts.replace("Z", "+00:00"))
    except ValueError:
        return None


def fmt_epoch(ts: Any) -> str:
    if ts in (None, ""):
        return "—"
    try:
        return datetime.fromtimestamp(int(ts), tz=timezone.utc).astimezone().isoformat(timespec="seconds")
    except (TypeError, ValueError, OSError):
        return str(ts)


def fmt_hash(value: str | None) -> str:
    if not value:
        return "—"
    return value[:12]


def has_table(conn: sqlite3.Connection, table: str) -> bool:
    row = conn.execute(
        "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?",
        (table,),
    ).fetchone()
    return row is not None


def has_column(conn: sqlite3.Connection, table: str, column: str) -> bool:
    try:
        rows = conn.execute(f"PRAGMA table_info({table})").fetchall()
    except sqlite3.OperationalError:
        return False
    return any(row[1] == column for row in rows)


def load_chain_index(root: Path) -> tuple[dict[str, ChainRecord], dict[str, ChainRecord]]:
    by_run: dict[str, ChainRecord] = {}
    by_hash: dict[str, ChainRecord] = {}
    if not root.is_dir():
        return by_run, by_hash

    for path in sorted(root.glob("events-*.jsonl")):
        run_key = path.name[len("events-") : -len(".jsonl")]
        events: list[dict[str, Any]] = []
        last_hash: str | None = None
        try:
            for raw in path.read_text().splitlines():
                line = raw.strip()
                if not line:
                    continue
                try:
                    payload = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if isinstance(payload, dict):
                    if payload.get("this_hash"):
                        last_hash = str(payload["this_hash"])
                    events.append(payload)
        except OSError:
            continue

        record = ChainRecord(path=path, run_key=run_key, last_hash=last_hash, events=events)
        by_run[run_key] = record
        if last_hash:
            by_hash[last_hash] = record
    return by_run, by_hash


def normalize_run(row: sqlite3.Row) -> dict[str, Any]:
    data = dict(row)
    data["run_ref"] = str(data.get("run_id") or data.get("id"))
    return data


def chain_event_summary(event: dict[str, Any]) -> str:
    event_type = str(event.get("event_type") or event.get("kind") or "unknown")
    seq = event.get("seq")
    prev_hash = event.get("prev_hash")
    return (
        f"{event_type}"
        f" seq={seq if seq is not None else '?'}"
        f" hash={fmt_hash(str(event.get('this_hash') or ''))}"
        f" prev={fmt_hash(str(prev_hash or ''))}"
    )


def kanban_event_summary(event: sqlite3.Row) -> str:
    payload_raw = event["payload"] or ""
    try:
        payload = json.loads(payload_raw) if payload_raw else {}
    except json.JSONDecodeError:
        payload = {}

    if event["kind"] == "status_transition":
        extra = payload.get("extra") or {}
        extra_suffix = ""
        if isinstance(extra, dict) and extra:
            compact = ", ".join(f"{k}={v}" for k, v in sorted(extra.items()))
            extra_suffix = f" extra[{compact}]"
        return (
            f"{payload.get('from', '?')} -> {payload.get('to', '?')}"
            f" by {payload.get('by', '?')}{extra_suffix}"
        )

    if event["kind"] == "pr_opened":
        return f"PR opened by {payload.get('by', '?')} url={payload.get('pr_url', '?')}"

    return f"{event['kind']} payload={payload_raw}"


conn = sqlite3.connect(DB_PATH)
conn.row_factory = sqlite3.Row

if not has_table(conn, "tasks"):
    print(f"trace-lifecycle: tasks table not found in {DB_PATH}", file=sys.stderr)
    raise SystemExit(2)

task = conn.execute(
    "SELECT id, title, status, created_at, started_at, completed_at FROM tasks WHERE id=?",
    (TICKET_ID,),
).fetchone()
if task is None:
    print(f"trace-lifecycle: ticket not found: {TICKET_ID}", file=sys.stderr)
    raise SystemExit(2)

if not has_table(conn, "task_events"):
    print(f"trace-lifecycle: task_events table not found in {DB_PATH}", file=sys.stderr)
    raise SystemExit(2)

run_id_column = has_column(conn, "task_runs", "run_id")
event_run_id_column = has_column(conn, "task_events", "run_id")

event_columns = "id, task_id, kind, payload, created_at"
if event_run_id_column:
    event_columns += ", run_id"
events = conn.execute(
    f"SELECT {event_columns} FROM task_events WHERE task_id=? ORDER BY created_at, id",
    (TICKET_ID,),
).fetchall()

runs: list[dict[str, Any]] = []
if has_table(conn, "task_runs"):
    run_columns = [
        "id",
        "task_id",
        "status",
        "started_at",
        "ended_at",
        "outcome",
        "summary",
        "error",
        "driver_id",
        "repo_sha",
        "lease_id",
        "event_chain_hash",
    ]
    if run_id_column:
        run_columns.append("run_id")
    run_rows = conn.execute(
        f"SELECT {', '.join(run_columns)} FROM task_runs WHERE task_id=? ORDER BY started_at, id",
        (TICKET_ID,),
    ).fetchall()
    runs = [normalize_run(row) for row in run_rows]

conn.close()

chain_by_run, chain_by_hash = load_chain_index(CHITIN_HOME)
warnings: list[str] = []
gaps: list[str] = []
timeline: list[tuple[float, int, str]] = []

for event in events:
    ts = float(event["created_at"] or 0)
    run_marker = ""
    if event_run_id_column and event["run_id"] not in (None, ""):
        run_marker = f" run={event['run_id']}"
    timeline.append((ts, 0, f"KANBAN {fmt_epoch(event['created_at'])} {kanban_event_summary(event)}{run_marker}"))

for run in runs:
    start_ts = float(run.get("started_at") or 0)
    run_ref = run["run_ref"]
    timeline.append(
        (
            start_ts,
            1,
            "RUN    "
            + f"{fmt_epoch(run.get('started_at'))} "
            + f"run={run_ref} started status={run.get('status') or '?'} "
            + f"chain_hash={fmt_hash(run.get('event_chain_hash'))}",
        )
    )

    chain_record: ChainRecord | None = None
    if run_id_column and run.get("run_id"):
        chain_record = chain_by_run.get(str(run["run_id"]))
        if chain_record is None:
            warnings.append(
                f"run {run_ref}: missing direct chain file {CHITIN_HOME / ('events-' + str(run['run_id']) + '.jsonl')}"
            )
    if chain_record is None and run.get("event_chain_hash"):
        chain_record = chain_by_hash.get(str(run["event_chain_hash"]))

    if chain_record is None:
        if run.get("event_chain_hash"):
            warnings.append(
                f"run {run_ref}: no chain file matched terminal hash {fmt_hash(run.get('event_chain_hash'))}"
            )
        else:
            warnings.append(f"run {run_ref}: no terminal event_chain_hash recorded; kanban-only timeline")
    else:
        if run.get("event_chain_hash") and chain_record.last_hash != run.get("event_chain_hash"):
            gaps.append(
                f"run {run_ref}: task_runs.event_chain_hash={fmt_hash(run.get('event_chain_hash'))} "
                f"but chain file ended at {fmt_hash(chain_record.last_hash)}"
            )
        timeline.append(
            (
                start_ts,
                2,
                f"CHAIN  {fmt_epoch(run.get('started_at'))} run={run_ref} file={chain_record.path}",
            )
        )
        for payload in chain_record.events:
            iso = parse_iso(str(payload.get("ts") or ""))
            sort_ts = iso.timestamp() if iso else start_ts
            timeline.append((sort_ts, 3, f"CHAIN  {payload.get('ts', '—')} run={run_ref} {chain_event_summary(payload)}"))

    if run.get("ended_at") not in (None, ""):
        timeline.append(
            (
                float(run["ended_at"]),
                4,
                "RUN    "
                + f"{fmt_epoch(run.get('ended_at'))} "
                + f"run={run_ref} finished status={run.get('status') or '?'} "
                + f"outcome={run.get('outcome') or '—'} "
                + f"chain_hash={fmt_hash(run.get('event_chain_hash'))}",
            )
        )

print(f"ticket: {task['id']}  status={task['status']}  title={task['title']}")
print(f"db: {DB_PATH}")
print(f"chitin_home: {CHITIN_HOME}")
print(f"task_runs: {len(runs)}  task_events: {len(events)}")
if warnings:
    for warning in warnings:
        print(f"WARNING: {warning}")
if gaps:
    for gap in gaps:
        print(f"GAP: {gap}")
print("timeline:")
for _, _, line in sorted(timeline, key=lambda item: (item[0], item[1], item[2])):
    print(f"  {line}")
print(f"summary: warnings={len(warnings)} gaps={len(gaps)}")
PY
