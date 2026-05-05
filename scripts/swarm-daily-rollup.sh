#!/usr/bin/env bash
# swarm-daily-rollup.sh — Daily swarm health rollup.
# Called by chitin-swarm-rollup.service. Exits 0 on success regardless
# of whether alarms fired — alarms are surfaced via the rollup JSON
# (read by alarm-feeder + watchdog) and optional Slack post, not via
# systemd's failed-unit signal. See swarm_health.py:main for rationale.
set -euo pipefail

REPO_ROOT="${CHITIN_REPO_ROOT:-$HOME/workspace/chitin}"

cd "$REPO_ROOT/python"
exec python3 -m analysis.swarm_health "$@"
