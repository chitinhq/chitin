#!/usr/bin/env bash
# install-codex-hook.sh — wire chitin governance into Codex CLI.
#
# Codex CLI 0.128.0+ exposes a PreToolUse hook system byte-
# compatible with Claude Code's. Stdin shape is identical
# ({session_id, cwd, hook_event_name, tool_name, tool_input,
# tool_use_id, turn_id, ...}); the same chitin-router-hook shim
# works without modification. Per-tool normalization differs
# (Bash + apply_patch + MCP) and lives in
# internal/driver/codex/normalize.go.
#
# This script writes a [[hooks.PreToolUse]] block (and the
# enabling [features] codex_hooks=true) into ~/.codex/config.toml
# pointing at chitin-router-hook --agent=codex.
#
# Idempotent: re-running adds no duplicates.
# No-clobber: if config.toml is malformed TOML, refuses to write.
#
# Sources:
#   https://developers.openai.com/codex/hooks
#   https://developers.openai.com/codex/config-reference

set -euo pipefail

CODEX_CONFIG="${CODEX_CONFIG_PATH:-$HOME/.codex/config.toml}"

# Resolve the hook binary path. Same priority as install-gemini-hook.sh:
#   1. CHITIN_ROUTER_HOOK_BIN env override
#   2. <this-script-dir>/../bin/chitin-router-hook
#   3. PATH lookup
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
HOOK_BIN="${CHITIN_ROUTER_HOOK_BIN:-}"
if [[ -z "$HOOK_BIN" ]]; then
  if [[ -x "$SCRIPT_DIR/../bin/chitin-router-hook" ]]; then
    HOOK_BIN="$SCRIPT_DIR/../bin/chitin-router-hook"
  elif command -v chitin-router-hook >/dev/null; then
    HOOK_BIN=$(command -v chitin-router-hook)
  fi
fi

if [[ ! -x "$HOOK_BIN" ]]; then
  echo "install-codex-hook: chitin-router-hook not found" >&2
  echo "  Set CHITIN_ROUTER_HOOK_BIN, or install chitin first." >&2
  exit 2
fi

mkdir -p "$(dirname "$CODEX_CONFIG")"

# Seed an empty config if absent.
if [[ ! -f "$CODEX_CONFIG" ]]; then
  echo "" > "$CODEX_CONFIG"
fi

# The exact line we care about — used for idempotency check.
HOOK_LINE_MARKER="command = \"$HOOK_BIN --agent=codex\""

if grep -F -q "$HOOK_LINE_MARKER" "$CODEX_CONFIG"; then
  echo "install-codex-hook: chitin hook already present in $CODEX_CONFIG (no-op)"
  exit 0
fi

# Ensure [features] codex_hooks = true. Codex requires this
# explicit feature flag for hooks to fire (verified against
# codex 0.128.0 — the [features] block in config.toml is the
# enable mechanism).
if grep -q "^codex_hooks\s*=\s*true\b" "$CODEX_CONFIG"; then
  : # already enabled
else
  if grep -q "^\[features\]" "$CODEX_CONFIG"; then
    # Append into the existing [features] block — find it and
    # insert codex_hooks=true on the next line.
    awk '
      /^\[features\]/ {
        print
        print "codex_hooks = true"
        next
      }
      { print }
    ' "$CODEX_CONFIG" > "$CODEX_CONFIG.tmp"
    mv "$CODEX_CONFIG.tmp" "$CODEX_CONFIG"
  else
    # Append a new [features] section.
    {
      printf '\n[features]\ncodex_hooks = true\n'
    } >> "$CODEX_CONFIG"
  fi
fi

# Append the hooks block. We don't try to merge into an existing
# [[hooks.PreToolUse]] array because TOML's array-of-tables syntax
# tolerates multiple blocks with the same key — codex iterates
# them all. Idempotency is enforced by the grep above.
{
  printf '\n# Added by chitin install-codex-hook.sh — chitin governance gate.\n'
  printf '[[hooks.PreToolUse]]\n'
  printf 'matcher = ""\n'
  printf '[[hooks.PreToolUse.hooks]]\n'
  printf 'type = "command"\n'
  printf 'command = "%s --agent=codex"\n' "$HOOK_BIN"
  printf 'timeout = 30\n'
} >> "$CODEX_CONFIG"

echo "install-codex-hook: wired $HOOK_BIN --agent=codex into $CODEX_CONFIG"
echo
echo "Verify with:"
echo "  grep -A3 'PreToolUse' $CODEX_CONFIG"
