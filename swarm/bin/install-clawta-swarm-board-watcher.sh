#!/usr/bin/env bash
# Register Clawta's OpenClaw swarm-board watcher cron.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LOCAL_BIN="$HOME/.local/bin"
OPENCLAW_BIN="${OPENCLAW_BIN:-$HOME/.vite-plus/bin/openclaw}"
JOB_NAME="clawta-swarm-board-watcher"
SWARM_TARGET="${SWARM_TARGET:-channel:1505613628286701588}"

if [[ ! -x "$OPENCLAW_BIN" ]]; then
  OPENCLAW_BIN="$(command -v openclaw || true)"
fi
if [[ -z "$OPENCLAW_BIN" || ! -x "$OPENCLAW_BIN" ]]; then
  echo "openclaw CLI not found" >&2
  exit 2
fi

mkdir -p "$LOCAL_BIN"
ln -sfn "$REPO_ROOT/swarm/bin/clawta-swarm-board-watcher" "$LOCAL_BIN/clawta-swarm-board-watcher"
echo "linked: $LOCAL_BIN/clawta-swarm-board-watcher → $REPO_ROOT/swarm/bin/clawta-swarm-board-watcher"

if "$OPENCLAW_BIN" cron list 2>/dev/null | grep -qE "[[:space:]]${JOB_NAME}([[:space:]]|$)"; then
  echo "openclaw cron: job '$JOB_NAME' already registered."
  exit 0
fi

"$OPENCLAW_BIN" cron add \
  --name "$JOB_NAME" \
  --description "Clawta swarm-board subscription: every 60s, detect ready swarm tickets assigned to clawta or * and post one #swarm receipt per unseen ticket." \
  --every 60s \
  --agent clawta \
  --session isolated \
  --light-context \
  --tools exec \
  --timeout-seconds 120 \
  --no-deliver \
  --message "Run exactly one command with exec using host=\"gateway\", security=\"full\", yieldMs=60000, timeout=90:

SWARM_TARGET=$SWARM_TARGET clawta-swarm-board-watcher

If exec is blocked, reply exactly 'blocked: <one-line reason>'. Otherwise reply exactly 'ok'. Do not retry or investigate; watcher failures must be loud in cron run history." \
  >/dev/null

echo "openclaw cron: '$JOB_NAME' registered (every 60s)."
