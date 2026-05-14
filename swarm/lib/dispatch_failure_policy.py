#!/usr/bin/env python3
"""Shared dispatch failure policy for clawta watchdog/finalize paths."""

from __future__ import annotations

import argparse
import json
import os
import re
import sqlite3
import time
from dataclasses import asdict, dataclass
from datetime import datetime
from pathlib import Path
from typing import Any


DB_PATH = Path(
    os.environ.get(
        "KANBAN_DB",
        str(Path.home() / ".hermes/kanban/boards/chitin/kanban.db"),
    )
)
RETRY_LIMIT = int(os.environ.get("CLAWTA_DISPATCH_RETRY_LIMIT", "3"))
EVENT_KIND = "dispatch_failure"
DISPATCH_PATTERNS = (
    re.compile(r"Starting dispatch to ([a-z0-9-]+)", re.I),
    re.compile(r"dispatching to ([a-z0-9-]+)", re.I),
    re.compile(r"Routed by clawta-poller: [^>]+ -> ([a-z0-9-]+)", re.I),
)


@dataclass
class DispatchFailureRecord:
    task_id: str
    failure_class: str
    reason: str
    retry_eligible: bool
    dispatch_failure_count: int
    created_at: int
    details: dict[str, Any]


@dataclass
class DispatchFailureDecision:
    action: str
    ticket_id: str
    failure_class: str
    retry_eligible: bool
    dispatch_failure_count: int
    retry_limit: int
    block_reason: str
    comment: str
    history: list[dict[str, Any]]
    recorded_failure: dict[str, Any]


def _connect(db_path: Path) -> sqlite3.Connection:
    return sqlite3.connect(db_path)


def _fetch_rows(db_path: Path, ticket_id: str) -> list[sqlite3.Row]:
    with _connect(db_path) as conn:
        conn.row_factory = sqlite3.Row
        return conn.execute(
            """
            SELECT payload, created_at
              FROM task_events
             WHERE task_id = ?
               AND kind = ?
             ORDER BY created_at ASC, id ASC
            """,
            (ticket_id, EVENT_KIND),
        ).fetchall()


def list_failures(db_path: Path, ticket_id: str) -> list[DispatchFailureRecord]:
    records: list[DispatchFailureRecord] = []
    for row in _fetch_rows(db_path, ticket_id):
        try:
            payload = json.loads(row["payload"] or "{}")
        except json.JSONDecodeError:
            payload = {}
        records.append(
            DispatchFailureRecord(
                task_id=ticket_id,
                failure_class=str(payload.get("failure_class", "unknown")),
                reason=str(payload.get("reason", "")),
                retry_eligible=bool(payload.get("retry_eligible")),
                dispatch_failure_count=int(payload.get("dispatch_failure_count", 0)),
                created_at=int(row["created_at"] or 0),
                details=dict(payload.get("details") or {}),
            )
        )
    return records


def retry_failures(db_path: Path, ticket_id: str) -> list[DispatchFailureRecord]:
    return [record for record in list_failures(db_path, ticket_id) if record.retry_eligible]


def _insert_failure(
    db_path: Path,
    *,
    ticket_id: str,
    failure_class: str,
    reason: str,
    retry_eligible: bool,
    details: dict[str, Any] | None,
    now: int | None = None,
) -> DispatchFailureRecord:
    now = int(time.time()) if now is None else now
    previous_retry_count = len(retry_failures(db_path, ticket_id))
    current_count = previous_retry_count + (1 if retry_eligible else 0)
    details = dict(details or {})
    payload = {
        "failure_class": failure_class,
        "reason": reason,
        "retry_eligible": retry_eligible,
        "dispatch_failure_count": current_count,
        "details": details,
    }
    with _connect(db_path) as conn:
        conn.execute(
            """
            INSERT INTO task_events(task_id, kind, payload, created_at)
            VALUES (?, ?, ?, ?)
            """,
            (ticket_id, EVENT_KIND, json.dumps(payload, sort_keys=True), now),
        )
        conn.commit()
    return DispatchFailureRecord(
        task_id=ticket_id,
        failure_class=failure_class,
        reason=reason,
        retry_eligible=retry_eligible,
        dispatch_failure_count=current_count,
        created_at=now,
        details=details,
    )


def _load_dispatch_comments(db_path: Path, ticket_id: str) -> list[tuple[int, str, str]]:
    try:
        with _connect(db_path) as conn:
            rows = conn.execute(
                """
                SELECT created_at, body
                  FROM task_comments
                 WHERE task_id = ?
                 ORDER BY created_at ASC, id ASC
                """,
                (ticket_id,),
            ).fetchall()
    except sqlite3.OperationalError:
        return []
    comments: list[tuple[int, str, str]] = []
    for created_at, body in rows:
        text = str(body or "")
        driver = ""
        for pattern in DISPATCH_PATTERNS:
            match = pattern.search(text)
            if match:
                driver = match.group(1).lower()
                comments.append((int(created_at or 0), driver, text))
                break
    return comments


def _history_driver(comments: list[tuple[int, str, str]], record: DispatchFailureRecord) -> tuple[str, int]:
    chosen_driver = str(record.details.get("assignee") or "unknown").lower()
    chosen_at = record.created_at
    for created_at, driver, _ in comments:
        if created_at <= record.created_at:
            chosen_driver = driver
            chosen_at = created_at
        else:
            break
    return chosen_driver or "unknown", chosen_at


def _history_summary(record: DispatchFailureRecord) -> str:
    details = record.details
    if record.failure_class == "silent_worker_death":
        quiet = details.get("quiet_seconds")
        return f"silent at {quiet}s" if quiet is not None else "silent"
    if record.failure_class == "empty_branch":
        branch = details.get("branch") or "unknown"
        return f"empty branch ({branch})"
    if record.failure_class == "gh_pr_create_failed":
        branch = details.get("branch") or "unknown"
        return f"gh pr create failed ({branch})"
    if record.failure_class == "worker_nonzero_exit":
        return f"worker failed ({record.reason})"
    return record.reason or record.failure_class.replace("_", " ")


def format_history(db_path: Path, ticket_id: str, records: list[DispatchFailureRecord]) -> list[dict[str, Any]]:
    comments = _load_dispatch_comments(db_path, ticket_id)
    history: list[dict[str, Any]] = []
    for record in records:
        driver, started_at = _history_driver(comments, record)
        history.append(
            {
                "time": datetime.fromtimestamp(started_at).strftime("%H:%M"),
                "driver": driver,
                "summary": _history_summary(record),
                "failure_class": record.failure_class,
                "created_at": record.created_at,
            }
        )
    return history


def _retry_comment(record: DispatchFailureRecord, retry_limit: int) -> str:
    count = record.dispatch_failure_count
    if record.failure_class == "silent_worker_death":
        return (
            f"Watchdog auto-retry {count}/{retry_limit}: silent worker death. "
            f"Resetting to ready for a fresh dispatch. {record.reason}"
        )
    return (
        f"Retry-eligible dispatch failure {count}/{retry_limit}: "
        f"{record.reason}. Resetting to ready for a fresh dispatch."
    )


def _escalation_reason(records: list[DispatchFailureRecord]) -> str:
    count = len(records)
    if records and all(record.failure_class == "silent_worker_death" for record in records):
        return f"{count} silent worker deaths; the ticket spec or the swarm infra needs operator review"
    return f"{count} retry-eligible dispatch failures; the ticket spec or the swarm infra needs operator review"


def _escalation_comment(
    db_path: Path,
    ticket_id: str,
    records: list[DispatchFailureRecord],
    retry_limit: int,
) -> str:
    history = format_history(db_path, ticket_id, records)
    if records and all(record.failure_class == "silent_worker_death" for record in records):
        header = f"Watchdog: ticket bounced {retry_limit}x with silent worker death."
    else:
        header = f"Dispatch watchdog: ticket bounced {retry_limit}x on retry-eligible infrastructure failures."
    lines = [header, "Dispatch history:"]
    lines.extend(f"  {item['time']} {item['driver']} -> {item['summary']}" for item in history)
    lines.append(
        "Recommend: (a) check copilot/codex wrapper health, or (b) ticket is too vague for the current model and needs re-spec."
    )
    return "\n".join(lines)


def plan_retryable_failure(
    db_path: Path,
    *,
    ticket_id: str,
    failure_class: str,
    reason: str,
    details: dict[str, Any] | None = None,
    retry_limit: int = RETRY_LIMIT,
    now: int | None = None,
) -> DispatchFailureDecision:
    recorded = _insert_failure(
        db_path,
        ticket_id=ticket_id,
        failure_class=failure_class,
        reason=reason,
        retry_eligible=True,
        details=details,
        now=now,
    )
    retry_history = retry_failures(db_path, ticket_id)
    if recorded.dispatch_failure_count < retry_limit:
        history = format_history(db_path, ticket_id, retry_history)
        return DispatchFailureDecision(
            action="retry",
            ticket_id=ticket_id,
            failure_class=failure_class,
            retry_eligible=True,
            dispatch_failure_count=recorded.dispatch_failure_count,
            retry_limit=retry_limit,
            block_reason="",
            comment=_retry_comment(recorded, retry_limit),
            history=history,
            recorded_failure=asdict(recorded),
        )
    history = format_history(db_path, ticket_id, retry_history)
    return DispatchFailureDecision(
        action="escalate",
        ticket_id=ticket_id,
        failure_class=failure_class,
        retry_eligible=True,
        dispatch_failure_count=recorded.dispatch_failure_count,
        retry_limit=retry_limit,
        block_reason=_escalation_reason(retry_history),
        comment=_escalation_comment(db_path, ticket_id, retry_history, retry_limit),
        history=history,
        recorded_failure=asdict(recorded),
    )


def plan_explicit_failure(
    db_path: Path,
    *,
    ticket_id: str,
    failure_class: str,
    reason: str,
    details: dict[str, Any] | None = None,
    retry_limit: int = RETRY_LIMIT,
    now: int | None = None,
) -> DispatchFailureDecision:
    recorded = _insert_failure(
        db_path,
        ticket_id=ticket_id,
        failure_class=failure_class,
        reason=reason,
        retry_eligible=False,
        details=details,
        now=now,
    )
    history = format_history(db_path, ticket_id, retry_failures(db_path, ticket_id))
    return DispatchFailureDecision(
        action="escalate",
        ticket_id=ticket_id,
        failure_class=failure_class,
        retry_eligible=False,
        dispatch_failure_count=len(retry_failures(db_path, ticket_id)),
        retry_limit=retry_limit,
        block_reason=reason,
        comment=reason,
        history=history,
        recorded_failure=asdict(recorded),
    )


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("retryable", "explicit"))
    parser.add_argument("--ticket-id", required=True)
    parser.add_argument("--failure-class", required=True)
    parser.add_argument("--reason", required=True)
    parser.add_argument("--details-json", default="{}")
    parser.add_argument("--db-path", default=str(DB_PATH))
    parser.add_argument("--retry-limit", type=int, default=RETRY_LIMIT)
    return parser.parse_args()


def main() -> int:
    args = _parse_args()
    try:
        details = json.loads(args.details_json or "{}")
    except json.JSONDecodeError as exc:
        raise SystemExit(f"invalid --details-json: {exc}")
    db_path = Path(args.db_path)
    if args.mode == "retryable":
        decision = plan_retryable_failure(
            db_path,
            ticket_id=args.ticket_id,
            failure_class=args.failure_class,
            reason=args.reason,
            details=details,
            retry_limit=args.retry_limit,
        )
    else:
        decision = plan_explicit_failure(
            db_path,
            ticket_id=args.ticket_id,
            failure_class=args.failure_class,
            reason=args.reason,
            details=details,
            retry_limit=args.retry_limit,
        )
    print(json.dumps(asdict(decision), sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
