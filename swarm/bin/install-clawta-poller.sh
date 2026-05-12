#!/usr/bin/env bash
# install-clawta-poller.sh — wire the clawta-poller into the openclaw cron
# substrate (primary path), or fall back to systemd for openclaw-less boxes.
#
# Idempotent. Re-running this script does not double-register the cron job
# or duplicate the systemd unit.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
LOCAL_BIN="$HOME/.local/bin"

mode="${1:-openclaw}"

usage() {
  cat <<'USAGE'
Usage: install-clawta-poller.sh [openclaw|systemd|both]

  openclaw   Register clawta-kanban-poller as an openclaw cron job (default).
             Visible in `openclaw cron list`. Lives in openclaw's substrate.
             Requires openclaw + a running gateway.
  systemd    Install a user systemd timer firing every 2 minutes.
             Standalone path for boxes without openclaw.
  both       Install both. Useful for dev boxes that want belt-and-suspenders.

The poller script itself is symlinked into ~/.local/bin in all modes so it's
reachable from cron/systemd PATH and manual invocation.
USAGE
}

case "${mode}" in
  -h|--help) usage; exit 0 ;;
  openclaw|systemd|both) ;;
  *) echo "unknown mode: ${mode}"; usage; exit 2 ;;
esac

mkdir -p "$LOCAL_BIN"
ln -sfn "$REPO_ROOT/swarm/bin/clawta-poller" "$LOCAL_BIN/clawta-poller"
echo "linked: $LOCAL_BIN/clawta-poller → $REPO_ROOT/swarm/bin/clawta-poller"

# Also ensure kanban-flow is reachable; some boxes may have installed the
# poller before kanban-flow's symlink.
if [[ -x "$REPO_ROOT/scripts/kanban-flow" ]]; then
  ln -sfn "$REPO_ROOT/scripts/kanban-flow" "$LOCAL_BIN/kanban-flow"
  echo "linked: $LOCAL_BIN/kanban-flow → $REPO_ROOT/scripts/kanban-flow"
fi

install_openclaw_cron() {
  if ! command -v openclaw >/dev/null 2>&1; then
    echo "openclaw: CLI not on PATH; skipping openclaw cron registration." >&2
    return 1
  fi
  # Idempotent: if a job with this name already exists, skip add.
  if openclaw cron list 2>/dev/null | grep -q '^[a-f0-9-]\+ clawta-kanban-poller '; then
    echo "openclaw cron: job 'clawta-kanban-poller' already registered."
    return 0
  fi
  openclaw cron add \
    --name "clawta-kanban-poller" \
    --description "Autonomous kanban dispatch tick. Reads ready terminal-lane tickets, sequences via LLM, dispatches top-N via lobster. See chitin swarm/bin/clawta-poller." \
    --every 2m \
    --agent glm-agent \
    --session isolated \
    --light-context \
    --tools exec \
    --timeout-seconds 240 \
    --no-deliver \
    --message "Run exactly one command, no commentary, no other tool calls:

  clawta-poller --once --max-dispatch 2

Wait for it to complete. If it prints JSON with dispatched/demoted counts, you are done — no follow-up turn needed. If it fails, that is logged in ~/.openclaw/logs/clawta-poller.log; do not retry, do not investigate, just exit." \
    >/dev/null
  echo "openclaw cron: 'clawta-kanban-poller' registered (every 2m)."
  echo "  inspect: openclaw cron show clawta-kanban-poller"
  echo "  runs:    openclaw cron runs --name clawta-kanban-poller"
}

install_systemd_timer() {
  local sysd_user_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
  mkdir -p "$sysd_user_dir"
  install -m 0644 "$REPO_ROOT/swarm/systemd/clawta-poller.service" "$sysd_user_dir/clawta-poller.service"
  install -m 0644 "$REPO_ROOT/swarm/systemd/clawta-poller.timer"   "$sysd_user_dir/clawta-poller.timer"
  systemctl --user daemon-reload
  systemctl --user enable --now clawta-poller.timer
  echo "systemd: clawta-poller.timer enabled (every 2m, alternate scheduler)."
  echo "  status:  systemctl --user status clawta-poller.timer"
  echo "  journal: journalctl --user -u clawta-poller.service -f"
}

case "$mode" in
  openclaw)
    install_openclaw_cron
    ;;
  systemd)
    install_systemd_timer
    ;;
  both)
    install_openclaw_cron
    install_systemd_timer
    echo "WARNING: both schedulers will fire — expect double-dispatch every 2min."
    echo "  Disable one with either:"
    echo "    openclaw cron disable clawta-kanban-poller"
    echo "    systemctl --user disable --now clawta-poller.timer"
    ;;
esac

echo ""
echo "Poller log: ~/.openclaw/logs/clawta-poller.log"
echo "Dry-run any time: clawta-poller --dry-run"
