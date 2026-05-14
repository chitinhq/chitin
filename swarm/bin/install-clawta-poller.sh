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

  openclaw   Register the poller plus runtime guard jobs as OpenClaw cron
             (default). Visible in `openclaw cron list`.
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
for tool in clawta-poller clawta-poller-safe-tick clawta-blocked-escalator clawta-stale-worker-watchdog clawta-worker-failure-sentinel; do
  ln -sfn "$REPO_ROOT/swarm/bin/$tool" "$LOCAL_BIN/$tool"
  echo "linked: $LOCAL_BIN/$tool → $REPO_ROOT/swarm/bin/$tool"
done

# Also ensure kanban-flow is reachable; some boxes may have installed the
# poller before kanban-flow's symlink.
if [[ -x "$REPO_ROOT/scripts/kanban-flow" ]]; then
  ln -sfn "$REPO_ROOT/scripts/kanban-flow" "$LOCAL_BIN/kanban-flow"
  echo "linked: $LOCAL_BIN/kanban-flow → $REPO_ROOT/scripts/kanban-flow"
fi

ensure_openclaw_cron_job() {
  local name="$1"
  local every="$2"
  local description="$3"
  local command="$4"
  if openclaw cron list 2>/dev/null | grep -q "^[a-f0-9-]\\+ ${name} "; then
    echo "openclaw cron: job '$name' already registered."
    return 0
  fi
  openclaw cron add \
    --name "$name" \
    --description "$description" \
    --every "$every" \
    --agent glm-agent \
    --session isolated \
    --light-context \
    --tools exec \
    --timeout-seconds 240 \
    --no-deliver \
    --message "Run exactly one command, no commentary, no other tool calls:

  ${command}

After the exec tool returns (whether the command has finished or is still running in the background), reply with exactly the word 'ok' and nothing else. Do not retry. Do not investigate failures — those are logged in ~/.openclaw/logs/.

The closing 'ok' token is REQUIRED — without it the cron metrics report the run as 'couldn't generate a response' even though the command fired correctly." \
    >/dev/null
  echo "openclaw cron: '$name' registered (${every})."
  echo "  inspect: openclaw cron show $name"
  echo "  runs:    openclaw cron runs --name $name"
}

install_openclaw_cron() {
  if ! command -v openclaw >/dev/null 2>&1; then
    echo "openclaw: CLI not on PATH; skipping openclaw cron registration." >&2
    return 1
  fi
  ensure_openclaw_cron_job \
    "clawta-kanban-poller" \
    "2m" \
    "Autonomous kanban dispatch tick. Reads ready terminal-lane tickets, sequences via LLM, dispatches top-N via lobster. See chitin swarm/bin/clawta-poller." \
    "TERMINAL_LANES=codex,copilot,gemini CLAWTA_MAX_ACTIVE_WORKERS=1 CLAWTA_MAX_LOAD=12 CLAWTA_MAX_DISPATCH=1 CLAWTA_ROUTER_MODE=deterministic flock -n /tmp/clawta-kanban-poller.lock clawta-poller-safe-tick"
  ensure_openclaw_cron_job \
    "clawta-blocked-escalator" \
    "10m" \
    "Escalate blocked non-red swarm tickets to the operator lane. See chitin swarm/bin/clawta-blocked-escalator." \
    "clawta-blocked-escalator"
  ensure_openclaw_cron_job \
    "clawta-stale-worker-watchdog" \
    "10m" \
    "Block and escalate stale in_progress swarm tickets with no PR, no worker, and no log movement. See chitin swarm/bin/clawta-stale-worker-watchdog." \
    "clawta-stale-worker-watchdog"
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
echo "Dry-run any time:"
echo "  clawta-poller --dry-run"
echo "  clawta-blocked-escalator --dry-run --json"
echo "  clawta-stale-worker-watchdog --dry-run --json"
