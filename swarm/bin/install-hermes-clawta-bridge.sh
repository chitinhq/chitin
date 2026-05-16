#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SRC="$ROOT/swarm/workflows/hermes-clawta-bridge.py"
TARGET_DIR="${HERMES_SCRIPTS_DIR:-$HOME/.hermes/scripts}"
TARGET="$TARGET_DIR/hermes-clawta-bridge.py"

if [[ ! -f "$SRC" ]]; then
  echo "source not found: $SRC" >&2
  exit 1
fi

mkdir -p "$TARGET_DIR"

if [[ -e "$TARGET" && ! -L "$TARGET" ]]; then
  if cmp -s "$SRC" "$TARGET"; then
    rm -f "$TARGET"
  else
    backup="$TARGET.pre-repo-owned-$(date +%s).bak"
    mv "$TARGET" "$backup"
    echo "backed up existing bridge to $backup"
  fi
fi

ln -sfn "$SRC" "$TARGET"
chmod +x "$SRC"
echo "installed hermes-clawta-bridge: $TARGET -> $SRC"
