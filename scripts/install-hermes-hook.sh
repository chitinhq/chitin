#!/usr/bin/env bash
# install-hermes-hook.sh — wire chitin-router-hook into hermes's
# `pre_tool_call` shell-hook surface.
#
# Hermes's hook protocol is byte-compatible with Claude Code's PreToolUse
# (per ~/.hermes/hermes-agent/agent/shell_hooks.py docstring), so the
# chitin-router-hook bash wrapper already speaks it. The only piece that
# differs per-driver is tool-name normalization, handled by the new
# internal/driver/hermes Go package.
#
# Idempotent: re-runs replace any prior hermes block in the operator's
# config rather than appending dups.

set -euo pipefail

REPO="${CHITIN_REPO:-$HOME/workspace/chitin}"
HOOK_BIN="${CHITIN_ROUTER_HOOK_BIN:-$REPO/bin/chitin-router-hook}"
HERMES_CONFIG="${HERMES_CONFIG:-$HOME/.hermes/config.yaml}"

if [[ ! -x "$HOOK_BIN" ]]; then
  echo "install-hermes-hook: chitin-router-hook missing or not executable at $HOOK_BIN" >&2
  exit 1
fi

if [[ ! -f "$HERMES_CONFIG" ]]; then
  echo "install-hermes-hook: hermes config not found at $HERMES_CONFIG (skipping; install when hermes is configured)" >&2
  exit 0
fi

# Edit YAML via Python — safer than sed for nested structures and
# preserves comments better than yq's YAML 1.2 strictness. The hermes
# side already requires Python (the hermes_cli is a Python package),
# so this dependency is free.
python3 - "$HERMES_CONFIG" "$HOOK_BIN" <<'PY'
import sys
import yaml

config_path = sys.argv[1]
hook_bin = sys.argv[2]

with open(config_path, "r") as f:
    raw = f.read()

data = yaml.safe_load(raw) or {}

hooks = data.get("hooks")
if not isinstance(hooks, dict):
    hooks = {}
    data["hooks"] = hooks

entries = hooks.get("pre_tool_call")
if not isinstance(entries, list):
    entries = []
    hooks["pre_tool_call"] = entries

# Marker so we can find and replace our entry on re-run without
# clobbering operator-installed hooks. Same convention as the codex
# and gemini installers.
chitin_command = f"{hook_bin} --agent=hermes"

# Drop any prior chitin entries (matched by command prefix) so the
# install is idempotent. Operator-added entries with different
# commands stay.
entries[:] = [
    e for e in entries
    if not (
        isinstance(e, dict)
        and isinstance(e.get("command"), str)
        and e["command"].startswith(hook_bin)
    )
]

# Append the canonical chitin entry. matcher='' = match all tool names
# (chitin's normalizer handles the per-tool routing). timeout 30s
# matches the codex installer.
entries.append({
    "command": chitin_command,
    "matcher": "",
    "timeout": 30,
})

with open(config_path, "w") as f:
    yaml.safe_dump(data, f, default_flow_style=False, sort_keys=False)

print(f"install-hermes-hook: wired {chitin_command} into {config_path}")
PY

# Also add to the shell-hooks allowlist so first-run consent isn't
# required (the agent service won't have a TTY for the prompt).
ALLOWLIST="$HOME/.hermes/shell-hooks-allowlist.json"
mkdir -p "$(dirname "$ALLOWLIST")"
python3 - "$ALLOWLIST" "$HOOK_BIN" <<'PY'
import json
import os
import sys

path = sys.argv[1]
hook_bin = sys.argv[2]
key = f"pre_tool_call::{hook_bin} --agent=hermes"

if os.path.exists(path):
    with open(path) as f:
        try:
            data = json.load(f)
        except json.JSONDecodeError:
            data = {}
else:
    data = {}

if not isinstance(data, dict):
    data = {}

data[key] = True

with open(path, "w") as f:
    json.dump(data, f, indent=2, sort_keys=True)

print(f"install-hermes-hook: allowlisted {key} in {path}")
PY

echo
echo "Verify with:"
echo "  grep -A5 'pre_tool_call' $HERMES_CONFIG"
echo "  jq . $ALLOWLIST"
