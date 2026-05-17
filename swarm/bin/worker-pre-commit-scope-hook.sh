#!/usr/bin/env bash
# worker-pre-commit-scope-hook.sh — git pre-commit hook for worker worktrees.
#
# Reads the file-system scope from the worker's worktree at .git/swarm-scope.json
# (written by the lobster spawn_worker step), then validates that every staged
# file matches a MAY glob and doesn't match a MUST_NOT glob.
#
# Rejects out-of-scope commits BEFORE they land in the worktree. The post-spawn
# validator stays as backstop for anything that slips through.
#
# Per Constitution §1.1 + chitinhq/workspace#418 day-0 retro tiger-team C2.
#
# Install: symlinked into <worktree>/.git/hooks/pre-commit by the lobster
# spawn_worker step. Scope file at .git/swarm-scope.json also written at spawn.
#
# Bypass: SWARM_SKIP_SCOPE_CHECK=1 (logged) or --no-verify (post-spawn catches).
set -uo pipefail

if [[ "${SWARM_SKIP_SCOPE_CHECK:-}" == "1" ]]; then
    echo "[scope-hook] SWARM_SKIP_SCOPE_CHECK=1 — bypass (post-spawn validator still applies)." >&2
    exit 0
fi

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
if [[ -z "$REPO_ROOT" ]]; then
    exit 0
fi

# Git-dir for worktrees is in <main>/.git/worktrees/<branch>/; use rev-parse
# to resolve.
GIT_DIR="$(git rev-parse --git-dir 2>/dev/null)"
if [[ -z "$GIT_DIR" ]]; then exit 0; fi
# Absolutize
if [[ "$GIT_DIR" != /* ]]; then GIT_DIR="$REPO_ROOT/$GIT_DIR"; fi

SCOPE_FILE="$GIT_DIR/swarm-scope.json"
if [[ ! -f "$SCOPE_FILE" ]]; then
    exit 0  # No scope declared = non-worker context. Pass.
fi

STAGED=$(git diff --cached --name-only --diff-filter=AM)
if [[ -z "$STAGED" ]]; then exit 0; fi

# Resolve sibling python helper (same dir as this hook script's source).
HOOK_SRC="$(readlink -f "$0" 2>/dev/null || realpath "$0" 2>/dev/null || echo "$0")"
HOOK_DIR="$(dirname "$HOOK_SRC")"
CHECKER="$HOOK_DIR/worker-pre-commit-scope-check.py"
if [[ ! -f "$CHECKER" ]]; then
    echo "[scope-hook] checker missing at $CHECKER — passing (post-spawn validator still applies)." >&2
    exit 0
fi

OFFENDING=$(printf '%s' "$STAGED" | python3 "$CHECKER" "$SCOPE_FILE")
RC=$?

if [[ $RC -ne 0 ]]; then
    echo "" >&2
    echo "[scope-hook] ⛔ COMMIT REJECTED — file-system scope violation" >&2
    echo "" >&2
    echo "Staged files outside the spec's declared scope:" >&2
    while IFS= read -r line; do echo "  $line" >&2; done <<< "$OFFENDING"
    echo "" >&2
    echo "Scope ($SCOPE_FILE):" >&2
    python3 -c "
import json
s = json.load(open('$SCOPE_FILE'))
print('  MAY:      ' + (', '.join(s.get('may', [])) or '(any)'))
print('  MUST NOT: ' + (', '.join(s.get('must_not', [])) or '(none)'))
" >&2
    echo "" >&2
    echo "Options:" >&2
    echo "  1. git reset HEAD <file>   # unstage the out-of-scope file(s)" >&2
    echo "  2. STOP and comment on the ticket — request a spec amendment" >&2
    echo "  3. SWARM_SKIP_SCOPE_CHECK=1 git commit ...  # bypass (post-spawn validator still applies)" >&2
    echo "" >&2
    exit 1
fi

exit 0
