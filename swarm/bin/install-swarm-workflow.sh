#!/usr/bin/env bash
# install-swarm-workflow.sh — symlink kanban-dispatch.lobster into ~/.openclaw/workflows
#
# Idempotent. Backs up any existing non-symlink file once.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET_DIR="$HOME/.openclaw/workflows"
SOURCE="$REPO_ROOT/swarm/workflows/kanban-dispatch.lobster"
TARGET="$TARGET_DIR/kanban-dispatch.lobster"

mkdir -p "$TARGET_DIR"

if [[ -e "$TARGET" && ! -L "$TARGET" ]]; then
  bak="$TARGET.bak.$(date +%Y%m%d-%H%M%S)"
  cp "$TARGET" "$bak"
  echo "backed up existing file → $bak"
  rm "$TARGET"
fi

ln -sfn "$SOURCE" "$TARGET"
echo "linked: $TARGET → $SOURCE"
