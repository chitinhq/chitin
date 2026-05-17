#!/usr/bin/env bash
# install-architecture-audit.sh — wire the weekly architecture audit into
# systemd user.
#
# Idempotent: re-runs are safe and just refresh the unit files + symlink.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LOCAL_BIN="$HOME/.local/bin"
SYSD="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
ENV_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/chitin"
ENV_FILE="$ENV_DIR/architecture-audit.env"

mkdir -p "$LOCAL_BIN" "$SYSD" "$ENV_DIR"

ln -sfn "$REPO_ROOT/swarm/bin/architecture-audit" "$LOCAL_BIN/architecture-audit"
echo "linked: $LOCAL_BIN/architecture-audit → $REPO_ROOT/swarm/bin/architecture-audit"

install -m 0644 "$REPO_ROOT/swarm/systemd/architecture-audit.service" "$SYSD/architecture-audit.service"
install -m 0644 "$REPO_ROOT/swarm/systemd/architecture-audit.timer"   "$SYSD/architecture-audit.timer"

# Bootstrap env file template if it doesn't exist yet. Operator edits to
# adjust budget / effort / skip flags. Changes pick up on next timer fire.
if [[ ! -f "$ENV_FILE" ]]; then
  cat > "$ENV_FILE" <<'ENV'
# Weekly architecture audit configuration. Edit and save; changes pick up
# on the next timer fire.

# Hard cap on claude --print spend per run. Default 5.
# ARCH_AUDIT_BUDGET_USD=5

# Effort level for the claude --print run: low|medium|high|xhigh|max.
# Default high. Lower if budget is tight; max if you want a thorough sweep.
# ARCH_AUDIT_EFFORT=high

# Workspace dir for sibling repo discovery. Default: parent of board workspace_root.
# ARCH_AUDIT_WORKSPACE_DIR=/home/operator/workspace

# Primary board checkout. Default: board config workspace_root.
# ARCH_AUDIT_REPO_DIR=/home/operator/workspace/repo-name

# Set to 1 to write the report locally without opening a PR (debug runs).
# ARCH_AUDIT_SKIP_PR=0

# Set to 1 to write the report locally without creating kanban tickets.
# ARCH_AUDIT_SKIP_TICKETS=0
ENV
  echo "created env template: $ENV_FILE"
fi

systemctl --user daemon-reload
systemctl --user enable --now architecture-audit.timer

echo ""
echo "architecture-audit.timer enabled (next fire: Sunday 06:00 America/Detroit)."
echo "  status:    systemctl --user status architecture-audit.timer"
echo "  next:      systemctl --user list-timers architecture-audit.timer"
echo "  fire now:  systemctl --user start architecture-audit.service"
echo "  journal:   journalctl --user -u architecture-audit.service -n 100 --no-pager"
echo "  log:       tail -f ~/.openclaw/logs/architecture-audit.log"
echo "  config:    $ENV_FILE"
