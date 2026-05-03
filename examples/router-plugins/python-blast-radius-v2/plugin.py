#!/usr/bin/env python3
"""Example chitin router heuristic plugin (Python).

Reads a single JSON line from stdin:
  { "hook_input": {...}, "config": {...} }

Writes a single JSON line to stdout:
  { "score": 0.0-1.0, "fired": bool, "reason": "...", "axis": {...} }

This example: a "destination-allowlist" heuristic. Fires when a
write-shaped action targets a path NOT on the operator's allowlist.

Operator chitin.yaml:

  router:
    plugins:
      - name: destination-allowlist
        type: heuristic
        runtime: python3
        module: examples/router-plugins/python-blast-radius-v2/plugin.py
        config:
          allowed_prefixes:
            - "apps/temporal-worker/"
            - "libs/contracts/"
            - "docs/"
          threshold: 0.5
        timeout_ms: 1000

Cold start: ~50-100ms (faster than Node).
"""
import json
import sys


def main() -> None:
    raw = sys.stdin.read()
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError as e:
        print(json.dumps({"score": 0, "fired": False, "reason": f"plugin-bad-stdin:{e}"}))
        sys.exit(0)

    hook_input = payload.get("hook_input", {})
    config = payload.get("config", {})
    allowed: list[str] = config.get("allowed_prefixes", [])
    threshold: float = float(config.get("threshold", 0.5))

    tool_name: str = hook_input.get("tool_name", "")
    tool_input: dict = hook_input.get("tool_input", {})

    # Read-shaped tools never fire — only writes are gated
    if tool_name in {
        "Read", "Glob", "Grep", "LS", "TaskGet", "TaskList",
        "TaskOutput", "ToolSearch", "AskUserQuestion",
        "EnterPlanMode", "ExitPlanMode",
    }:
        print(json.dumps({"score": 0.0, "fired": False, "reason": "read-shape-skip"}))
        return

    # Extract target path from any of the common shapes
    target = (
        tool_input.get("file_path")
        or tool_input.get("notebook_path")
        or _extract_path_from_command(tool_input.get("command", ""))
        or ""
    )
    if not target:
        print(json.dumps({"score": 0.0, "fired": False, "reason": "no-target-path"}))
        return

    if any(target.startswith(p) or p in target for p in allowed):
        print(json.dumps({
            "score": 0.0,
            "fired": False,
            "reason": f"in-allowlist:{target[:60]}",
        }))
        return

    # Out of allowlist — fire
    score = 0.7  # always above default threshold
    print(json.dumps({
        "score": score,
        "fired": score >= threshold,
        "reason": f"target-not-in-allowlist:{target[:80]}",
        "axis": {"allowed_prefixes_count": len(allowed)},
    }))


def _extract_path_from_command(cmd: str) -> str:
    if not cmd:
        return ""
    for tok in cmd.split():
        if tok.startswith("-"):
            continue
        if "/" in tok or any(tok.endswith(s) for s in (".ts", ".go", ".py", ".md", ".json")):
            return tok
    return ""


if __name__ == "__main__":
    main()
