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
link_file "$REPO_ROOT/swarm/workflows/_pick_driver.py" "$TARGET_DIR/_pick_driver.py"
link_file "$REPO_ROOT/swarm/workflows/clawta_decisions.py" "$TARGET_DIR/clawta_decisions.py"
link_file "$REPO_ROOT/swarm/workflows/spawn_worker_subprocess.py" "$TARGET_DIR/spawn_worker_subprocess.py"
link_file "$REPO_ROOT/swarm/workflows/worker_failure_report.py" "$TARGET_DIR/worker_failure_report.py"
link_file "$REPO_ROOT/swarm/workflows/pr_failure_report.py" "$TARGET_DIR/pr_failure_report.py"
link_file "$REPO_ROOT/swarm/workflows/judge.py" "$TARGET_DIR/judge.py"
