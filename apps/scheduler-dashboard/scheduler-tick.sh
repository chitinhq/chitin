#!/usr/bin/env bash
# Systemd-timer dispatch wrapper: runs every 5 minutes via the .timer unit below.
# Calls `chitin scheduler tick` and notifies for items coming up in the next 15 min.
set -euo pipefail

CHITIN_BIN="${CHITIN_BIN:-chitin}"

if ! command -v "$CHITIN_BIN" &>/dev/null; then
  echo "chitin binary not found — set CHITIN_BIN env var" >&2
  exit 1
fi

exec "$CHITIN_BIN" scheduler tick
