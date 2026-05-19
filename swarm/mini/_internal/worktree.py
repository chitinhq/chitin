"""Worktree management for Mini sessions.

Per spec line 244: auto-create ~/workspace/chitin-octi-<slug> unless
--cwd-is-worktree. No primary checkout edits (constitution §2).
"""

from __future__ import annotations

import os
import subprocess
from pathlib import Path

DEFAULT_PRIMARY_NAME = "chitin"
DEFAULT_WORKSPACE = Path.home() / "workspace"


def primary_checkout() -> Path:
    """Heuristic: ~/workspace/chitin per operator convention.

    Operators can override via MINI_PRIMARY_CHECKOUT.
    """
    override = os.environ.get("MINI_PRIMARY_CHECKOUT")
    if override:
        return Path(override).expanduser().resolve()
    return (DEFAULT_WORKSPACE / DEFAULT_PRIMARY_NAME).resolve()


def worktree_path(goal_id: str) -> Path:
    """~/workspace/chitin-octi-<goal-id>"""
    return DEFAULT_WORKSPACE / f"chitin-octi-{goal_id}"


def resolve_branch_name(*, ticket: str | None, goal_id: str) -> str:
    if ticket:
        return f"agent/octi-{ticket}"
    return f"octi/{goal_id}"


def create_worktree(
    *,
    goal_id: str,
    ticket: str | None = None,
    base: str = "origin/main",
    runner=subprocess.run,
) -> tuple[Path, str]:
    """Create a fresh git worktree off origin/main. Returns (path, branch).

    Raises FileExistsError if the worktree path already exists.
    """
    primary = primary_checkout()
    wt = worktree_path(goal_id)
    branch = resolve_branch_name(ticket=ticket, goal_id=goal_id)

    if wt.exists():
        raise FileExistsError(
            f"worktree path already exists: {wt}; use --recovery to resume"
        )

    fetch = runner(
        ["git", "-C", str(primary), "fetch", "origin", "main"],
        capture_output=True, text=True,
    )
    if fetch.returncode != 0:
        raise RuntimeError(f"git fetch failed: {fetch.stderr.strip()}")

    add = runner(
        [
            "git", "-C", str(primary),
            "worktree", "add",
            "-b", branch,
            str(wt), base,
        ],
        capture_output=True, text=True,
    )
    if add.returncode != 0:
        raise RuntimeError(f"git worktree add failed: {add.stderr.strip()}")

    return wt, branch
