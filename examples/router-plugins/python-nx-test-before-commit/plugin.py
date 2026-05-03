#!/usr/bin/env python3
"""Pre-action analysis plugin: block git commit when nx-affected tests fail.

Runs `pnpm exec nx affected -t test --base=<base>` against the
current diff before allowing a `git commit` shell.exec action. If
any test fails, the plugin returns Block=true and the router
denies the commit immediately (no advisor consultation — this is
a deterministic check, not a judgment call).

Operator chitin.yaml:

  router:
    enabled: true
    plugins:
      - name: nx-test-before-commit
        type: heuristic
        runtime: python3
        module: examples/router-plugins/python-nx-test-before-commit/plugin.py
        config:
          base_ref: origin/main
          test_target: test
        timeout_ms: 120000           # nx affected can take up to 2 min on big repos

  # Trust this plugin (path+hash):
  plugins_trust:
    mode: path+hash
    trusted_paths:
      - <abs path to plugin.py>
    trusted_hashes:
      nx-test-before-commit: <sha256>
"""
import json
import subprocess
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
    tool_name: str = hook_input.get("tool_name", "")
    cmd: str = hook_input.get("tool_input", {}).get("command", "") or ""
    cwd: str = hook_input.get("cwd", "") or "."

    # Only fires on git-commit shell calls
    if tool_name != "Bash" or "git commit" not in cmd:
        print(json.dumps({"score": 0, "fired": False, "reason": "not-git-commit"}))
        return

    base_ref: str = config.get("base_ref", "origin/main")
    test_target: str = config.get("test_target", "test")

    # Run nx affected (catches: any test for any project that the
    # current diff touches must pass before commit)
    try:
        result = subprocess.run(
            ["pnpm", "exec", "nx", "affected", "-t", test_target, "--base=" + base_ref],
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=110,  # leave headroom inside the plugin's 120s timeout
        )
    except FileNotFoundError:
        # No pnpm/nx available — non-blocking warn
        print(json.dumps({
            "score": 0, "fired": False,
            "reason": "pnpm-or-nx-not-available; commit allowed (operator should configure)",
        }))
        return
    except subprocess.TimeoutExpired:
        print(json.dumps({
            "score": 1.0, "fired": True, "block": True,
            "reason": "nx-affected-timed-out — commit blocked; tests took too long",
            "axis": {"timeout_s": 110},
        }))
        return

    if result.returncode != 0:
        # nx affected failed — block the commit
        tail = (result.stderr or result.stdout or "")[-400:]
        print(json.dumps({
            "score": 1.0,
            "fired": True,
            "block": True,
            "reason": f"nx-affected-test-failed (target={test_target}, base={base_ref}); commit blocked. Tail of nx output:\n{tail}",
            "axis": {"exit_code": result.returncode, "test_target": test_target},
        }))
        return

    # All tests passed — allow the commit
    print(json.dumps({
        "score": 0.0,
        "fired": False,
        "reason": f"nx-affected-tests-pass (target={test_target})",
    }))


if __name__ == "__main__":
    main()
