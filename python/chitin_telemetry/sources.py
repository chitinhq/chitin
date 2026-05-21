"""Read-only Argus source pollers for kanban and git/GitHub history."""
from __future__ import annotations

import json
import os
import re
import sqlite3
import subprocess
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

from chitin_telemetry.indexer import insert_event


KANBAN_POLL_SECONDS = 300
TICKET_RE = re.compile(r"\b(t_[0-9a-f]{8})\b")
DRIVER_RE = re.compile(r"^swarm/([a-z0-9_-]+)-")


def _now_ts() -> int:
    return int(time.time())


def _to_iso(ts_unix: int) -> str:
    return datetime.fromtimestamp(ts_unix, timezone.utc).isoformat()


def _parse_iso(value: str | None) -> int | None:
    if not value:
        return None
    try:
        return int(datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp())
    except ValueError:
        return None


def _state_get(conn: sqlite3.Connection, key: str, default: str = "0") -> str:
    row = conn.execute("SELECT value FROM kernel_state WHERE key = ?", (key,)).fetchone()
    if not row:
        return default
    return str(row["value"] if isinstance(row, sqlite3.Row) else row[0])


def _state_set(conn: sqlite3.Connection, key: str, value: str) -> None:
    now = _now_ts()
    conn.execute(
        """
        INSERT INTO kernel_state (key, value, updated_ts)
        VALUES (?, ?, ?)
        ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_ts = excluded.updated_ts
        """,
        (key, value, now),
    )
    conn.commit()


def _open_kanban_readonly(db_path: Path, retry_attempts: int, sleep_s: float) -> sqlite3.Connection:
    last_err: Exception | None = None
    for attempt in range(retry_attempts):
        try:
            conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
            conn.row_factory = sqlite3.Row
            conn.execute("PRAGMA query_only = ON")
            return conn
        except sqlite3.OperationalError as e:
            last_err = e
            if "locked" not in str(e).lower() and "busy" not in str(e).lower():
                raise
            if attempt + 1 < retry_attempts:
                time.sleep(sleep_s * (attempt + 1))
    assert last_err is not None
    raise last_err


def discover_kanban_dbs(boards_root: Path | None = None) -> list[Path]:
    root = boards_root or (Path.home() / ".hermes" / "kanban" / "boards")
    if not root.exists():
        return []
    return sorted(p for p in root.glob("*/kanban.db") if p.is_file())


def ingest_kanban_sources(
    conn: sqlite3.Connection,
    boards_root: Path | None = None,
    *,
    retry_attempts: int = 3,
    retry_sleep_s: float = 0.25,
) -> dict[str, int]:
    """Index Hermes kanban DB snapshots as read-only source events."""
    stats = {"tickets": 0, "transitions": 0, "comments": 0}
    for db_path in discover_kanban_dbs(boards_root):
        board = db_path.parent.name
        state_key = f"kanban:{db_path}:last_seen_ts"
        last_seen = int(_state_get(conn, state_key, "0"))
        max_seen = last_seen

        src = _open_kanban_readonly(db_path, retry_attempts, retry_sleep_s)
        try:
            ticket_rows = src.execute(
                """
                SELECT id, title, body, status, assignee, priority, created_at
                  FROM tasks
                 WHERE created_at > ?
                 ORDER BY created_at ASC, id ASC
                """,
                (last_seen,),
            ).fetchall()
            for row in ticket_rows:
                ts_unix = int(row["created_at"] or 0)
                payload = {
                    "title": row["title"],
                    "body": row["body"],
                    "status": row["status"],
                    "assignee": row["assignee"],
                    "priority": row["priority"],
                }
                insert_event(conn, {
                    "external_id": f"kanban:{board}:ticket:{row['id']}:create",
                    "source": "kanban",
                    "kind": "kanban_ticket_create",
                    "subject": row["title"],
                    "ts": _to_iso(ts_unix),
                    "ts_unix": ts_unix,
                    "last_seen_ts": ts_unix,
                    "payload_json": json.dumps(payload, sort_keys=True),
                    "board": board,
                    "ticket_id": row["id"],
                    "status": row["status"],
                    "source_ref": str(db_path),
                })
                stats["tickets"] += 1
                max_seen = max(max_seen, ts_unix)

            event_rows = src.execute(
                """
                SELECT e.id, e.task_id, e.kind, e.payload, e.created_at, t.title
                  FROM task_events e
                  LEFT JOIN tasks t ON t.id = e.task_id
                 WHERE e.created_at > ?
                   AND e.kind = 'status_transition'
                 ORDER BY e.created_at ASC, e.id ASC
                """,
                (last_seen,),
            ).fetchall()
            for row in event_rows:
                ts_unix = int(row["created_at"] or 0)
                payload = json.loads(row["payload"] or "{}")
                insert_event(conn, {
                    "external_id": f"kanban:{board}:event:{row['id']}",
                    "source": "kanban",
                    "kind": "kanban_status_transition",
                    "subject": row["title"] or row["task_id"],
                    "ts": _to_iso(ts_unix),
                    "ts_unix": ts_unix,
                    "last_seen_ts": ts_unix,
                    "payload_json": json.dumps(payload, sort_keys=True),
                    "board": board,
                    "ticket_id": row["task_id"],
                    "status": payload.get("to"),
                    "source_ref": str(db_path),
                })
                stats["transitions"] += 1
                max_seen = max(max_seen, ts_unix)

            comment_rows = src.execute(
                """
                SELECT c.id, c.task_id, c.author, c.body, c.created_at, t.title
                  FROM task_comments c
                  LEFT JOIN tasks t ON t.id = c.task_id
                 WHERE c.created_at > ?
                 ORDER BY c.created_at ASC, c.id ASC
                """,
                (last_seen,),
            ).fetchall()
            for row in comment_rows:
                ts_unix = int(row["created_at"] or 0)
                body = row["body"] or ""
                insert_event(conn, {
                    "external_id": f"kanban:{board}:comment:{row['id']}",
                    "source": "kanban",
                    "kind": "kanban_comment",
                    "subject": (body[:80] + "...") if len(body) > 80 else body,
                    "ts": _to_iso(ts_unix),
                    "ts_unix": ts_unix,
                    "last_seen_ts": ts_unix,
                    "payload_json": json.dumps({"author": row["author"], "body": body}, sort_keys=True),
                    "board": board,
                    "ticket_id": row["task_id"],
                    "source_ref": str(db_path),
                })
                stats["comments"] += 1
                max_seen = max(max_seen, ts_unix)
        finally:
            src.close()

        _state_set(conn, state_key, str(max_seen))
    return stats


def discover_git_repos(repo_roots: list[Path] | None = None) -> list[Path]:
    roots = repo_roots
    if roots is None:
        env = os.environ.get("ARGUS_REPO_ROOTS")
        if env:
            roots = [Path(p).expanduser() for p in env.split(os.pathsep) if p]
        else:
            roots = [Path.cwd(), Path.home() / "workspace", Path.home() / ".cache" / "chitin"]
    found: set[Path] = set()
    for root in roots:
        if not root.exists():
            continue
        if (root / ".git").exists():
            found.add(root.resolve())
            continue
        for dotgit in root.glob("**/.git"):
            if dotgit.is_dir() or dotgit.is_file():
                found.add(dotgit.parent.resolve())
    return sorted(found)


def _repo_slug(repo_path: Path) -> str:
    return repo_path.name


def _run(
    args: list[str],
    *,
    cwd: Path,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env.setdefault("GH_PROMPT_DISABLED", "1")
    env.setdefault("GIT_TERMINAL_PROMPT", "0")
    return runner(args, cwd=str(cwd), text=True, capture_output=True, check=False, env=env)


def _extract_ticket_id(*values: str | None) -> str | None:
    for value in values:
        if not value:
            continue
        m = TICKET_RE.search(value)
        if m:
            return m.group(1)
    return None


def _extract_driver(branch: str | None) -> str | None:
    if not branch:
        return None
    m = DRIVER_RE.match(branch)
    if not m:
        return None
    return m.group(1)


def _parse_ci_state(status_rollup: Any) -> str | None:
    if isinstance(status_rollup, list):
        states = {str(item.get("conclusion") or item.get("state") or "").upper() for item in status_rollup if isinstance(item, dict)}
        if states and states <= {"SUCCESS"}:
            return "green"
        if "FAILURE" in states or "ERROR" in states:
            return "red"
        if "PENDING" in states or "IN_PROGRESS" in states:
            return "pending"
    if isinstance(status_rollup, dict):
        state = str(status_rollup.get("state") or status_rollup.get("conclusion") or "").upper()
        if state in {"SUCCESS", "PASSED"}:
            return "green"
        if state in {"FAILURE", "ERROR"}:
            return "red"
        if state in {"PENDING", "IN_PROGRESS", "QUEUED"}:
            return "pending"
    return None


def _parse_headers(raw: str) -> dict[str, str]:
    headers: dict[str, str] = {}
    for line in raw.splitlines():
        if ":" not in line:
            continue
        name, value = line.split(":", 1)
        if name.lower().startswith("x-ratelimit-"):
            headers[name.lower()] = value.strip()
    return headers


def _respect_gh_rate_limit(
    repo_path: Path,
    *,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    sleep_fn: Callable[[float], None] = time.sleep,
    now_fn: Callable[[], float] = time.time,
) -> None:
    probe = _run(["gh", "api", "-i", "rate_limit"], cwd=repo_path, runner=runner)
    if probe.returncode != 0:
        return
    headers = _parse_headers(probe.stdout)
    remaining = int(headers.get("x-ratelimit-remaining", "1") or "1")
    reset_at = int(headers.get("x-ratelimit-reset", "0") or "0")
    if remaining > 0 or reset_at <= 0:
        return
    delay = max(0.0, reset_at - now_fn()) + 1.0
    if delay > 0:
        sleep_fn(delay)


def ingest_git_repo(
    conn: sqlite3.Connection,
    repo_path: Path,
    *,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    sleep_fn: Callable[[float], None] = time.sleep,
    now_fn: Callable[[], float] = time.time,
) -> dict[str, int]:
    """Index commit and PR history for one git repo."""
    stats = {"commits": 0, "prs": 0, "merged_prs": 0, "reviews": 0}
    repo_key = f"git:{repo_path}:"
    last_commit_ts = int(_state_get(conn, repo_key + "last_commit_ts", "0"))
    last_pr_updated_ts = int(_state_get(conn, repo_key + "last_pr_updated_ts", "0"))
    repo = _repo_slug(repo_path)

    since_arg = _to_iso(last_commit_ts) if last_commit_ts > 0 else "1970-01-01T00:00:00+00:00"
    commit_cmd = [
        "git", "-C", str(repo_path), "log", f"--since={since_arg}",
        "--format=%H%x1f%ct%x1f%an%x1f%s",
    ]
    env = os.environ.copy()
    env.setdefault("GH_PROMPT_DISABLED", "1")
    env.setdefault("GIT_TERMINAL_PROMPT", "0")
    commit_result = runner(commit_cmd, text=True, capture_output=True, check=False, env=env)
    if commit_result.returncode == 0:
        max_commit_ts = last_commit_ts
        for line in commit_result.stdout.splitlines():
            if not line.strip():
                continue
            sha, ts_raw, author, subject = line.split("\x1f", 3)
            ts_unix = int(ts_raw)
            insert_event(conn, {
                "external_id": f"git:{repo}:commit:{sha}",
                "source": "git",
                "kind": "git_commit",
                "subject": subject,
                "ts": _to_iso(ts_unix),
                "ts_unix": ts_unix,
                "last_seen_ts": ts_unix,
                "payload_json": json.dumps({"author": author, "subject": subject}, sort_keys=True),
                "repo": repo,
                "commit_sha": sha,
                "ticket_id": _extract_ticket_id(subject),
                "source_ref": str(repo_path),
            })
            stats["commits"] += 1
            max_commit_ts = max(max_commit_ts, ts_unix)
        _state_set(conn, repo_key + "last_commit_ts", str(max_commit_ts))

    _respect_gh_rate_limit(repo_path, runner=runner, sleep_fn=sleep_fn, now_fn=now_fn)

    updated_since = _to_iso(last_pr_updated_ts) if last_pr_updated_ts > 0 else "1970-01-01T00:00:00+00:00"
    pr_list_cmd = [
        "gh", "pr", "list", "--repo", repo, "--state", "all",
        "--search", f"updated:>={updated_since}",
        "--json", "number,title,body,createdAt,updatedAt,mergedAt,state,isDraft,url,headRefName",
    ]
    pr_list = _run(pr_list_cmd, cwd=repo_path, runner=runner)
    if pr_list.returncode != 0:
        return stats

    try:
        prs = json.loads(pr_list.stdout or "[]")
    except json.JSONDecodeError:
        return stats

    max_pr_updated = last_pr_updated_ts
    for pr in prs:
        number = int(pr["number"])
        view_cmd = [
            "gh", "pr", "view", str(number), "--repo", repo,
            "--json", "files,reviews,statusCheckRollup,mergeStateStatus,reviewDecision,commits",
        ]
        view_res = _run(view_cmd, cwd=repo_path, runner=runner)
        if view_res.returncode != 0:
            continue
        try:
            detail = json.loads(view_res.stdout or "{}")
        except json.JSONDecodeError:
            continue
        created_ts = _parse_iso(pr.get("createdAt"))
        updated_ts = _parse_iso(pr.get("updatedAt")) or created_ts or _now_ts()
        merged_ts = _parse_iso(pr.get("mergedAt"))
        ticket_id = _extract_ticket_id(pr.get("headRefName"), pr.get("title"), pr.get("body"))
        driver = _extract_driver(pr.get("headRefName"))
        ci_state = _parse_ci_state(detail.get("statusCheckRollup"))
        payload = {
            "body": pr.get("body"),
            "url": pr.get("url"),
            "state": pr.get("state"),
            "isDraft": pr.get("isDraft"),
            "headRefName": pr.get("headRefName"),
            "mergeStateStatus": detail.get("mergeStateStatus"),
            "reviewDecision": detail.get("reviewDecision"),
            "statusCheckRollup": detail.get("statusCheckRollup"),
            "ci_state": ci_state,
            "files": [f.get("path") for f in detail.get("files", []) if isinstance(f, dict)],
            "driver": driver,
        }
        if created_ts is not None:
            insert_event(conn, {
                "external_id": f"git:{repo}:pr:{number}:opened",
                "source": "git",
                "kind": "git_pr_opened",
                "subject": pr.get("title"),
                "ts": _to_iso(created_ts),
                "ts_unix": created_ts,
                "last_seen_ts": updated_ts,
                "payload_json": json.dumps(payload, sort_keys=True),
                "repo": repo,
                "ticket_id": ticket_id,
                "pr_number": number,
                "status": pr.get("state"),
                "source_ref": str(repo_path),
            })
            stats["prs"] += 1
        if merged_ts is not None:
            insert_event(conn, {
                "external_id": f"git:{repo}:pr:{number}:merged",
                "source": "git",
                "kind": "git_pr_merged",
                "subject": pr.get("title"),
                "ts": _to_iso(merged_ts),
                "ts_unix": merged_ts,
                "last_seen_ts": updated_ts,
                "payload_json": json.dumps(payload, sort_keys=True),
                "repo": repo,
                "ticket_id": ticket_id,
                "pr_number": number,
                "status": "MERGED",
                "source_ref": str(repo_path),
            })
            stats["merged_prs"] += 1
        for review in detail.get("reviews", []) or []:
            submitted_ts = _parse_iso(review.get("submittedAt"))
            if submitted_ts is None:
                continue
            review_id = str(review.get("id") or f"{number}:{submitted_ts}:{review.get('author', {}).get('login', 'unknown')}")
            insert_event(conn, {
                "external_id": f"git:{repo}:pr:{number}:review:{review_id}",
                "source": "git",
                "kind": "git_review_submitted",
                "subject": pr.get("title"),
                "ts": _to_iso(submitted_ts),
                "ts_unix": submitted_ts,
                "last_seen_ts": submitted_ts,
                "payload_json": json.dumps(review, sort_keys=True),
                "repo": repo,
                "ticket_id": ticket_id,
                "pr_number": number,
                "review_id": review_id,
                "status": review.get("state"),
                "source_ref": str(repo_path),
            })
            stats["reviews"] += 1
        max_pr_updated = max(max_pr_updated, updated_ts)

    _state_set(conn, repo_key + "last_pr_updated_ts", str(max_pr_updated))
    return stats


def ingest_git_sources(
    conn: sqlite3.Connection,
    repo_roots: list[Path] | None = None,
    *,
    runner: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    sleep_fn: Callable[[float], None] = time.sleep,
    now_fn: Callable[[], float] = time.time,
) -> dict[str, int]:
    totals = {"repos": 0, "commits": 0, "prs": 0, "merged_prs": 0, "reviews": 0}
    for repo in discover_git_repos(repo_roots):
        totals["repos"] += 1
        repo_stats = ingest_git_repo(conn, repo, runner=runner, sleep_fn=sleep_fn, now_fn=now_fn)
        for key, value in repo_stats.items():
            totals[key] += value
    return totals
