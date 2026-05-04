#!/usr/bin/env bash
# install-gemini-hook.sh — wire chitin governance into Gemini CLI.
#
# Gemini CLI fires a BeforeTool hook whose stdin shape is byte-
# identical to Claude Code's PreToolUse (only the event name +
# tool names differ). The kernel's gemini driver remaps the tool
# names; this script installs the hook block into
# ~/.gemini/settings.json so the kernel actually gets called.
#
# Idempotent: runs the merge through jq so re-running adds no
# duplicates; can be invoked from chitin-kernel-redeploy.timer
# alongside the kernel rebuild.
#
# Falls open: if jq is missing or ~/.gemini/settings.json is
# unparseable, prints a clear error and exits non-zero rather
# than overwriting the operator's config.

set -euo pipefail

SETTINGS_PATH="${GEMINI_SETTINGS_PATH:-$HOME/.gemini/settings.json}"
HOOK_BIN="${CHITIN_ROUTER_HOOK_BIN:-/home/red/workspace/chitin/bin/chitin-router-hook}"

if ! command -v jq >/dev/null; then
  echo "install-gemini-hook: jq is required (apt install jq)" >&2
  exit 2
fi

if [[ ! -x "$HOOK_BIN" ]]; then
  echo "install-gemini-hook: hook binary not found at $HOOK_BIN" >&2
  echo "  set CHITIN_ROUTER_HOOK_BIN to override the default path." >&2
  exit 2
fi

mkdir -p "$(dirname "$SETTINGS_PATH")"

# Seed an empty settings file if absent.
if [[ ! -f "$SETTINGS_PATH" ]]; then
  echo '{}' > "$SETTINGS_PATH"
fi

# Validate parseability before mutation — never clobber a config
# we can't read.
if ! jq empty "$SETTINGS_PATH" 2>/dev/null; then
  echo "install-gemini-hook: $SETTINGS_PATH is not valid JSON; refusing to overwrite" >&2
  exit 3
fi

# Build the hook block. Idempotency: jq merges, then we drop
# duplicates by command path so re-running this script doesn't
# add the chitin hook twice.
HOOK_BLOCK=$(jq -n --arg cmd "$HOOK_BIN --agent=gemini" '
  {
    matcher: "",
    hooks: [
      { type: "command", command: $cmd }
    ]
  }')

TMP=$(mktemp)
trap 'rm -f "$TMP"' EXIT

jq --argjson hookBlock "$HOOK_BLOCK" --arg cmd "$HOOK_BIN --agent=gemini" '
  .hooks //= {} |
  .hooks.BeforeTool //= [] |
  # Drop any existing entry whose hook command matches ours, then add.
  .hooks.BeforeTool |= (
    map(select(
      [.hooks[]? | select(.type=="command" and .command==$cmd)] | length == 0
    )) + [$hookBlock]
  )
' "$SETTINGS_PATH" > "$TMP"

mv "$TMP" "$SETTINGS_PATH"

echo "install-gemini-hook: wired $HOOK_BIN --agent=gemini into $SETTINGS_PATH"
echo
echo "Verify with:"
echo "  jq '.hooks.BeforeTool' $SETTINGS_PATH"
