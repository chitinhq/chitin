#!/usr/bin/env bash
# install-swarm-workflow.sh — symlink swarm workflow helpers into ~/.openclaw/workflows
#
# Idempotent. Backs up any existing non-symlink file once.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET_DIR="$HOME/.openclaw/workflows"
mkdir -p "$TARGET_DIR"

link_file() {
  local source="$1"
  local target="$2"

  if [[ -e "$target" && ! -L "$target" ]]; then
    bak="$target.bak.$(date +%Y%m%d-%H%M%S)"
    cp "$target" "$bak"
    echo "backed up existing file → $bak"
    rm "$target"
  fi

  ln -sfn "$source" "$target"
  echo "linked: $target → $source"
}

link_file "$REPO_ROOT/swarm/workflows/kanban-dispatch.lobster" "$TARGET_DIR/kanban-dispatch.lobster"
link_file "$REPO_ROOT/docs/governance-setup-extras/_pick_driver.py" "$TARGET_DIR/_pick_driver.py"
link_file "$REPO_ROOT/swarm/workflows/clawta_decisions.py" "$TARGET_DIR/clawta_decisions.py"
