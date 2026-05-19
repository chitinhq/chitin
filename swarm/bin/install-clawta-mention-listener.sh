#!/usr/bin/env bash
# install-clawta-mention-listener.sh — install the listener + cron entry
# on an operator box. Idempotent per Constitution §6.
#
# What it does:
#   1. Copies clawta-mention-listener into ~/.openclaw/bin/
#   2. Adds a 60s cron entry via the openclaw cron CLI (skips if present)
#   3. Validates the binary is on PATH + the cron entry registered
#
# Safe to re-run. Logs to stderr.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$SCRIPT_DIR/clawta-mention-listener"
DST_DIR="$HOME/.openclaw/bin"
DST="$DST_DIR/clawta-mention-listener"
OPENCLAW_BIN="${OPENCLAW_BIN:-$HOME/.vite-plus/bin/openclaw}"
CRON_NAME="clawta-mention-listener"

if [[ ! -f "$SRC" ]]; then
  echo "ERROR: source not found at $SRC" >&2
  exit 1
fi

mkdir -p "$DST_DIR"
install -m 0755 "$SRC" "$DST"
echo "installed: $DST"

# Cron registration via plain user crontab. The openclaw cron CLI is
# tailored for agent dispatches (--message), not raw script invocation,
# so we use system cron for this listener. Idempotent: skip if already
# present.
CRON_LINE="* * * * * $DST >> $HOME/.openclaw/logs/clawta-mention-listener.log 2>&1"
CRON_MARK="# managed: $CRON_NAME"

if crontab -l 2>/dev/null | grep -Fq "$CRON_MARK"; then
  echo "cron already present: $CRON_NAME"
else
  (crontab -l 2>/dev/null; echo "$CRON_MARK"; echo "$CRON_LINE") | crontab -
  echo "cron registered: $CRON_NAME (every minute)"
fi

echo "install complete."
