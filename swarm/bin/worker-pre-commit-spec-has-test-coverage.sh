#!/usr/bin/env bash
# worker-pre-commit-spec-has-test-coverage.sh — git pre-commit hook for spec 020 L2.
#
# Enforces "spec has test coverage": when a spec.md is staged, it must contain
# a ## Test coverage section with ≥1 table row. When a test file is staged
# under recognized test directories, it must contain a spec reference comment
# in its first 20 lines.
#
# Per spec 020 Layer 2 + Constitution §1.2.
# Install: symlinked into <worktree>/.git/hooks/pre-commit by lobster spawn_worker.
set -uo pipefail

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
CHECKER="$HOOK_DIR/worker-pre-commit-spec-has-test-coverage.py"

if [[ ! -f "$CHECKER" ]]; then
    echo "[spec-coverage-hook] checker missing at $CHECKER — passing (post-spawn validator still applies)." >&2
    exit 0
fi

# Build list of staged spec and test files
SPEC_FILES=""
TEST_FILES=""
while IFS= read -r f; do
    case "$f" in
        .specify/specs/*/spec.md) SPEC_FILES="$SPEC_FILES"$'\n'"$f" ;;
    esac
    # Test file detection handled by python checker via staged list on stdin
done <<< "$STAGED"

printf '%s' "$STAGED" | python3 "$CHECKER" --repo-root "$REPO_ROOT"
RC=$?

if [[ $RC -ne 0 ]]; then
    echo "" >&2
    echo "[spec-coverage-hook] ⛔ COMMIT REJECTED — spec/test coverage requirement not met (spec 020 L2)." >&2
    echo "" >&2
    echo "See spec 020 Layer 2 for requirements:" >&2
    echo "  - Every staged spec.md must have a ## Test coverage section with ≥1 table row" >&2
    echo "  - Every staged test file must contain a 'spec: NNN-<slug>' reference in the first 20 lines" >&2
    echo "" >&2
    exit 1
fi

exit 0