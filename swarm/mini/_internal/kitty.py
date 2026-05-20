"""Kitty remote-control wrappers.

All `kitty @` calls happen here. Tests monkeypatch `run_kitten` to avoid
real kitty subprocess invocation.
"""

from __future__ import annotations

import json
import os
import secrets
import subprocess
import time
from pathlib import Path

# Claude Code's TUI shows this footer string once it is ready for input —
# present in both idle and mid-thought states, so it is a stable readiness
# marker. Injecting before the TUI is up loses the keystrokes (spec 050 R4).
_CLAUDE_READY_MARKER = "bypass permissions"


KITTY_BIN_ENV = "MINI_KITTY_BIN"


def kitty_bin() -> str:
    return os.environ.get(KITTY_BIN_ENV) or "kitty"


def run_kitten(args: list[str], *, input_bytes: bytes | None = None, timeout: int = 10) -> subprocess.CompletedProcess:
    """Run `kitty @ <args>` and return CompletedProcess. Raises on non-zero."""
    cmd = [kitty_bin(), "@"] + list(args)
    return subprocess.run(
        cmd,
        input=input_bytes,
        capture_output=True,
        timeout=timeout,
        check=False,
    )


def kitty_ls() -> list[dict]:
    proc = run_kitten(["ls"])
    if proc.returncode != 0:
        raise RuntimeError(
            f"kitty @ ls failed (rc={proc.returncode}): "
            f"{proc.stderr.decode('utf-8', 'replace').strip()}"
        )
    return json.loads(proc.stdout)


def find_window_by_goal(goal_id: str) -> dict | None:
    """Walk `kitty @ ls` looking for a window with user_var mini_goal=<id>."""
    for os_window in kitty_ls():
        for tab in os_window.get("tabs", []):
            for window in tab.get("windows", []):
                if window.get("user_vars", {}).get("mini_goal") == goal_id:
                    return window
    return None


def launch_session(goal_id: str, *, cwd: Path, command: list[str]) -> int:
    """Launch a new kitty tab for the goal. Returns the window id.

    Uses --hold so that if `command` exits, the operator can see the error.
    """
    args = [
        "launch",
        "--type=tab",
        f"--tab-title=mini:{goal_id}",
        f"--var=mini_goal={goal_id}",
        f"--cwd={str(cwd)}",
        "--hold",
    ] + list(command)
    proc = run_kitten(args)
    if proc.returncode != 0:
        raise RuntimeError(
            f"kitty @ launch failed (rc={proc.returncode}): "
            f"{proc.stderr.decode('utf-8', 'replace').strip()}"
        )
    out = proc.stdout.decode("utf-8", "replace").strip()
    try:
        return int(out)
    except ValueError as e:
        raise RuntimeError(f"kitty @ launch returned non-integer window id: {out!r}") from e


def send_text_from_file(goal_id: str, file_path: Path, *, timeout: int = 10) -> None:
    args = [
        "send-text",
        f"--match=var:mini_goal={goal_id}",
        f"--from-file={str(file_path)}",
    ]
    proc = run_kitten(args, timeout=timeout)
    if proc.returncode != 0:
        raise RuntimeError(
            f"kitty @ send-text failed (rc={proc.returncode}): "
            f"{proc.stderr.decode('utf-8', 'replace').strip()}"
        )


def get_screen_text(goal_id: str) -> str:
    args = [
        "get-text",
        f"--match=var:mini_goal={goal_id}",
        "--extent=screen",
    ]
    proc = run_kitten(args)
    if proc.returncode != 0:
        raise RuntimeError(
            f"kitty @ get-text failed (rc={proc.returncode}): "
            f"{proc.stderr.decode('utf-8', 'replace').strip()}"
        )
    return proc.stdout.decode("utf-8", "replace")


def close_window(goal_id: str) -> None:
    args = [
        "close-window",
        f"--match=var:mini_goal={goal_id}",
    ]
    proc = run_kitten(args)
    if proc.returncode != 0 and b"No matching" not in proc.stderr:
        raise RuntimeError(
            f"kitty @ close-window failed (rc={proc.returncode}): "
            f"{proc.stderr.decode('utf-8', 'replace').strip()}"
        )


def send_enter(goal_id: str, *, timeout: int = 10) -> None:
    """Send a single Enter keypress to the goal's window.

    Spec 050 R4: a trailing \\r appended to the send-text payload does NOT
    commit the prompt. Claude Code's TUI applies a paste heuristic to a
    text blob that arrives in one read() — the \\r inside that blob is
    absorbed as a literal newline, not Enter. Submission must be a
    distinct input event: `kitty @ send-key enter` arrives as its own
    key press/release, after the paste, and commits the prompt.
    """
    args = ["send-key", f"--match=var:mini_goal={goal_id}", "enter"]
    proc = run_kitten(args, timeout=timeout)
    if proc.returncode != 0:
        raise RuntimeError(
            f"kitty @ send-key enter failed (rc={proc.returncode}): "
            f"{proc.stderr.decode('utf-8', 'replace').strip()}"
        )


def wait_for_claude_ready(goal_id: str, *, timeout: float = 45.0,
                          poll: float = 0.5) -> bool:
    """Poll the goal's window until Claude Code's TUI is ready for input.

    Spec 050 R4: injecting at session-open/recovery time raced the TUI
    boot — send-text + send-key landed before Claude was listening and
    the prompt sat unsubmitted. Returns True once the readiness marker
    appears, False on timeout.
    """
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            if _CLAUDE_READY_MARKER in get_screen_text(goal_id):
                return True
        except RuntimeError:
            pass  # window not up yet — keep polling
        time.sleep(poll)
    return False


def inject_via_temp_file(goal_id: str, content: str, *, state_dir: Path,
                         label: str, wait_ready: bool = True) -> None:
    """AC9/AC13 — write content to temp file, send-text --from-file, unlink,
    then commit with a separate Enter keypress (spec 050 R4).

    The prompt body is sent WITHOUT a trailing \\r — submission is a
    distinct `send_enter` call so Enter lands outside the TUI's paste
    chunk. When `wait_ready` is set (default), block until Claude's TUI
    is up before injecting, so the keystrokes are not lost to a boot
    race. Invariant: after this returns, the prompt has been submitted
    to Claude, not left sitting in the input buffer.

    Already-gone unlink is not an error.
    """
    if wait_ready and not wait_for_claude_ready(goal_id):
        raise RuntimeError(
            f"claude TUI for {goal_id} not ready within timeout; "
            f"prompt not injected"
        )
    state_dir.mkdir(parents=True, exist_ok=True)
    tmp = state_dir / f".inject-{label}-{os.getpid()}-{secrets.token_hex(4)}.txt"
    # Body only — strip any trailing CR/LF so the heuristic sees a clean
    # paste and the Enter we send next is unambiguous.
    tmp.write_text(content.rstrip("\r\n"))
    try:
        send_text_from_file(goal_id, tmp)
        send_enter(goal_id)
    finally:
        try:
            tmp.unlink()
        except FileNotFoundError:
            pass
