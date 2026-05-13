#!/usr/bin/env bash
# check-swarm-deployed-sync — invariant: chitin's repo-side swarm files
# match what's deployed in the operator's OpenClaw runtime directory.
#
# Joins the regression-gate registry (any scripts/check-*.sh is picked up
# by scripts/regression-gate.sh). The contract is:
#
#   - In a non-operator environment (CI, fresh clone, no ~/.openclaw),
#     exit 0 — there's nothing deployed to compare against.
#   - On an operator machine where ~/.openclaw exists, every file the
#     repo owns under swarm/workflows/ and swarm/data/agent-cards/ must
#     match its deployed counterpart byte-for-byte. Any drift exits 1.
#
# To resolve drift: bash scripts/install-swarm.sh
#
# Spec: docs/superpowers/specs/2026-05-13-regression-gate.md
# Related: discussion in operator session 2026-05-13 about repo-as-canonical
# for chitin-owned swarm pieces (the bug that hit was clawta_decisions.py
# existing in repo but missing from deployed).

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEPLOYED_ROOT="${OPENCLAW_HOME:-$HOME/.openclaw}"

# If the operator hasn't deployed yet, there's nothing to drift against.
# This is the CI / fresh-clone path; pass cleanly.
if [ ! -d "$DEPLOYED_ROOT" ]; then
    echo "check-swarm-deployed-sync: $DEPLOYED_ROOT does not exist (CI or fresh clone) — skipping"
    exit 0
fi

WORKFLOWS_SRC="$REPO_ROOT/swarm/workflows"
WORKFLOWS_DST="$DEPLOYED_ROOT/workflows"
CARDS_SRC="$REPO_ROOT/swarm/data/agent-cards"
CARDS_DST="$DEPLOYED_ROOT/data/agent-cards"
BIN_SRC="$REPO_ROOT/swarm/bin"
BIN_DST="$DEPLOYED_ROOT/bin"

drift=0

check_pair() {
    local src="$1" dst="$2"
    if [ ! -e "$dst" ]; then
        echo "  MISSING  $dst  (repo has: $src)"
        drift=$((drift + 1))
        return
    fi
    if ! cmp -s "$src" "$dst"; then
        echo "  DIFFERS  $dst  (vs $src)"
        drift=$((drift + 1))
    fi
}

# Workflows
if [ -d "$WORKFLOWS_SRC" ]; then
    while IFS= read -r src; do
        check_pair "$src" "$WORKFLOWS_DST/$(basename "$src")"
    done < <(find "$WORKFLOWS_SRC" -maxdepth 1 -type f \
        \( -name '*.lobster' -o -name '*.py' -o -name '*.md' \) \
        ! -name '*.bak*' \
        ! -name '.*' 2>/dev/null)
fi

# Agent cards
if [ -d "$CARDS_SRC" ]; then
    while IFS= read -r src; do
        check_pair "$src" "$CARDS_DST/$(basename "$src")"
    done < <(find "$CARDS_SRC" -maxdepth 1 -type f -name '*.json' \
        ! -name '*.bak*' 2>/dev/null)
fi

# Operator scripts (clawta-* cron helpers under swarm/bin/)
if [ -d "$BIN_SRC" ] && [ -d "$BIN_DST" ]; then
    while IFS= read -r src; do
        check_pair "$src" "$BIN_DST/$(basename "$src")"
    done < <(find "$BIN_SRC" -maxdepth 1 -type f -name 'clawta-*' \
        ! -name '*.bak*' 2>/dev/null)
fi

if [ "$drift" -gt 0 ]; then
    echo
    echo "check-swarm-deployed-sync: $drift file(s) drifted between repo and $DEPLOYED_ROOT"
    echo "  Resolve: bash scripts/install-swarm.sh"
    echo "  Or revert the repo to match deployed if the operator-side is the source of truth."
    exit 1
fi

echo "check-swarm-deployed-sync: repo swarm/ matches deployed $DEPLOYED_ROOT"
exit 0
