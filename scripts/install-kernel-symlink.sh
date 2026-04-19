#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_SRC="$REPO_ROOT/dist/go/execution-kernel/chitin-kernel"
BIN_DST_DIR="${CHITIN_BIN_DIR:-$HOME/.local/bin}"
BIN_DST="$BIN_DST_DIR/chitin-kernel"

if [[ ! -x "$BIN_SRC" ]]; then
  echo "error: $BIN_SRC does not exist or is not executable. Run: pnpm nx build execution-kernel" >&2
  exit 1
fi

mkdir -p "$BIN_DST_DIR"

# If $BIN_DST exists and is NOT a symlink, abort (safety).
if [[ -e "$BIN_DST" && ! -L "$BIN_DST" ]]; then
  echo "error: $BIN_DST exists and is not a symlink. Refusing to overwrite." >&2
  exit 1
fi

ln -sf "$BIN_SRC" "$BIN_DST"
echo "symlinked $BIN_DST -> $BIN_SRC"
