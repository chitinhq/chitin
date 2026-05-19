#!/usr/bin/env bash
# install-mini-mention-listener.sh — install the listener + cron entry
# on an operator box. Idempotent per Constitution §6.
#
# What it does:
#   1. Copies mini-mention-listener into ~/.openclaw/bin/
#   2. Registers a 1-minute cron entry that runs the listener with --once
#   3. Validates the bus DB path is reachable (warns if missing — bridge
#      will populate it on first @mini)
#
# Box convention is cron via crontab (see install-clawta-mention-listener.sh).
# Spec 039's `infra/systemd/` aspiration was retired in slice 1.1 to match
# what's actually deployed.
#
# Safe to re-run. Logs to stderr.

set -euo pipefail

DRY_RUN=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help)
      echo "Usage: $(basename "$0") [--dry-run]"
      echo "  --dry-run  Print actions without copying files or touching crontab"
      exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$SCRIPT_DIR/mini-mention-listener"
DST_DIR="$HOME/.openclaw/bin"
DST="$DST_DIR/mini-mention-listener"
LOG_DIR="$HOME/.openclaw/logs"
BUS_DB="${AGENT_BUS_DB:-$HOME/.chitin/agent-bus/bus.db}"
CRON_NAME="mini-mention-listener"

if [[ ! -f "$SRC" ]]; then
  echo "ERROR: source not found at $SRC" >&2
  exit 1
fi

if [[ $DRY_RUN -eq 1 ]]; then
  echo "[dry-run] would: mkdir -p $DST_DIR $LOG_DIR"
  echo "[dry-run] would: install -m 0755 $SRC $DST"
else
  mkdir -p "$DST_DIR" "$LOG_DIR"
  install -m 0755 "$SRC" "$DST"
  echo "installed: $DST"
fi

# Cron registration. --once mode so cron schedules the cadence and the
# process always exits — no daemon-mode accumulation.
CRON_LINE="* * * * * $DST --once >> $LOG_DIR/mini-mention-listener.log 2>&1"
CRON_MARK="# managed: $CRON_NAME"

if [[ $DRY_RUN -eq 1 ]]; then
  echo "[dry-run] would register cron entry:"
  echo "          $CRON_MARK"
  echo "          $CRON_LINE"
elif crontab -l 2>/dev/null | grep -Fq "$CRON_MARK"; then
  echo "cron already present: $CRON_NAME"
else
  (crontab -l 2>/dev/null; echo "$CRON_MARK"; echo "$CRON_LINE") | crontab -
  echo "cron registered: $CRON_NAME (every minute, --once mode)"
fi

# Bus DB sanity. Don't fail — the agent-bus server creates the file on
# first write; the listener handles a missing DB gracefully (logs and
# returns zero counts).
if [[ ! -f "$BUS_DB" ]]; then
  echo "INFO: bus DB not yet present at $BUS_DB"
  echo "  The listener will log 'bus_db_missing' until Hermes' bus<->Discord"
  echo "  bridge writes the first row. No action needed if Hermes is running."
else
  echo "bus DB: $BUS_DB"
fi

# Mini state root sanity. Listener resolves goal_ids by scanning here.
STATE_ROOT="${MINI_STATE_ROOT:-$HOME/.swarm/octi}"
if [[ ! -d "$STATE_ROOT" ]]; then
  echo "INFO: Mini state root not yet present at $STATE_ROOT"
  echo "  Run install-mini.sh first, or open a Mini session — the dir is"
  echo "  created lazily. The listener will log 'no_match' for every"
  echo "  inbound message until at least one goal exists."
else
  goals=$(find "$STATE_ROOT" -maxdepth 1 -mindepth 1 -type d 2>/dev/null | wc -l)
  echo "state root: $STATE_ROOT ($goals goal-id$([ "$goals" = "1" ] || echo s))"
fi

echo "install complete. Try: $DST --dry-run"
