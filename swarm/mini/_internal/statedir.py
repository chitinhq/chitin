"""State directory helpers for Mini sessions.

Layout: <root>/.swarm/octi/<goal-id>/
  - goal.txt        : raw goal text
  - goal_id         : the goal id (for cwd-based resolution)
  - branch          : git branch name
  - worktree        : absolute worktree path
  - status.json     : 6-field state contract (written by Claude)
  - input.lock      : input lease (JSON: holder/acquired_at/expires_at)
  - transcript.log  : append-only kitty get-text capture
  - watch.pid       : optional PID file for `mini watch`
  - webhook.url     : optional per-session webhook override
"""

from __future__ import annotations

import os
import time
from pathlib import Path

STATE_ROOT_ENV = "MINI_STATE_ROOT"


def state_root() -> Path:
    """Root directory under which all goal-id state dirs live.

    Default is ``~/.swarm/octi`` — never the operator's primary checkout.
    Putting state under cwd was a slice-1 bug that caused writes inside
    ``~/workspace/chitin/`` (constitution §2 violation) whenever the
    operator ran ``mini open`` from the primary.
    """
    override = os.environ.get(STATE_ROOT_ENV)
    if override:
        return Path(override).expanduser().resolve()
    return (Path.home() / ".swarm" / "octi").resolve()


def state_dir(goal_id: str) -> Path:
    return state_root() / goal_id


def create_state_dir(goal_id: str, *, goal: str, branch: str, worktree: Path) -> Path:
    sd = state_dir(goal_id)
    sd.mkdir(parents=True, exist_ok=True)
    (sd / "goal.txt").write_text(goal + ("\n" if not goal.endswith("\n") else ""))
    (sd / "goal_id").write_text(goal_id + "\n")
    (sd / "branch").write_text(branch + "\n")
    (sd / "worktree").write_text(str(worktree) + "\n")
    return sd


def read_state_file(goal_id: str, name: str) -> str:
    return (state_dir(goal_id) / name).read_text().strip()


def cleanup_stale_temp_files(goal_id: str, *, max_age_seconds: int = 300) -> int:
    """Unlink stale .inject-*.txt files in the goal state dir.

    Called on every `mini open` and `mini status` per AC9.
    Returns count of files unlinked. FileNotFoundError swallowed.
    """
    sd = state_dir(goal_id)
    if not sd.is_dir():
        return 0
    cutoff = time.time() - max_age_seconds
    unlinked = 0
    for f in sd.glob(".inject-*.txt"):
        try:
            if f.stat().st_mtime < cutoff:
                f.unlink()
                unlinked += 1
        except FileNotFoundError:
            pass
    return unlinked
