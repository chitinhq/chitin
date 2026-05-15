#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
SERVICE_SRC="$ROOT/infra/systemd/clawta-judge-backfill.service"
TIMER_SRC="$ROOT/infra/systemd/clawta-judge-backfill.timer"
SERVICE_DST="/etc/systemd/system/clawta-judge-backfill.service"
TIMER_DST="/etc/systemd/system/clawta-judge-backfill.timer"

install -m 0644 "$SERVICE_SRC" "$SERVICE_DST"
install -m 0644 "$TIMER_SRC" "$TIMER_DST"
systemctl daemon-reload
systemctl enable --now clawta-judge-backfill.timer
systemctl list-timers --all clawta-judge-backfill.timer
