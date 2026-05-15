#!/usr/bin/env python3
"""Cron-driven post-PR judge backfill for merged swarm PRs."""

from __future__ import annotations

import argparse
import json
import os
import re
import sqlite3
import subprocess
import sys
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Iterable

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

import _swarm_elo as elo  # noqa: E402

LOG_PATH = Path(os.path.expanduser("~/.openclaw/logs/judge_backfill.log"))
TICKET_RE = re.compile(r"t_[a-f0-9]{8}")
DISPATCH_META_RE = re.compile(
    r"\bdispatch\s+driver=(?P<driver>\S+)\s+model=(?P<model>\S+)\s+role=(?P<role>\S+)",
    re.IGNORECASE,
)
DEFAULT_DRIVER = "clawta"
DEFAULT_MODEL = "gpt-5.5"
DEFAULT_ROLE = "programmer"


@dataclass(frozen=True)
class DispatchMeta:
    driver: str
    model: str
    role: str
    inferred: bool = False


def log(message: str, *, log_path: Path = LOG_PATH) -> None:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    ts = datetime.now(timezone.utc).isoformat(timespec="seconds")
    line = f"{ts} {message}"
    with log_path.open("a", encoding="utf-8") as fh:
        fh.write(line + "\n")
    print(line)


def cutoff_iso(hours: int) -> str:
    return (datetime.now(timezone.utc) - timedelta(hours=hours)).isoformat(timespec="seconds").replace("+00:00", "Z")


def query_merged_prs(hours: int = 48) -> list[dict]:
    search = f"merged:>={cutoff_iso(hours)}"
    result = subprocess.run(
        [
            "gh", "pr", "list",
            "--repo", "chitinhq/chitin",
            "--state", "merged",
            "--search", search,
            "--json", "number,url,headRefName,body,title,mergedAt",
            "--limit", "100",
        ],
        capture_output=True,
        text=True,
        timeout=45,
    )
    if result.returncode != 0:
        raise RuntimeError(f"gh pr list failed rc={result.returncode}: {result.stderr.strip()[:300]}")
    return json.loads(result.stdout or "[]")


def ticket_from_pr(pr: dict) -> str | None:
    parts = [
        str(pr.get("body") or ""),
        str(pr.get("headRefName") or ""),
        str(pr.get("title") or ""),
    ]
    for text in parts:
        match = TICKET_RE.search(text)
        if match:
            return match.group(0)
    return None


def score_exists(conn: sqlite3.Connection, pr_url: str) -> bool:
    row = conn.execute(
        "SELECT 1 FROM swarm_dispatch_scores WHERE pr_url = ? LIMIT 1",
        (pr_url,),
    ).fetchone()
    return row is not None


def _comment_bodies(ticket_json: dict) -> Iterable[str]:
    candidates = []
    if isinstance(ticket_json, dict):
        candidates.extend(ticket_json.get("comments") or [])
        candidates.extend(ticket_json.get("task_comments") or [])
        task = ticket_json.get("task")
        if isinstance(task, dict):
            candidates.extend(task.get("comments") or [])
    for item in candidates:
        if isinstance(item, str):
            yield item
        elif isinstance(item, dict):
            body = item.get("body") or item.get("text") or item.get("comment")
            if body:
                yield str(body)


def parse_dispatch_meta_from_comments(ticket_json: dict) -> DispatchMeta | None:
    # Last matching comment wins so retries can update model/role.
    found: DispatchMeta | None = None
    for body in _comment_bodies(ticket_json):
        match = DISPATCH_META_RE.search(body)
        if match:
            found = DispatchMeta(
                driver=match.group("driver"),
                model=match.group("model"),
                role=match.group("role"),
                inferred=False,
            )
    return found


def fetch_ticket_json(ticket_id: str) -> dict:
    result = subprocess.run(
        ["hermes", "kanban", "--board", "chitin", "show", ticket_id, "--json"],
        capture_output=True,
        text=True,
        timeout=20,
    )
    if result.returncode != 0:
        raise RuntimeError(f"hermes show {ticket_id} failed rc={result.returncode}: {result.stderr.strip()[:200]}")
    return json.loads(result.stdout or "{}")


def recover_dispatch_meta(ticket_id: str) -> DispatchMeta:
    try:
        meta = parse_dispatch_meta_from_comments(fetch_ticket_json(ticket_id))
        if meta:
            return meta
        log(f"{ticket_id}: dispatch metadata absent; using inferred default")
    except Exception as exc:  # noqa: BLE001 - log and fall back for backfill robustness
        log(f"{ticket_id}: metadata recovery failed: {exc}; using inferred default")
    return DispatchMeta(DEFAULT_DRIVER, DEFAULT_MODEL, DEFAULT_ROLE, inferred=True)


def run_judge(ticket_id: str, pr_url: str, meta: DispatchMeta, *, dry_run: bool = False) -> subprocess.CompletedProcess[str]:
    cmd = [
        sys.executable,
        str(SCRIPT_DIR / "judge.py"),
        "--ticket", ticket_id,
        "--pr-url", pr_url,
        "--driver", meta.driver,
        "--model", meta.model,
        "--role", meta.role,
    ]
    if meta.inferred:
        cmd.append("--inferred")
    if dry_run:
        cmd.append("--dry-run")
    return subprocess.run(cmd, capture_output=True, text=True, timeout=360)


def process_prs(prs: list[dict], *, conn: sqlite3.Connection, dry_run: bool = False) -> dict:
    stats = {"considered": 0, "scored": 0, "skipped_no_ticket": 0, "skipped_existing": 0, "failed": 0}
    for pr in prs:
        stats["considered"] += 1
        pr_url = str(pr.get("url") or "")
        pr_num = pr.get("number") or pr_url.rsplit("/", 1)[-1]
        ticket_id = ticket_from_pr(pr)
        if not ticket_id:
            stats["skipped_no_ticket"] += 1
            log(f"PR #{pr_num}: skip no ticket id")
            continue
        if pr_url and score_exists(conn, pr_url):
            stats["skipped_existing"] += 1
            log(f"PR #{pr_num} {ticket_id}: skip already scored")
            continue
        meta = recover_dispatch_meta(ticket_id)
        try:
            result = run_judge(ticket_id, pr_url, meta, dry_run=dry_run)
        except Exception as exc:  # noqa: BLE001
            stats["failed"] += 1
            log(f"PR #{pr_num} {ticket_id}: judge failed to start: {exc}")
            continue
        if result.returncode == 0:
            stats["scored"] += 1
            log(f"PR #{pr_num} {ticket_id}: scored driver={meta.driver} model={meta.model} role={meta.role} inferred={int(meta.inferred)}")
            if result.stdout.strip():
                log(f"PR #{pr_num} judge stdout: {result.stdout.strip()[:500]}")
        else:
            stats["failed"] += 1
            log(f"PR #{pr_num} {ticket_id}: judge rc={result.returncode} stderr={result.stderr.strip()[:500]}")
    return stats


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--hours", type=int, default=48, help="Look back this many hours for merged PRs")
    ap.add_argument("--limit", type=int, default=0, help="Optional max PRs to consider this run (0 = no limit)")
    ap.add_argument("--dry-run", action="store_true", help="Run discovery/judge prompt only; do not write scores")
    args = ap.parse_args()

    conn = elo.open_db()
    try:
        prs = query_merged_prs(args.hours)
        if args.limit > 0:
            prs = prs[: args.limit]
        stats = process_prs(prs, conn=conn, dry_run=args.dry_run)
        log("summary " + json.dumps(stats, sort_keys=True))
        return 0 if stats["failed"] == 0 else 1
    except Exception as exc:  # noqa: BLE001
        log(f"fatal: {exc}")
        return 2
    finally:
        conn.close()


if __name__ == "__main__":
    raise SystemExit(main())
