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
    "status": "completed" | "failed" | "timeout",
    "returncode": <int>,
    "stdout": "<worker stdout>",
    "stderr": "<worker stderr>",
    "error": "<error message if status != completed>"
  }
"""

import json
import subprocess
import sys
import os


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

            status = "completed" if result.returncode == 0 else "failed"
            output = {
                "status": status,
                "returncode": result.returncode,
                "stdout": result.stdout,
                "stderr": result.stderr,
                "error": None if status == "completed" else f"Worker exited with code {result.returncode}"
            }
            print(json.dumps(output))
            return 0

        except subprocess.TimeoutExpired:
            print(json.dumps({
                "status": "timeout",
                "returncode": -1,
                "stdout": "",
                "stderr": "Worker process timed out after 3600 seconds",
                "error": "Worker timeout"
            }))
            return 1

        except Exception as e:
            print(json.dumps({
                "status": "failed",
                "returncode": -1,
                "stdout": "",
                "stderr": str(e),
                "error": f"Failed to spawn worker: {str(e)}"
            }))
            return 1

    except Exception as e:
        print(json.dumps({
            "status": "failed",
            "returncode": -1,
            "stdout": "",
            "stderr": str(e),
            "error": f"Worker spawn error: {str(e)}"
        }), file=sys.stderr)
        return 1

if __name__ == "__main__":
    sys.exit(main())
