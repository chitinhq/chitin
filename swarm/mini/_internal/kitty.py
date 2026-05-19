"""Kitty remote-control wrappers.

All `kitty @` calls happen here. Tests monkeypatch `run_kitten` to avoid
real kitty subprocess invocation.
"""

from __future__ import annotations

import json
import os
import secrets
import subprocess
from pathlib import Path


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


def inject_via_temp_file(goal_id: str, content: str, *, state_dir: Path, label: str) -> None:
    """AC9/AC13 — write content to temp file, send-text --from-file, unlink.

    Adds trailing \\r so the prompt commits in Claude Code's TUI.
    Already-gone unlink is not an error.
    """
    state_dir.mkdir(parents=True, exist_ok=True)
    tmp = state_dir / f".inject-{label}-{os.getpid()}-{secrets.token_hex(4)}.txt"
    payload = content if content.endswith("\r") else content + "\r"
    tmp.write_text(payload)
    try:
        send_text_from_file(goal_id, tmp)
    finally:
        try:
            tmp.unlink()
        except FileNotFoundError:
            pass
