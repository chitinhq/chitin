#!/usr/bin/env bash
# install-swarm.sh — install chitin-owned swarm files into the operator's
# OpenClaw runtime directory.
#
# The chitin repo is the canonical source for:
#   - swarm/workflows/*.lobster        — dispatch + routing workflows
#   - swarm/workflows/*.py             — _pick_driver, clawta_decisions, etc.
#   - swarm/workflows/*.md             — design notes that live next to code
#   - swarm/data/agent-cards/*.json    — worker capability + invocation cards
#
# This script copies them to ~/.openclaw/ so the deployed runtime matches
# the repo. Drift between repo and deployed is caught by
# `scripts/check-swarm-deployed-sync.sh` (a regression-gate invariant).
#
# Idempotent: re-running overwrites with the current repo state and leaves
# untouched any files the repo doesn't own (operator state, hermes config,
# disabled cards, etc.).
#
# Backups: each file replaced gets a `.bak-<TS>` copy so the operator can
# revert if needed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOYED_ROOT="${OPENCLAW_HOME:-$HOME/.openclaw}"
TS=$(date +%Y%m%d-%H%M%S)

WORKFLOWS_SRC="$REPO_ROOT/swarm/workflows"
WORKFLOWS_DST="$DEPLOYED_ROOT/workflows"
CARDS_SRC="$REPO_ROOT/swarm/data/agent-cards"
CARDS_DST="$DEPLOYED_ROOT/data/agent-cards"
BIN_SRC="$REPO_ROOT/swarm/bin"
BIN_DST="$DEPLOYED_ROOT/bin"
BOARD_ROOT="$HOME/.hermes/kanban/boards/chitin"
BOARD_CONFIG="$BOARD_ROOT/config.json"

mkdir -p "$WORKFLOWS_DST" "$CARDS_DST" "$BIN_DST"

copied=0
backed_up=0

install_file() {
    local src="$1" dst="$2"
    if [ -e "$dst" ]; then
        # Skip if unchanged.
        if cmp -s "$src" "$dst"; then
            return 0
        fi
        # Back up the operator's version before clobbering.
        cp -p "$dst" "$dst.bak-$TS"
        backed_up=$((backed_up + 1))
    fi
    cp -p "$src" "$dst"
    copied=$((copied + 1))
    echo "  installed $(basename "$dst")"
}

echo "Installing swarm workflows into $WORKFLOWS_DST"
# Skip backups, pycache, hidden dotfiles.
find "$WORKFLOWS_SRC" -maxdepth 1 -type f \
    \( -name '*.lobster' -o -name '*.py' -o -name '*.md' \) \
    ! -name '*.bak*' \
    ! -name '.*' \
    -print 2>/dev/null \
| while IFS= read -r src; do
    install_file "$src" "$WORKFLOWS_DST/$(basename "$src")"
done

echo "Installing agent cards into $CARDS_DST"
find "$CARDS_SRC" -maxdepth 1 -type f -name '*.json' \
    ! -name '*.bak*' \
    -print 2>/dev/null \
| while IFS= read -r src; do
    install_file "$src" "$CARDS_DST/$(basename "$src")"
done

# clawta-* operator cron + helper scripts. The poller (~/.local/bin/...)
# is a separate install path managed by the operator; this set covers
# the openclaw-cron-resident scripts (pool guard, failure sentinel,
# escalator, etc.) that live under ~/.openclaw/bin/.
echo "Installing swarm operator scripts into $BIN_DST"
find "$BIN_SRC" -maxdepth 1 -type f -name 'clawta-*' \
    ! -name '*.bak*' \
    -print 2>/dev/null \
| while IFS= read -r src; do
    dst="$BIN_DST/$(basename "$src")"
    install_file "$src" "$dst"
    # Preserve executable bit (operator scripts are commonly +x in repo)
    chmod +x "$dst" 2>/dev/null || true
done

# `find ... | while read` runs in a subshell, so $copied/$backed_up don't
# survive. Recount the destination tree against the source as a final
# summary.
src_count=$(find "$WORKFLOWS_SRC" "$CARDS_SRC" "$BIN_SRC" -maxdepth 1 -type f \
    \( -name '*.lobster' -o -name '*.py' -o -name '*.md' -o -name '*.json' -o -name 'clawta-*' \) \
    ! -name '*.bak*' 2>/dev/null | wc -l)

echo
echo "install-swarm: ${src_count} canonical file(s) under swarm/ are now installed at $DEPLOYED_ROOT"
echo "  Backups (if any) at: ${WORKFLOWS_DST}/*.bak-${TS}, ${CARDS_DST}/*.bak-${TS}"

mkdir -p "$BOARD_ROOT"
if [ ! -f "$BOARD_CONFIG" ]; then
    cat > "$BOARD_CONFIG" <<'EOF'
{
  "repo": "chitinhq/chitin",
  "default_branch": "main",
  "workspace_root": "~/workspace/chitin",
  "kernel_bin": "chitin-kernel",
  "chitin_yaml": "chitin.yaml"
}
EOF
    echo "  seeded board config at $BOARD_CONFIG"
fi

echo
echo "To verify no drift: bash scripts/check-swarm-deployed-sync.sh"
