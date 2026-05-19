#!/usr/bin/env bash
# install-mini.sh — wire `mini` and `octi-worker` onto PATH and verify
# kitty remote control. Idempotent per constitution §6.
#
# What it does:
#   1. Symlinks swarm/bin/mini and swarm/bin/octi-worker into ~/.local/bin/
#   2. Verifies kitty is installed and remote-control is enabled
#   3. Warns if OCTI_DISCORD_WEBHOOK_URL is not set (does not fail)
#   4. Creates ~/.swarm/octi/.gitkeep so the default state root exists
#
# Safe to re-run.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LOCAL_BIN="$HOME/.local/bin"
STATE_ROOT_DEFAULT="$HOME/.swarm/octi"

mkdir -p "$LOCAL_BIN"

for tool in mini octi-worker; do
  src="$REPO_ROOT/swarm/bin/$tool"
  if [[ ! -f "$src" ]]; then
    echo "ERROR: missing source: $src" >&2
    exit 1
  fi
  chmod +x "$src"
  ln -sfn "$src" "$LOCAL_BIN/$tool"
  echo "linked: $LOCAL_BIN/$tool -> $src"
done

mkdir -p "$STATE_ROOT_DEFAULT"
touch "$STATE_ROOT_DEFAULT/.gitkeep"
echo "state root: $STATE_ROOT_DEFAULT"

# kitty checks
if ! command -v kitty >/dev/null 2>&1; then
  echo "WARNING: kitty terminal not on PATH; install kitty before using mini." >&2
  exit 0
fi

if ! kitty @ ls >/dev/null 2>&1; then
  cat >&2 <<EOF
WARNING: kitty remote control is not reachable. Add to ~/.config/kitty/kitty.conf:

  allow_remote_control yes
  listen_on unix:/tmp/kitty-rc

Then restart kitty. mini will not work until this succeeds.
EOF
  exit 2
fi
echo "kitty: remote control OK."

# webhook
if [[ -z "${OCTI_DISCORD_WEBHOOK_URL:-}" ]]; then
  echo "INFO: OCTI_DISCORD_WEBHOOK_URL is not set."
  echo "  Per-session override available at .swarm/octi/<goal-id>/webhook.url"
  echo "  \`mini watch\` requires a webhook URL."
fi

echo "install complete. Try: mini --help"
