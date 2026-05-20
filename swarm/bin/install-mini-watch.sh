#!/usr/bin/env bash
# install-mini-watch.sh — install the mini-watch lifecycle on an operator
# box. Idempotent per Constitution §6.
#
# What it does:
#   1. Symlinks swarm/bin/mini into ~/.local/bin/ (if not already linked)
#   2. Creates the ~/.swarm/octi/ state root (if not present)
#   3. Verifies kitty remote control is reachable (warns only)
#   4. Validates the webhook URL is configured (warns only)
#
# The watcher is NOT a standalone daemon. `mini open` auto-starts
# `mini watch` as a background process per session. The watcher's
# lifecycle is managed via watch.pid in the session state dir —
# `mini stop` kills it. No cron entry needed.
#
# This installer exists because constitution §4 requires any
# operator-box script to ship its installer in the same PR.
#
# Usage:
#   bash swarm/bin/install-mini-watch.sh [--dry-run]
#
# Safe to re-run.

set -euo pipefail

DRY_RUN=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help)
      echo "Usage: $(basename "$0") [--dry-run]"
      echo "  Install the mini-watch lifecycle. Idempotent."
      exit 0 ;;
    *) echo "ERROR: unknown arg: $1" >&2; exit 2 ;;
  esac
  shift
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MINI_BIN="$REPO_ROOT/swarm/bin/mini"
LOCAL_BIN="$HOME/.local/bin"
STATE_ROOT_DEFAULT="$HOME/.swarm/octi"

if [[ ! -f "$MINI_BIN" ]]; then
  echo "ERROR: mini CLI not found at $MINI_BIN" >&2
  exit 1
fi

# Ensure mini is linked into PATH (shared with install-mini.sh)
if [[ $DRY_RUN -eq 1 ]]; then
  echo "[dry-run] would: mkdir -p $LOCAL_BIN"
  echo "[dry-run] would: ln -sfn $MINI_BIN $LOCAL_BIN/mini"
else
  mkdir -p "$LOCAL_BIN"
  chmod +x "$MINI_BIN"
  ln -sfn "$MINI_BIN" "$LOCAL_BIN/mini"
  echo "linked: $LOCAL_BIN/mini -> $MINI_BIN"
fi

# State root
if [[ $DRY_RUN -eq 1 ]]; then
  echo "[dry-run] would: mkdir -p $STATE_ROOT_DEFAULT"
else
  mkdir -p "$STATE_ROOT_DEFAULT"
  touch "$STATE_ROOT_DEFAULT/.gitkeep"
  echo "state root: $STATE_ROOT_DEFAULT"
fi

# Kitty check (non-fatal)
if ! command -v kitty >/dev/null 2>&1; then
  echo "WARNING: kitty terminal not on PATH; install kitty before using mini." >&2
else
  if kitty @ ls >/dev/null 2>&1; then
    echo "kitty: remote control OK."
  else
    cat >&2 <<EOF
WARNING: kitty remote control is not reachable. Add to ~/.config/kitty/kitty.conf:

  allow_remote_control yes
  listen_on unix:/tmp/kitty-rc

Then restart kitty. mini watch will not work until this succeeds.
EOF
  fi
fi

# Webhook check (non-fatal)
if [[ -z "${OCTI_DISCORD_WEBHOOK_URL:-}" ]]; then
  echo "INFO: OCTI_DISCORD_WEBHOOK_URL is not set. Set it so mini watch can"
  echo "  post per-session event threads to Discord. Three ways to wire:"
  echo "  1. Export OCTI_DISCORD_WEBHOOK_URL=<webhook url>"
  echo "  2. Use hermes convention: export MINI_DISCORD_CHANNEL_ID=<channel id>"
  echo "     and ensure ~/.hermes/.env has DISCORD_WEBHOOK_URL_<id>=<url>"
  echo "  3. Per-session override: echo '<url>' > ~/.swarm/octi/<goal-id>/webhook.url"
else
  echo "webhook: OCTI_DISCORD_WEBHOOK_URL is set."
fi

# Bot token (needed for thread creation on non-forum channels)
if [[ -z "${DISCORD_BOT_TOKEN:-}" ]]; then
  echo "INFO: DISCORD_BOT_TOKEN is not set. Thread creation on regular"
  echo "  text channels requires a bot token. Without it, mini falls back"
  echo "  to channel-level posts (S2-R4). Forum channels work without it."
else
  echo "bot token: DISCORD_BOT_TOKEN is set (thread creation enabled)."
fi

echo ""
echo "mini-watch lifecycle install complete."
echo "  - 'mini open' auto-starts 'mini watch' per session"
echo "  - 'mini stop' kills the watcher via watch.pid"
echo "  - No separate cron or daemon is needed"