#!/usr/bin/env bash
# swarm-daily-rollup.sh — Daily swarm health rollup.
# Called by chitin-swarm-rollup.service. Exits non-zero if alarms fired.
set -euo pipefail

REPO_ROOT="${CHITIN_REPO_ROOT:-$HOME/workspace/chitin}"

cd "$REPO_ROOT/python"
exec python3 -m analysis.swarm_health "$@"
