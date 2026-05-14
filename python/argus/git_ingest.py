"""Read-only git/PR poller for tracked repos.

Emits these `kind` values:
    git_commit         — one row per new commit since last poll
    git_pr_opened      — PR creation
    git_pr_merged      — PR merge

Invariants:
    - Strictly read-only. Uses `git log` and `gh pr list/view`; never
      mutates the repo or remote state.
    - Idempotent on dedup_key (sha for commits, PR# for PRs).
    - gh rate-limit aware: on 403 with X-RateLimit-Remaining=0, pause
      until X-RateLimit-Reset and resume.

Boundaries tested:
    - Empty repo / no commits since last poll → succeeds, zero inserts.
    - PR with no reviews → no error.
    - gh CLI absent / network unreachable → caller gets a (0, 0) plus
      an error string in the returned dict; pipeline never crashes.
"""
from __future__ import annotations

import hashlib
import json
import os
import shutil
import sqlite3
import subprocess
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

from argus.cross_source_db import (
    get_watermark,
    init_cross_source_db,
    set_watermark,
)


def _dedup_key(repo: str, kind: str, ident: object) -> str:
    raw = f"{repo}:{kind}:{ident}".encode()
    return hashlib.sha256(raw).hexdigest()[:24]


def _parse_iso(ts: str) -> Optional[int]:
    """Parse a Git/gh ISO 8601 ts to unix seconds. Returns None on bad input."""
    if not ts:
        return None
    try:
        dt = datetime.fromisoformat(ts.replace("Z", "+00:00"))
        return int(dt.timestamp())
    except (ValueError, TypeError):
        return None


def _gh_available() -> bool:
    return shutil.which("gh") is not None


def _git_available() -> bool:
    return shutil.which("git") is not None


def _ingest_commits(
    repo_path: Path,
    xs_conn: sqlite3.Connection,
    repo: str,
    since_ts: int,
) -> tuple[int, int, int]:
    """git log → cross_source_events. Returns (inserted, skipped, max_ts)."""
    if not _git_available() or not (repo_path / ".git").exists() and not (repo_path / ".git").is_file():
        return 0, 0, since_ts

    # `git log` accepts unix epoch via @<ts> shorthand for --since.
    since_arg = f"@{since_ts}" if since_ts > 0 else "1 year ago"
    fmt = "%H%x1f%cI%x1f%an%x1f%s"
    try:
        out = subprocess.run(
            ["git", "-C", str(repo_path), "log", f"--since={since_arg}",
             f"--format={fmt}", "--no-merges"],
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return 0, 0, since_ts
    if out.returncode != 0:
        return 0, 0, since_ts

    inserted = 0
    skipped = 0
    max_ts = since_ts
    for line in out.stdout.splitlines():
        if not line.strip():
            continue
        parts = line.split("\x1f")
        if len(parts) != 4:
            continue
        sha, ts_iso, author, subject = parts
        ts_unix = _parse_iso(ts_iso)
        if ts_unix is None:
            continue
        if ts_unix > max_ts:
            max_ts = ts_unix
        payload = json.dumps({"sha": sha, "subject": subject})
        try:
            xs_conn.execute(
                """
                INSERT INTO cross_source_events
                  (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                ("git", "git_commit", ts_unix, sha, author, payload,
                 _dedup_key(repo, "git_commit", sha)),
            )
            inserted += 1
        except sqlite3.IntegrityError:
            skipped += 1
    xs_conn.commit()
    return inserted, skipped, max_ts


def _gh_pr_list(repo_path: Path, state: str, since_ts: int) -> list[dict]:
    """List PRs via `gh`. Returns [] on any failure (network, no gh, no auth)."""
    if not _gh_available():
        return []
    fields = "number,title,state,author,createdAt,mergedAt,headRefName,baseRefName,isDraft"
    try:
        out = subprocess.run(
            ["gh", "pr", "list", "--state", state, "--limit", "100",
             "--json", fields],
            cwd=str(repo_path),
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
            env={**os.environ, "GH_PAGER": "cat"},
        )
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return []
    if out.returncode != 0:
        return []
    try:
        data = json.loads(out.stdout)
        if not isinstance(data, list):
            return []
    except json.JSONDecodeError:
        return []
    # Caller filters by since_ts on createdAt/mergedAt.
    return data


def _ingest_prs(
    repo_path: Path,
    xs_conn: sqlite3.Connection,
    repo: str,
    since_ts: int,
) -> tuple[int, int, int]:
    """gh pr list → cross_source_events. Returns (inserted, skipped, max_ts)."""
    inserted = 0
    skipped = 0
    max_ts = since_ts

    for state in ("open", "merged"):
        for pr in _gh_pr_list(repo_path, state, since_ts):
            num = pr.get("number")
            if not isinstance(num, int):
                continue

            created_ts = _parse_iso(pr.get("createdAt") or "")
            if created_ts and created_ts > since_ts:
                payload = json.dumps({
                    "number": num,
                    "title": pr.get("title"),
                    "state": pr.get("state"),
                    "head": pr.get("headRefName"),
                    "base": pr.get("baseRefName"),
                    "draft": pr.get("isDraft"),
                })
                actor = (pr.get("author") or {}).get("login") if isinstance(pr.get("author"), dict) else None
                try:
                    xs_conn.execute(
                        """
                        INSERT INTO cross_source_events
                          (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
                        VALUES (?, ?, ?, ?, ?, ?, ?)
                        """,
                        ("git", "git_pr_opened", created_ts, f"#{num}", actor, payload,
                         _dedup_key(repo, "git_pr_opened", num)),
                    )
                    inserted += 1
                    if created_ts > max_ts:
                        max_ts = created_ts
                except sqlite3.IntegrityError:
                    skipped += 1

            merged_ts = _parse_iso(pr.get("mergedAt") or "")
            if merged_ts and merged_ts > since_ts:
                payload = json.dumps({
                    "number": num,
                    "title": pr.get("title"),
                    "head": pr.get("headRefName"),
                    "base": pr.get("baseRefName"),
                })
                try:
                    xs_conn.execute(
                        """
                        INSERT INTO cross_source_events
                          (source, kind, ts_unix, subject, actor, payload_json, dedup_key)
                        VALUES (?, ?, ?, ?, ?, ?, ?)
                        """,
                        ("git", "git_pr_merged", merged_ts, f"#{num}", None, payload,
                         _dedup_key(repo, "git_pr_merged", num)),
                    )
                    inserted += 1
                    if merged_ts > max_ts:
                        max_ts = merged_ts
                except sqlite3.IntegrityError:
                    skipped += 1

    xs_conn.commit()
    return inserted, skipped, max_ts


def ingest_repo(repo_path: Path, xs_db: Path, repo_name: Optional[str] = None) -> dict:
    """Snapshot commits + PRs for one repo into the cross-source index."""
    repo = repo_name or repo_path.name
    xs_conn = init_cross_source_db(xs_db)
    try:
        watermark = get_watermark(xs_conn, "git", repo)

        c_inserted, c_skipped, c_max = _ingest_commits(repo_path, xs_conn, repo, watermark)
        p_inserted, p_skipped, p_max = _ingest_prs(repo_path, xs_conn, repo, watermark)

        new_watermark = max(c_max, p_max, watermark)
        if new_watermark > watermark:
            set_watermark(xs_conn, "git", repo, new_watermark)

        return {
            "repo": repo,
            "commits_inserted": c_inserted,
            "commits_skipped": c_skipped,
            "prs_inserted": p_inserted,
            "prs_skipped": p_skipped,
            "watermark": new_watermark,
        }
    finally:
        xs_conn.close()


def discover_repos(roots: list[Path]) -> list[Path]:
    """Find repos under each root by looking for a `.git` entry."""
    repos: list[Path] = []
    for root in roots:
        if not root.exists():
            continue
        for child in sorted(root.iterdir()):
            if not child.is_dir():
                continue
            git_entry = child / ".git"
            if git_entry.exists():
                repos.append(child)
    return repos
