#!/usr/bin/env bash
# worker-pre-commit-no-code-without-test.sh — git pre-commit hook for spec 020 L1.
#
# Enforces "no code without test": when any code file is staged, at least one
# test file must also be staged, OR the commit message must contain
# 'no-test-change-justified: <reason>'.
#
# Per spec 020 Layer 1 + Constitution §1.2.
# Install: symlinked into <worktree>/.git/hooks/pre-commit by lobster spawn_worker.
# Bypass: SWARM_SKIP_TEST_CHECK=1 (logged).
set -uo pipefail

if [[ "${SWARM_SKIP_TEST_CHECK:-}" == "1" ]]; then
    echo "[test-hook] SWARM_SKIP_TEST_CHECK=1 — bypass." >&2
    exit 0
fi

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null)"
if [[ -z "$REPO_ROOT" ]]; then
    exit 0
fi

GIT_DIR="$(git rev-parse --git-dir 2>/dev/null)"
if [[ -z "$GIT_DIR" ]]; then exit 0; fi
if [[ "$GIT_DIR" != /* ]]; then GIT_DIR="$REPO_ROOT/$GIT_DIR"; fi

STAGED=$(git diff --cached --name-only --diff-filter=AM)
if [[ -z "$STAGED" ]]; then exit 0; fi

HOOK_SRC="$(readlink -f "$0" 2>/dev/null || realpath "$0" 2>/dev/null || echo "$0")"
HOOK_DIR="$(dirname "$HOOK_SRC")"
CHECKER="$HOOK_DIR/worker-pre-commit-no-code-without-test.py"

if [[ ! -f "$CHECKER" ]]; then
    echo "[test-hook] checker missing at $CHECKER — passing (post-spawn validator still applies)." >&2
    exit 0
fi

COMMIT_MSG_FILE="$GIT_DIR/COMMIT_EDITMSG"
# For merge commits the message is in MERGE_MSG
if [[ ! -f "$COMMIT_MSG_FILE" ]]; then
    COMMIT_MSG_FILE="$GIT_DIR/MERGE_MSG"
fi
COMMIT_MSG=""
if [[ -f "$COMMIT_MSG_FILE" ]]; then
    COMMIT_MSG="$(cat "$COMMIT_MSG_FILE")"
fi

printf '%s' "$STAGED" | python3 "$CHECKER" --commit-message "$COMMIT_MSG"
RC=$?

if [[ $RC -ne 0 ]]; then
    echo "" >&2
    echo "[test-hook] ⛔ COMMIT REJECTED — no test file changed alongside code (spec 020 L1)." >&2
    echo "" >&2
    echo "Staged code files require a matching test change or an escape clause." >&2
    echo "" >&2
    echo "Options:" >&2
    echo "  1. Stage a test file alongside the code change" >&2
    echo "  2. Add 'no-test-change-justified: <reason>' to your commit message" >&2
    echo "  3. SWARM_SKIP_TEST_CHECK=1 git commit ...  # bypass (not recommended)" >&2
    echo "" >&2
    exit 1
fi

exit 0