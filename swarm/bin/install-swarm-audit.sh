#!/usr/bin/env bash
# install-swarm-audit.sh — wire the daily swarm audit into systemd user.
#
# Idempotent: re-runs are safe and just refresh the unit files + symlink.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LOCAL_BIN="$HOME/.local/bin"
SYSD="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
ENV_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/chitin"
ENV_FILE="$ENV_DIR/swarm-audit.env"

mkdir -p "$LOCAL_BIN" "$SYSD" "$ENV_DIR"

ln -sfn "$REPO_ROOT/swarm/bin/swarm-audit" "$LOCAL_BIN/swarm-audit"
echo "linked: $LOCAL_BIN/swarm-audit → $REPO_ROOT/swarm/bin/swarm-audit"

install -m 0644 "$REPO_ROOT/swarm/systemd/swarm-audit.service" "$SYSD/swarm-audit.service"
install -m 0644 "$REPO_ROOT/swarm/systemd/swarm-audit.timer"   "$SYSD/swarm-audit.timer"

# Bootstrap env file template if it doesn't exist yet. Operator fills in
# the Discord channel id (or removes the file to keep delivery local).
if [[ ! -f "$ENV_FILE" ]]; then
  cat > "$ENV_FILE" <<'ENV'
# Daily swarm-audit configuration. Edit and save; changes pick up on
# next timer fire.

# Discord channel id to post the summary to. Leave commented to print to
# the service journal instead of Discord (useful for first-run sanity).
# SWARM_AUDIT_DISCORD_CHANNEL=1503...

# Which openclaw agent runs the summarization step. clawta gives you
# gpt-5.5 reasoning today; main is gpt-5.4. Leave unset for default.
# SWARM_AUDIT_AGENT=clawta

# Override the audit window (in hours). Default 24.
# SWARM_AUDIT_HOURS=24
ENV
  echo "created env template: $ENV_FILE"
fi

systemctl --user daemon-reload
systemctl --user enable --now swarm-audit.timer

echo ""
echo "swarm-audit.timer enabled (next fire: daily 08:00 America/Detroit)."
echo "  status:    systemctl --user status swarm-audit.timer"
echo "  fire now:  systemctl --user start swarm-audit.service"
echo "  journal:   journalctl --user -u swarm-audit.service -n 50 --no-pager"
echo "  config:    $ENV_FILE"
