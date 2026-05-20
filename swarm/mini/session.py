"""MiniSession — the public interface Octi (slices 2+) imports.

Per spec AC10: this is the only public surface. Internals under _internal/
are never directly importable by code under swarm/octi/.
"""

from __future__ import annotations

import json
import os
import shutil
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
    """Resolve the claude binary to an absolute path before handing it
    to `kitty @ launch`. The kitty child shell may not inherit the
    operator's interactive PATH — when claude lives in a Node-managed
    location like ~/.vite-plus/bin, that dir isn't on the default zsh
    PATH the child shell loads. Resolving once at session-open avoids
    the "claude not found in PATH" silent kitty-window-dies-immediately
    failure mode."""
    raw = os.environ.get(CLAUDE_CMD_ENV)
    if raw:
        return raw.split()
    claude_bin = shutil.which("claude")
    if claude_bin is None:
        raise FileNotFoundError(
            "claude CLI not found in PATH. Set MINI_CLAUDE_CMD env var "
            "with an absolute command, or add claude to PATH."
        )
    return [claude_bin, "--dangerously-skip-permissions"]


# Status-to-emoji mapping for Discord posts (S2-R3)
_STATUS_EMOJI: dict[str, str] = {
    "working": "✅",
    "verifying": "✅",
    "blocked": "🔶",
    "done": "🏁",
    "needs_review": "🏁",
    "failed": "🛑",
    "paused": "🔶",
    "idle": "🔶",
}


def _status_emoji(state: str) -> str:
    return _STATUS_EMOJI.get(state, "🔁")


def _format_opened_message(
    goal_id: str,
    worktree: Path,
    goal: str,
    *,
    invoked_by: str | None = None,
    source: str = "cli",
    specs: list[str] | None = None,
) -> str:
    """Build the 🐙 session.opened message per S2-R1.

    Format:
      🐙 Mini session opened — spec 037,038,039 — invoked by `ares` via mcp
         goal_id: spec-037..039-3a1f   ·   thread ↳
    """
    invoker = invoked_by or os.environ.get("OCTI_OPERATOR") or "operator"
    spec_list = ",".join(specs) if specs else ""
    parts = [f"🐙 Mini session opened"]
    if spec_list:
        parts.append(f"spec {spec_list}")
    parts.append(f"invoked by `{invoker}` via {source}")
    line1 = " — ".join(parts)
    line2 = f"   goal_id: `{goal_id}`   ·   worktree: `{worktree}`"
    line3 = f"> {goal[:200]}"
    return f"{line1}\n{line2}\n{line3}"


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
        invoked_by: str | None = None,
        source: str = "cli",
        specs: list[str] | None = None,
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

        # S2-R1: enhanced opened message with invoker, source, specs
        opened_msg = _format_opened_message(
            goal_id, wt, goal,
            invoked_by=invoked_by, source=source, specs=specs,
        )

        webhook_url = _webhook.resolve_webhook_url(sd)

        # S2-R2: create a per-session Discord thread
        thread_id: str | None = None
        thread_id = _webhook.create_and_post_to_thread(
            webhook_url, goal_id, opened_msg,
        )
        if thread_id:
            _statedir.write_discord_thread_id(goal_id, thread_id)
        else:
            # Thread creation failed; post to channel as fallback (S2-R4)
            _webhook.post(webhook_url, opened_msg)

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

    def _webhook_url_and_thread(self) -> tuple[str | None, str | None]:
        """Resolve webhook URL and per-session thread_id (if any)."""
        sd = self.state_dir
        url = _webhook.resolve_webhook_url(sd)
        thread_id = _statedir.read_discord_thread_id(self._goal_id)
        return url, thread_id

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
            # S2-R3: post nudge.sent into the session thread when available
            url, thread_id = self._webhook_url_and_thread()
            _webhook.post_to_thread(
                url, thread_id,
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
        # S2-R3: post session.stopped into the session thread when available
        url, thread_id = self._webhook_url_and_thread()
        _webhook.post_to_thread(
            url, thread_id,
            f"🛑 session.stopped `{self._goal_id}` reason=`{reason}` state=failed",
        )