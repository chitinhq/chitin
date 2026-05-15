#!/usr/bin/env python3
"""
Spawn a frontier-coder worker as a subprocess with output capture.

This replaces the lobster `exec` pattern with subprocess + wait, enabling:
- Output capture
- Result observation
- Proper failure handling
- Parent-child session tracking

Reads config from stdin (JSON):
  {
    "driver": "claude-code",
    "model": "claude-opus-4-7",
    "worktree": "/path/to/worktree",
    "branch": "swarm/claude-code-t_xxx",
    "cmd": "claude",
    "args": ["--model", "...", "-p", "..."],
    "env": {
      "CHITIN_DRIVER": "clawta",
      "CHITIN_BUDGET_ENVELOPE": "..."
    }
  }

Outputs JSON:
  {
    "status": "completed" | "completed_no_commit" | "failed" | "timeout",
    "returncode": <int>,
    "stdout": "<worker stdout>",
    "stderr": "<worker stderr>",
    "error": "<error message if status != completed>",
    "exit_reason": "<machine-readable failure classification>",
    "transcript_tail": "<last lines of stdout/stderr>",
    "commit_count_ahead": <int>
  }
"""

import json
import os
import subprocess
import sys
from pathlib import Path


TRANSCRIPT_TAIL_LINES = 40


def prepare_worker_command(config: dict) -> tuple[list[str], str | None]:
    """Build a worker command line while keeping long prompts out of argv.

    Returns (argv, stdin_text). The caller is responsible for feeding
    stdin_text to the subprocess when non-None.
    """
    driver = config.get("driver", "unknown")
    model = config.get("model", "")
    prompt = config.get("prompt", "")
    args_template = config.get("args", [])

    argv: list[str] = [config.get("cmd", "")]
    stdin_text: str | None = None

    for arg in args_template:
        if arg == "{model}":
            argv.append(model)
            continue

        if arg == "{prompt}":
            if driver in {"codex", "copilot"}:
                stdin_text = prompt
                continue
            if driver == "gemini":
                # gemini CLI: `-p ""` selects non-interactive mode while
                # leaving the prompt body to be read from stdin. The empty
                # argv slot is the flag value, not the prompt — the real
                # prompt travels via stdin_text so it stays off argv.
                argv.append("")
                stdin_text = prompt
                continue
            argv.append(prompt)
            continue

        argv.append(arg)

    if prompt and driver == "gemini" and "{prompt}" not in args_template:
        # Card has no {prompt} placeholder: append `-p ""` ourselves so the
        # gemini CLI still runs non-interactively and consumes stdin_text.
        argv.extend(["-p", ""])
        stdin_text = prompt

    return argv, stdin_text


def build_transcript_tail(stdout: str, stderr: str, max_lines: int = TRANSCRIPT_TAIL_LINES) -> str:
    sections: list[str] = []
    if stdout:
        sections.extend(f"[stdout] {line}" for line in stdout.splitlines())
    if stderr:
        sections.extend(f"[stderr] {line}" for line in stderr.splitlines())
    if not sections:
        return ""
    return "\n".join(sections[-max_lines:])


def commits_ahead_of_base(cwd: str) -> int:
    merge_base = subprocess.run(
        ["git", "merge-base", "origin/main", "HEAD"],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    if merge_base.returncode == 0:
        result = subprocess.run(
            ["git", "rev-list", "--count", f"{merge_base.stdout.strip()}..HEAD"],
            cwd=cwd,
            capture_output=True,
            text=True,
        )
        if result.returncode == 0:
            return int(result.stdout.strip() or "0")

    fallback = subprocess.run(
        ["git", "rev-list", "--count", "origin/main..HEAD"],
        cwd=cwd,
        capture_output=True,
        text=True,
    )
    if fallback.returncode != 0:
        raise RuntimeError((fallback.stderr or merge_base.stderr).strip() or "unable to count commits ahead of origin/main")
    return int(fallback.stdout.strip() or "0")


def snapshot_event_files(chitin_home: str) -> dict[str, int]:
    root = Path(chitin_home).expanduser()
    if not root.is_dir():
        return {}
    snapshot: dict[str, int] = {}
    for path in root.glob("*events-*.jsonl"):
        try:
            snapshot[str(path)] = path.stat().st_mtime_ns
        except OSError:
            continue
    return snapshot


def detect_event_chain(
    chitin_home: str, before: dict[str, int]
) -> tuple[str | None, str | None]:
    after = snapshot_event_files(chitin_home)
    changed: list[tuple[str, int]] = []
    for path, mtime in after.items():
        if path not in before or mtime > before[path]:
            changed.append((path, mtime))
    if not changed:
        return None, None

    chain_file = max(changed, key=lambda item: item[1])[0]
    last_hash: str | None = None
    try:
        for raw in Path(chain_file).read_text().splitlines():
            line = raw.strip()
            if not line:
                continue
            try:
                payload = json.loads(line)
            except json.JSONDecodeError:
                continue
            this_hash = payload.get("this_hash")
            if this_hash:
                last_hash = str(this_hash)
    except OSError:
        return chain_file, None
    return chain_file, last_hash


def summarize_completed_run(
    config: dict,
    returncode: int,
    stdout: str,
    stderr: str,
    cwd: str,
    event_chain_file: str | None = None,
    event_chain_hash: str | None = None,
) -> dict:
    transcript_tail = build_transcript_tail(stdout, stderr)
    commit_count_ahead = commits_ahead_of_base(cwd)
    driver = config.get("driver", "unknown")
    model = config.get("model", "")
    if returncode == 0 and commit_count_ahead == 0:
        return {
            "status": "completed_no_commit",
            "returncode": returncode,
            "stdout": stdout,
            "stderr": stderr,
            "error": "Worker session ended without creating any commits",
            "exit_reason": "model-concluded-nothing",
            "transcript_tail": transcript_tail,
            "commit_count_ahead": commit_count_ahead,
            "driver": driver,
            "model": model,
            "event_chain_file": event_chain_file,
            "event_chain_hash": event_chain_hash,
        }

    status = "completed" if returncode == 0 else "failed"
    return {
        "status": status,
        "returncode": returncode,
        "stdout": stdout,
        "stderr": stderr,
        "error": None if status == "completed" else f"Worker exited with code {returncode}",
        "exit_reason": None if status == "completed" else "session-error",
        "transcript_tail": transcript_tail,
        "commit_count_ahead": commit_count_ahead,
        "driver": driver,
        "model": model,
        "event_chain_file": event_chain_file,
        "event_chain_hash": event_chain_hash,
    }

def main():
    try:
        # Read config from stdin
        config_json = sys.stdin.read()
        config = json.loads(config_json)

        driver = config.get("driver", "unknown")
        cmd = config.get("cmd", "")
        worktree = config.get("worktree", "")
        env_vars = config.get("env", {})

        if not cmd:
            print(json.dumps({
                "status": "failed",
                "returncode": 1,
                "stdout": "",
                "stderr": "No cmd specified in config",
                "error": "Missing cmd in config"
            }))
            return 1

        # Prepare environment
        env = os.environ.copy()
        env.update(env_vars)
        chitin_home = env.get("CHITIN_HOME", os.path.join(os.path.expanduser("~"), ".chitin"))

        # Prepare working directory
        cwd = worktree if worktree else os.getcwd()
        if not os.path.isdir(cwd):
            print(json.dumps({
                "status": "failed",
                "returncode": 1,
                "stdout": "",
                "stderr": f"Worktree directory not found: {cwd}",
                "error": f"Worktree missing: {cwd}"
            }))
            return 1

        # Spawn worker process. Driver-specific prompt handling keeps
        # large ticket bodies off argv where possible.
        full_cmd, stdin_text = prepare_worker_command(config)
        event_files_before = snapshot_event_files(chitin_home)
        try:
            result = subprocess.run(
                full_cmd,
                cwd=cwd,
                env=env,
                capture_output=True,
                text=True,
                input=stdin_text,
                timeout=3600  # 1 hour timeout, same as before
            )
            event_chain_file, event_chain_hash = detect_event_chain(chitin_home, event_files_before)

            output = summarize_completed_run(
                config,
                result.returncode,
                result.stdout,
                result.stderr,
                cwd,
                event_chain_file=event_chain_file,
                event_chain_hash=event_chain_hash,
            )
            if output["status"] == "completed_no_commit":
                print(
                    json.dumps(
                        {
                            "exit_reason": output["exit_reason"],
                            "model": output["model"],
                            "driver": output["driver"],
                            "commit_count_ahead": output["commit_count_ahead"],
                            "transcript_tail": output["transcript_tail"],
                        }
                    ),
                    file=sys.stderr,
                )
            print(json.dumps(output))
            return 0

        except subprocess.TimeoutExpired:
            # A timed-out worker may still have written Chitin events before
            # it was killed — detect the chain so the run ledger keeps the
            # link instead of recording a null hash.
            timeout_chain_file, timeout_chain_hash = detect_event_chain(
                chitin_home, event_files_before
            )
            print(json.dumps({
                "status": "timeout",
                "returncode": -1,
                "stdout": "",
                "stderr": "Worker process timed out after 3600 seconds",
                "error": "Worker timeout",
                "exit_reason": "session-timeout",
                "transcript_tail": "",
                "commit_count_ahead": 0,
                "driver": driver,
                "model": config.get("model", ""),
                "event_chain_file": timeout_chain_file,
                "event_chain_hash": timeout_chain_hash,
            }))
            return 1

        except Exception as e:
            print(json.dumps({
                "status": "failed",
                "returncode": -1,
                "stdout": "",
                "stderr": str(e),
                "error": f"Failed to spawn worker: {str(e)}",
                "exit_reason": "session-error",
                "transcript_tail": "",
                "commit_count_ahead": 0,
                "driver": driver,
                "model": config.get("model", ""),
                "event_chain_file": None,
                "event_chain_hash": None,
            }))
            return 1

    except Exception as e:
        print(json.dumps({
            "status": "failed",
            "returncode": -1,
            "stdout": "",
            "stderr": str(e),
            "error": f"Worker spawn error: {str(e)}",
            "exit_reason": "spawn-error",
            "transcript_tail": "",
            "commit_count_ahead": 0,
            "driver": "unknown",
            "model": "",
            "event_chain_file": None,
            "event_chain_hash": None,
        }), file=sys.stderr)
        return 1

if __name__ == "__main__":
    sys.exit(main())
