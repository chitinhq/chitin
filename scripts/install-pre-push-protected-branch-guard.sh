#!/usr/bin/env bash
# install-pre-push-protected-branch-guard.sh — install the protected-branch
# guard as a git pre-push hook in every chitin / workspace / bench-devs-platform
# checkout the operator works in.
#
# Idempotent. Looks for repos under $HOME/workspace/ and installs into each.
# Override target repos via REPOS env (space-separated absolute paths).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GUARD_SOURCE="$REPO_ROOT/scripts/pre-push-protected-branch-guard.sh"

if [[ ! -x "$GUARD_SOURCE" ]]; then
    chmod +x "$GUARD_SOURCE"
fi

REPOS="${REPOS:-$HOME/workspace/chitin $HOME/workspace/bench-devs-platform $HOME/workspace}"

for repo in $REPOS; do
    if [[ ! -d "$repo/.git" ]]; then
        echo "skip: $repo (not a git repo)"
        continue
    fi
    hook_path="$repo/.git/hooks/pre-push"
    if [[ -e "$hook_path" && ! -L "$hook_path" ]]; then
        bak="$hook_path.bak.$(date +%Y%m%d-%H%M%S)"
        cp "$hook_path" "$bak"
        echo "backed up existing pre-push → $bak"
        rm "$hook_path"
    fi
    ln -sfn "$GUARD_SOURCE" "$hook_path"
    echo "installed: $hook_path → $GUARD_SOURCE"
done
