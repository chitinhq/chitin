#!/usr/bin/env bash
# install-claude-code-hook.sh — wire chitin governance into Claude Code.
#
# Claude Code's PreToolUse hook lives in ~/.claude/settings.json (JSON).
# Stdin shape is byte-compatible with the codex/gemini/hermes hooks
# (chitin-router-hook handles the dispatch). This script writes a
# PreToolUse entry pointing at chitin-router-hook --agent=claude-code,
# replacing any prior PreToolUse handlers (specifically capture.sh from
# the 2026-04-19 "Curie experiment 2 — Phase B" research probe that
# silently bypassed the gate for 17 days, which is exactly the gap
# this script closes).
#
# Idempotent: re-runs replace any prior PreToolUse block rather than
# appending dups. Other event handlers (UserPromptSubmit, PostToolUse,
# SessionEnd, SessionStart) are left untouched — they're not chitin-
# gated surfaces; if the operator wants those captured for research
# they can stay pointed at capture.sh independently.
#
# No-clobber: if settings.json is malformed JSON, refuses to write.
#
# Backs up the original file once per session (settings.json.before-
# install-claude-code-hook-<TS>) so the operator can restore manually.

set -euo pipefail

CLAUDE_SETTINGS="${CLAUDE_SETTINGS_PATH:-$HOME/.claude/settings.json}"

# Resolve the hook binary path. Same priority as install-codex-hook.sh:
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
  echo "install-claude-code-hook: chitin-router-hook not found" >&2
  echo "  Set CHITIN_ROUTER_HOOK_BIN, or install chitin first." >&2
  exit 2
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "install-claude-code-hook: jq required to safely edit JSON config" >&2
  exit 2
fi

mkdir -p "$(dirname "$CLAUDE_SETTINGS")"

# Seed an empty config if absent.
if [[ ! -f "$CLAUDE_SETTINGS" ]]; then
  echo "{}" > "$CLAUDE_SETTINGS"
fi

# Refuse to write if existing settings.json isn't valid JSON.
if ! jq empty "$CLAUDE_SETTINGS" 2>/dev/null; then
  echo "install-claude-code-hook: $CLAUDE_SETTINGS is not valid JSON; refusing to overwrite" >&2
  exit 2
fi

CHITIN_CMD="$HOOK_BIN --agent=claude-code"

# Idempotency check: if hooks.PreToolUse already has exactly our entry as
# its only handler, skip the rewrite (and the backup).
EXISTING_CMD=$(jq -r '.hooks.PreToolUse[0].hooks[0].command // ""' "$CLAUDE_SETTINGS" 2>/dev/null || echo "")
EXISTING_LEN=$(jq -r '.hooks.PreToolUse | length' "$CLAUDE_SETTINGS" 2>/dev/null || echo "0")
EXISTING_HOOKS=$(jq -r '.hooks.PreToolUse[0].hooks | length' "$CLAUDE_SETTINGS" 2>/dev/null || echo "0")

if [[ "$EXISTING_CMD" == "$CHITIN_CMD" && "$EXISTING_LEN" == "1" && "$EXISTING_HOOKS" == "1" ]]; then
  echo "install-claude-code-hook: chitin hook already present in $CLAUDE_SETTINGS (no-op)"
  exit 0
fi

# Backup before writing.
TS=$(date -u +%Y%m%dT%H%M%SZ)
BACKUP="${CLAUDE_SETTINGS}.before-install-claude-code-hook-${TS}"
cp -a "$CLAUDE_SETTINGS" "$BACKUP"
echo "install-claude-code-hook: backed up existing settings to $BACKUP"

# Rewrite hooks.PreToolUse to a single matcher-block pointing at chitin.
# Other event handlers are preserved as-is (capture.sh on
# hooks.UserPromptSubmit / hooks.PostToolUse / hooks.SessionEnd /
# hooks.SessionStart stay if present). The Claude Code schema nests all
# event handlers under .hooks (top-level .PreToolUse is ignored).
TMP=$(mktemp)
jq --arg cmd "$CHITIN_CMD" '
  .hooks //= {}
  | .hooks.PreToolUse = [
    {
      matcher: "",
      hooks: [
        { type: "command", command: $cmd }
      ]
    }
  ]
' "$CLAUDE_SETTINGS" > "$TMP"

# Sanity check the rewrite parses.
if ! jq empty "$TMP" 2>/dev/null; then
  echo "install-claude-code-hook: jq rewrite produced invalid JSON; aborting" >&2
  rm -f "$TMP"
  exit 2
fi

mv "$TMP" "$CLAUDE_SETTINGS"
echo "install-claude-code-hook: PreToolUse → $CHITIN_CMD"
