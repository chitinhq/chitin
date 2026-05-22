#!/usr/bin/env bash
# install-operator-report.sh — idempotent installer for the operator report
# delivery script (spec 085, Constitution §4). Symlinks the repo source into
# the runtime bin dir so `deliver-operator-report` is on the operator's PATH
# (the Temporal JobSpec invokes the repo source directly; this symlink serves
# the operator and the on-demand Clawta trigger).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$SCRIPT_DIR/deliver-operator-report.sh"
DEST_DIR="${CHITIN_BIN_DIR:-$HOME/.local/bin}"
DEST="$DEST_DIR/deliver-operator-report"

if [[ ! -f "$SRC" ]]; then
  echo "install-operator-report: source not found: $SRC" >&2
  exit 1
fi

mkdir -p "$DEST_DIR"
chmod +x "$SRC"
ln -sfn "$SRC" "$DEST"
echo "installed: $DEST -> $SRC"
