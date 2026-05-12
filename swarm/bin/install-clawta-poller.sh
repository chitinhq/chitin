#!/usr/bin/env bash
# install-clawta-poller.sh — wire the poller into the user systemd path.
#
# Idempotent. Run after pulling main, or after editing the poller /
# unit files.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
SYSTEMD_USER_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
LOCAL_BIN="$HOME/.local/bin"

mkdir -p "$SYSTEMD_USER_DIR" "$LOCAL_BIN"

# Symlink the poller script into ~/.local/bin so systemd PATH finds it
ln -sfn "$REPO_ROOT/swarm/bin/clawta-poller" "$LOCAL_BIN/clawta-poller"

# Install service + timer units (copy, not symlink — systemd doesn't
# always handle symlinked units well across upgrades)
install -m 0644 "$REPO_ROOT/swarm/systemd/clawta-poller.service" "$SYSTEMD_USER_DIR/clawta-poller.service"
install -m 0644 "$REPO_ROOT/swarm/systemd/clawta-poller.timer"   "$SYSTEMD_USER_DIR/clawta-poller.timer"

systemctl --user daemon-reload
systemctl --user enable --now clawta-poller.timer

echo "clawta-poller installed."
echo "  systemctl --user status clawta-poller.timer"
echo "  journalctl --user -u clawta-poller.service -f"
echo "  tail -F ~/.openclaw/logs/clawta-poller.log"
