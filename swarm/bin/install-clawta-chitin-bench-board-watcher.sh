#!/usr/bin/env bash
# install-clawta-chitin-bench-board-watcher.sh — register an openclaw cron
# that subscribes Clawta to the chitin-bench kanban board (the chitin-bench
# emitter writes tickets here on failures).
#
# Reuses the existing parameterized watcher
# (``swarm/bin/clawta-swarm-board-watcher``); we just point its
# KANBAN_BOARD env var at "chitin-bench" instead of "swarm".
#
# Idempotent: re-running detects an existing job and exits 0.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
WATCHER_SRC="$REPO_ROOT/swarm/bin/clawta-swarm-board-watcher"
LOCAL_BIN="$HOME/.local/bin"
OPENCLAW_BIN="${OPENCLAW_BIN:-$HOME/.vite-plus/bin/openclaw}"
JOB_NAME="clawta-chitin-bench-board-watcher"
# Operator's #chitin-bench Discord channel ID. Override via env if needed.
CHITIN_BENCH_TARGET="${CHITIN_BENCH_TARGET:-channel:1505613628286701588}"

if [[ ! -x "$OPENCLAW_BIN" ]]; then
    OPENCLAW_BIN="$(command -v openclaw || true)"
fi
if [[ -z "$OPENCLAW_BIN" || ! -x "$OPENCLAW_BIN" ]]; then
    echo "openclaw CLI not found" >&2
    exit 2
fi
if [[ ! -x "$WATCHER_SRC" ]]; then
    echo "watcher script not found: $WATCHER_SRC" >&2
    exit 2
fi

mkdir -p "$LOCAL_BIN"
ln -sfn "$WATCHER_SRC" "$LOCAL_BIN/clawta-swarm-board-watcher"

if "$OPENCLAW_BIN" cron list 2>/dev/null | grep -qE "[[:space:]]${JOB_NAME}([[:space:]]|$)"; then
    echo "openclaw cron: '$JOB_NAME' already registered."
    exit 0
fi

"$OPENCLAW_BIN" cron add \
    --name "$JOB_NAME" \
    --description "Clawta subscription to the chitin-bench kanban board: every 60s, detect ready tickets and post one #chitin-bench receipt per unseen ticket." \
    --every 60s \
    --agent clawta \
    --session isolated \
    --light-context \
    --tools exec \
    --timeout-seconds 120 \
    --no-deliver \
    --message "Run exactly one command with exec using host=\"gateway\", security=\"full\", yieldMs=60000, timeout=90:

KANBAN_BOARD=chitin-bench SWARM_TARGET=$CHITIN_BENCH_TARGET clawta-swarm-board-watcher

If exec is blocked, reply exactly 'blocked: <one-line reason>'. Otherwise reply exactly 'ok'. Watcher failures must be loud in cron run history; do not retry or investigate." \
    >/dev/null

echo "openclaw cron: '$JOB_NAME' registered (every 60s, board=chitin-bench)."
