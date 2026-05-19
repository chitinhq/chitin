"""Worktree management for Mini sessions.

Per spec line 244: auto-create ~/workspace/chitin-octi-<slug> unless
--cwd-is-worktree. No primary checkout edits (constitution §2).

Also propagates the chitin governance signature (chitin.yaml.sig) from
the primary checkout to the new worktree, because that file is
gitignored and so git worktree add does not copy it — without the
sidecar, every tool call inside the worktree is rejected by the
governance hook as policy_signature_missing.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

DEFAULT_PRIMARY_NAME = "chitin"
DEFAULT_WORKSPACE = Path.home() / "workspace"
GOVERNANCE_SIDECARS = ("chitin.yaml.sig",)


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

    copy_governance_sidecars(primary=primary, worktree=wt)
    return wt, branch


def copy_governance_sidecars(
    *,
    primary: Path,
    worktree: Path,
    sidecars: tuple[str, ...] = GOVERNANCE_SIDECARS,
) -> list[str]:
    """Copy gitignored governance sidecars (e.g. chitin.yaml.sig) into the
    new worktree. Without these, the chitin policy hook rejects every
    tool call inside the worktree with `policy_signature_missing`.

    Returns the list of relative paths copied. Missing sources are
    silently skipped — the governance hook will surface a clear error if
    the sidecar is genuinely required and missing.
    """
    if not worktree.is_dir():
        return []
    copied: list[str] = []
    for rel in sidecars:
        src = primary / rel
        dst = worktree / rel
        if not src.is_file():
            continue
        if dst.exists():
            continue
        shutil.copy2(src, dst)
        copied.append(rel)
    return copied
