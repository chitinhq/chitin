"""MiniSession — the public interface Octi (slices 2+) imports.

Per spec AC10: this is the only public surface. Internals under _internal/
are never directly importable by code under swarm/octi/.
"""

from __future__ import annotations

import json
import os
import signal
import time
from pathlib import Path

from . import _internal as _i  # noqa: F401  (re-exports nothing; package marker only)
from ._internal import (
    goalid as _goalid,
    kitty as _kitty,
    lease as _lease,
    prompt as _prompt,
    statedir as _statedir,
    webhook as _webhook,
    worktree as _worktree,
)


class StatusMissingError(RuntimeError):
    pass


class GoalIdCollisionError(RuntimeError):
    pass


class RecoveryStateMissingError(RuntimeError):
    pass


CLAUDE_CMD_ENV = "MINI_CLAUDE_CMD"
DEFAULT_CLAUDE_CMD = ["claude", "--dangerously-skip-permissions"]


def _claude_command() -> list[str]:
    raw = os.environ.get(CLAUDE_CMD_ENV)
    if raw:
        return raw.split()
    return list(DEFAULT_CLAUDE_CMD)


class MiniSession:
    def __init__(self, goal_id: str) -> None:
        self._goal_id = goal_id

    # ---------------------------- factory methods ----------------------------

    @classmethod
    def open(
        cls,
        goal: str,
        *,
        recovery: str | None = None,
        ticket: str | None = None,
        cwd_is_worktree: bool = False,
    ) -> "MiniSession":
        if recovery is not None and goal:
            raise ValueError("--recovery and --goal are mutually exclusive")

        if recovery is not None:
            return cls._open_recovery(recovery)

        goal_id = _goalid.mint_goal_id(goal)
        sd = _statedir.state_dir(goal_id)
        if sd.exists():
            raise GoalIdCollisionError(
                f"state dir already exists: {sd}; use --recovery {goal_id}"
            )

        if cwd_is_worktree:
            wt = Path.cwd().resolve()
            branch = _worktree.resolve_branch_name(ticket=ticket, goal_id=goal_id)
        else:
            wt, branch = _worktree.create_worktree(goal_id=goal_id, ticket=ticket)

        _statedir.create_state_dir(goal_id, goal=goal, branch=branch, worktree=wt)
        _statedir.cleanup_stale_temp_files(goal_id)

        window_id = _kitty.launch_session(goal_id, cwd=wt, command=_claude_command())
        (_statedir.state_dir(goal_id) / "window_id").write_text(f"{window_id}\n")

        prompt = _prompt.render_initial_prompt(
            goal=goal, goal_id=goal_id, state_dir=sd, recovered=False
        )
        _kitty.inject_via_temp_file(goal_id, prompt, state_dir=sd, label="open")

        _webhook.post(
            _webhook.resolve_webhook_url(sd),
            f"🐙 session.opened `{goal_id}` worktree=`{wt}`\n> {goal[:200]}",
        )
        return cls(goal_id)

    @classmethod
    def _open_recovery(cls, goal_id: str) -> "MiniSession":
        sd = _statedir.state_dir(goal_id)
        if not sd.is_dir():
            raise RecoveryStateMissingError(
                f"no state dir for goal-id {goal_id!r}; cannot recover"
            )
        goal = (sd / "goal.txt").read_text().strip()
        wt = Path((sd / "worktree").read_text().strip())
        _statedir.cleanup_stale_temp_files(goal_id)

        if _kitty.find_window_by_goal(goal_id) is None:
            window_id = _kitty.launch_session(goal_id, cwd=wt, command=_claude_command())
            (sd / "window_id").write_text(f"{window_id}\n")
            prompt = _prompt.render_initial_prompt(
                goal=goal, goal_id=goal_id, state_dir=sd, recovered=True
            )
            _kitty.inject_via_temp_file(goal_id, prompt, state_dir=sd, label="recovery")
        return cls(goal_id)

    @classmethod
    def attach(cls, goal_id: str) -> "MiniSession":
        sd = _statedir.state_dir(goal_id)
        if not sd.is_dir():
            raise RecoveryStateMissingError(f"no state dir for goal-id {goal_id!r}")
        return cls(goal_id)

    # ---------------------------- properties ----------------------------

    @property
    def goal_id(self) -> str:
        return self._goal_id

    @property
    def state_dir(self) -> Path:
        return _statedir.state_dir(self._goal_id)

    @property
    def worktree(self) -> Path:
        return Path(_statedir.read_state_file(self._goal_id, "worktree"))

    @property
    def branch(self) -> str:
        return _statedir.read_state_file(self._goal_id, "branch")

    # ---------------------------- operations ----------------------------

    def status(self) -> dict:
        path = self.state_dir / "status.json"
        if not path.is_file():
            raise StatusMissingError(
                f"status.json not written yet for {self._goal_id}"
            )
        return json.loads(path.read_text())

    def nudge(
        self,
        message: str,
        *,
        holder: str | None = None,
        lease_seconds: int = _lease.DEFAULT_LEASE_SECONDS,
    ) -> None:
        if not message or not message.strip():
            raise ValueError("nudge message cannot be empty")
        sd = self.state_dir
        with _lease.Lease(sd, holder=holder, lease_seconds=lease_seconds):
            _kitty.inject_via_temp_file(self._goal_id, message, state_dir=sd, label="nudge")
            _webhook.post(
                _webhook.resolve_webhook_url(sd),
                f"📣 nudge.sent `{self._goal_id}` by `{holder or 'operator'}`\n> {message[:120]}",
            )

    def stop(self, *, reason: str = "operator-stop") -> None:
        sd = self.state_dir
        # Mark status=failed first so observers see the stop reason.
        try:
            current = self.status()
        except StatusMissingError:
            current = {}
        current.update(
            {
                "state": "failed",
                "updated_at": int(time.time()),
                "summary": f"stopped: {reason}",
                "next": "",
                "blockers": [],
                "verify": current.get("verify", ""),
            }
        )
        (sd / "status.json").write_text(json.dumps(current, indent=2) + "\n")

        _kitty.close_window(self._goal_id)

        # Kill watch.pid if present
        pid_file = sd / "watch.pid"
        if pid_file.is_file():
            try:
                pid = int(pid_file.read_text().strip())
                os.kill(pid, signal.SIGTERM)
            except (ValueError, ProcessLookupError, PermissionError):
                pass
            pid_file.unlink(missing_ok=True)

        _lease.release(sd)
        _webhook.post(
            _webhook.resolve_webhook_url(sd),
            f"🛑 session.stopped `{self._goal_id}` reason=`{reason}` state=failed",
        )
