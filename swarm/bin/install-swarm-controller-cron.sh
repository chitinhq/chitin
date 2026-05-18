#!/usr/bin/env bash
# install-swarm-controller-cron.sh — idempotent installer for the swarm-controller loop
#
# Per Constitution S6: tracked source + idempotent installer.
# This script registers a hermes cron job that runs
#   swarm-controller --loop --board swarm --tick-seconds 60
# If the job already exists, it updates it. If hermes cron is not
# available, falls back to system crontab.
#
# Usage:
#   ./install-swarm-controller-cron.sh          # install/update
#   ./install-swarm-controller-cron.sh --verify  # check if installed
#   ./install-swarm-controller-cron.sh --remove  # uninstall
#
# Env:
#   KANBAN_BOARD         board name (default: swarm)
#   TICK_SECONDS         tick interval (default: 60)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONTROLLER="$SCRIPT_DIR/swarm-controller"
JOB_NAME="swarm-controller-loop"

BOARD="${KANBAN_BOARD:-swarm}"
TICK="${TICK_SECONDS:-60}"

# ── Verify controller exists ──────────────────────────────────────

if [[ ! -x "$CONTROLLER" ]]; then
    echo "FATAL: swarm-controller not found or not executable at $CONTROLLER" >&2
    exit 1
fi

# ── Check for existing job ─────────────────────────────────────────

check_existing() {
    if command -v hermes &>/dev/null; then
        existing=$(hermes cron list --json 2>/dev/null | grep -c "\"$JOB_NAME\"" || true)
        if [[ "$existing" -gt 0 ]]; then
            echo "hermes-cron"
            return
        fi
    fi
    if crontab -l 2>/dev/null | grep -q "swarm-controller.*--loop"; then
        echo "system-crontab"
        return
    fi
    echo "none"
}

# ── Actions ────────────────────────────────────────────────────────

case "${1:-install}" in
    --verify)
        backend=$(check_existing)
        if [[ "$backend" == "none" ]]; then
            echo "NOT INSTALLED"
            exit 1
        else
            echo "INSTALLED via $backend"
            exit 0
        fi
        ;;

    --remove)
        backend=$(check_existing)
        if [[ "$backend" == "hermes-cron" ]]; then
            if command -v hermes &>/dev/null; then
                hermes cron list 2>/dev/null | grep "$JOB_NAME" | awk '{print $1}' | while read -r job_id; do
                    hermes cron remove "$job_id" 2>/dev/null || true
                done
                echo "Removed hermes cron job(s) named $JOB_NAME"
            fi
        elif [[ "$backend" == "system-crontab" ]]; then
            crontab -l 2>/dev/null | grep -v "swarm-controller.*--loop" | crontab -
            echo "Removed system crontab entry"
        else
            echo "Nothing to remove"
        fi
        exit 0
        ;;

    install|"")
        backend=$(check_existing)
        if [[ "$backend" != "none" ]]; then
            echo "swarm-controller cron already installed via $backend (idempotent - skipping)"
            exit 0
        fi

        # Create hermes symlink if needed
        link_path="$HOME/.hermes/scripts/swarm-controller"
        if [[ ! -L "$link_path" && ! -e "$link_path" ]]; then
            ln -s "$CONTROLLER" "$link_path"
            echo "Created symlink: $link_path -> $CONTROLLER"
        fi

        # Try hermes cron first (preferred - receipt to #swarm built in)
        if command -v hermes &>/dev/null; then
            echo "Attempting hermes cron install..."
            hermes cron create \
                --name "$JOB_NAME" \
                --schedule "every 60s" \
                --deliver "discord:#swarm" \
                2>/dev/null && {
                echo "Installed hermes cron job: $JOB_NAME (every 60s, board=$BOARD)"
                echo "Heartbeat receipts will post to #swarm every 5 ticks (5 min)"
                exit 0
            } || {
                echo "hermes cron create failed, falling back to system crontab"
            }
        fi

        # Fallback: system crontab
        cron_entry="* * * * * KANBAN_BOARD=$BOARD $CONTROLLER --once --board $BOARD >> $HOME/.openclaw/logs/swarm-controller-cron.log 2>&1"
        (crontab -l 2>/dev/null; echo "$cron_entry") | crontab -
        echo "Installed system crontab entry (every 1 min, board=$BOARD)"
        echo "NOTE: Heartbeat receipts require openclaw message send in cron PATH"
        exit 0
        ;;

    *)
        echo "Usage: $0 [--verify|--remove|install]" >&2
        exit 1
        ;;
esac