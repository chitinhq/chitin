#!/usr/bin/env bash
# install — wire swarm-tickets-for.sh into Claude Code SessionStart.
#
# Per constitution §6: every operator-box script needs tracked source +
# idempotent installer. This script is the installer for the source at
# scripts/swarm-sessionstart-hook/swarm-tickets-for.sh.
#
# Idempotent: re-running produces the same result. Safe to invoke
# from `chitin rollout` or any sync flow.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK_SRC="$SCRIPT_DIR/swarm-tickets-for.sh"
HOOK_DEST_DIR="$HOME/.claude/hooks"
HOOK_DEST="$HOOK_DEST_DIR/swarm-tickets.sh"
SETTINGS="$HOME/.claude/settings.json"

[[ -x "$HOOK_SRC" ]] || chmod +x "$HOOK_SRC"
mkdir -p "$HOOK_DEST_DIR"

# Symlink the source so updates propagate
if [[ -L "$HOOK_DEST" ]] && [[ "$(readlink "$HOOK_DEST")" == "$HOOK_SRC" ]]; then
    echo "[install] hook symlink already correct"
else
    rm -f "$HOOK_DEST"
    ln -s "$HOOK_SRC" "$HOOK_DEST"
    echo "[install] linked $HOOK_DEST -> $HOOK_SRC"
fi

# Wire into Claude Code settings.json SessionStart hooks. We use jq to
# do the merge so we don't clobber existing hooks.
if ! command -v jq >/dev/null 2>&1; then
    echo "[install] WARN: jq not available; you'll need to add the SessionStart hook manually:" >&2
    echo "[install]   hook command: $HOOK_DEST red" >&2
    echo "[install]   matcher: empty (fires on every session start)" >&2
    exit 0
fi

if [[ ! -f "$SETTINGS" ]]; then
    echo "{}" > "$SETTINGS"
fi

# Read current hooks. Idempotent check: skip if our hook is already wired.
current=$(jq --arg cmd "$HOOK_DEST red" \
    '.hooks.SessionStart // [] | map(select(.hooks[]?.command == $cmd)) | length' \
    "$SETTINGS")

if [[ "$current" -gt 0 ]]; then
    echo "[install] SessionStart hook already wired"
    exit 0
fi

# Append our hook to .hooks.SessionStart array
tmp=$(mktemp)
jq --arg cmd "$HOOK_DEST red" '
    .hooks //= {} |
    .hooks.SessionStart //= [] |
    .hooks.SessionStart += [{
        "matcher": "",
        "hooks": [{"type": "command", "command": $cmd}]
    }]
' "$SETTINGS" > "$tmp" && mv "$tmp" "$SETTINGS"

echo "[install] SessionStart hook added to $SETTINGS"
echo "[install] try it now: $HOOK_DEST red"
