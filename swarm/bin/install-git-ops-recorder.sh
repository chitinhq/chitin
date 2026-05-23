#!/usr/bin/env bash
# install-git-ops-recorder.sh — idempotent installer for the git ops recorder.
#
# Installs two git hooks (reference-transaction, post-checkout) and the
# git-ops-replay query tool. Logs ref mutations to ~/.chitin/git-ops.jsonl
# with full process-tree attribution.
#
# Usage:
#   ./install-git-ops-recorder.sh                # install + symlink replay tool
#   ./install-git-ops-recorder.sh --verify       # check installed state
#   ./install-git-ops-recorder.sh --remove       # uninstall (removes symlinks only)
#   ./install-git-ops-recorder.sh --git-dir PATH # use a non-default .git dir
#
# Idempotency:
#   - Symlinks (not copies) so improvements to the tracked hook source
#     automatically apply on next git op.
#   - Re-running on an already-installed hook is a no-op.
#   - If a non-recorder hook exists at the target path, the installer
#     REFUSES TO OVERWRITE and emits an actionable error. Operators
#     must chain manually.
#
# Why hooks-by-symlink:
#   - The hook source lives under swarm/hooks/git-ops-recorder/ and is
#     version-tracked. Symlinking from .git/hooks/ to that source means
#     `git pull` picks up updates without rerunning the installer.
#   - The replay tool symlinks from ~/.chitin/bin/git-ops-replay to
#     swarm/bin/git-ops-replay so its updates are also pull-driven.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HOOK_SRC_DIR="$REPO_ROOT/swarm/hooks/git-ops-recorder"

# Resolve the active .git directory. Works for normal checkouts and worktrees
# (where .git is a file pointing at the real gitdir under .git/worktrees/<name>).
GIT_DIR_OVERRIDE=""
ACTION="install"

for arg in "$@"; do
    case "$arg" in
        --verify)   ACTION="verify" ;;
        --remove)   ACTION="remove" ;;
        --git-dir)  shift; GIT_DIR_OVERRIDE="$1" ;;
        --git-dir=*) GIT_DIR_OVERRIDE="${arg#*=}" ;;
        --help|-h)
            sed -n '2,18p' "$0"
            exit 0
            ;;
    esac
done

if [[ -n "$GIT_DIR_OVERRIDE" ]]; then
    GIT_DIR="$GIT_DIR_OVERRIDE"
else
    # Run from inside the repo so this picks up the right gitdir.
    cd "$REPO_ROOT"
    GIT_DIR=$(git rev-parse --absolute-git-dir 2>/dev/null) || {
        echo "FATAL: not inside a git repository (run from $REPO_ROOT or use --git-dir)" >&2
        exit 2
    }
fi

HOOKS_DIR="$GIT_DIR/hooks"
LOCAL_BIN="$HOME/.chitin/bin"

REF_HOOK_SRC="$HOOK_SRC_DIR/reference-transaction"
POST_HOOK_SRC="$HOOK_SRC_DIR/post-checkout"
REF_HOOK_DST="$HOOKS_DIR/reference-transaction"
POST_HOOK_DST="$HOOKS_DIR/post-checkout"

REPLAY_SRC="$REPO_ROOT/swarm/bin/git-ops-replay"
REPLAY_DST="$LOCAL_BIN/git-ops-replay"

is_our_symlink() {
    # Return 0 if the file at $1 is a symlink pointing at $2.
    [[ -L "$1" ]] && [[ "$(readlink "$1")" == "$2" ]]
}

install_hook() {
    local src="$1" dst="$2" name="$3"
    if [[ ! -f "$src" ]]; then
        echo "FATAL: missing hook source: $src" >&2
        exit 2
    fi
    chmod +x "$src"
    mkdir -p "$(dirname "$dst")"
    if is_our_symlink "$dst" "$src"; then
        echo "OK   $name (already symlinked to $src)"
        return 0
    fi
    if [[ -e "$dst" || -L "$dst" ]]; then
        echo "REFUSING to overwrite existing hook: $dst" >&2
        echo "  Existing: $(ls -la "$dst" 2>/dev/null | awk '{print $NF}')" >&2
        echo "  Wanted:   symlink to $src" >&2
        echo "  Resolution: chain the recorder from your existing hook, or" >&2
        echo "  move the existing hook aside and re-run this installer." >&2
        exit 1
    fi
    ln -s "$src" "$dst"
    echo "OK   $name (symlinked $dst -> $src)"
}

install_replay() {
    if [[ ! -f "$REPLAY_SRC" ]]; then
        echo "FATAL: missing replay tool source: $REPLAY_SRC" >&2
        exit 2
    fi
    chmod +x "$REPLAY_SRC"
    mkdir -p "$LOCAL_BIN"
    if is_our_symlink "$REPLAY_DST" "$REPLAY_SRC"; then
        echo "OK   git-ops-replay (already symlinked to $REPLAY_SRC)"
        return 0
    fi
    if [[ -e "$REPLAY_DST" || -L "$REPLAY_DST" ]]; then
        echo "WARN existing file at $REPLAY_DST — backing up to ${REPLAY_DST}.bak" >&2
        mv "$REPLAY_DST" "${REPLAY_DST}.bak"
    fi
    ln -s "$REPLAY_SRC" "$REPLAY_DST"
    echo "OK   git-ops-replay (symlinked $REPLAY_DST -> $REPLAY_SRC)"
}

verify() {
    local ok=true
    if is_our_symlink "$REF_HOOK_DST" "$REF_HOOK_SRC"; then
        echo "INSTALLED reference-transaction: $REF_HOOK_DST -> $REF_HOOK_SRC"
    else
        echo "MISSING reference-transaction at $REF_HOOK_DST"
        ok=false
    fi
    if is_our_symlink "$POST_HOOK_DST" "$POST_HOOK_SRC"; then
        echo "INSTALLED post-checkout: $POST_HOOK_DST -> $POST_HOOK_SRC"
    else
        echo "MISSING post-checkout at $POST_HOOK_DST"
        ok=false
    fi
    if is_our_symlink "$REPLAY_DST" "$REPLAY_SRC"; then
        echo "INSTALLED git-ops-replay: $REPLAY_DST -> $REPLAY_SRC"
    else
        echo "MISSING git-ops-replay at $REPLAY_DST"
        ok=false
    fi
    echo "LOG path: ${CHITIN_GIT_OPS_LOG_DIR:-$HOME/.chitin}/git-ops.jsonl"
    if [[ -f "${CHITIN_GIT_OPS_LOG_DIR:-$HOME/.chitin}/git-ops.jsonl" ]]; then
        local lines
        lines=$(wc -l < "${CHITIN_GIT_OPS_LOG_DIR:-$HOME/.chitin}/git-ops.jsonl")
        echo "  $lines record(s) captured"
    else
        echo "  (no records yet)"
    fi
    $ok || exit 1
}

remove() {
    local removed=0
    for f in "$REF_HOOK_DST" "$POST_HOOK_DST" "$REPLAY_DST"; do
        if [[ -L "$f" ]]; then
            rm "$f"
            echo "REMOVED $f"
            removed=$((removed + 1))
        fi
    done
    if [[ $removed -eq 0 ]]; then
        echo "(nothing to remove — no recorder symlinks found)"
    fi
    echo
    echo "NOTE: the log at ${CHITIN_GIT_OPS_LOG_DIR:-$HOME/.chitin}/git-ops.jsonl is preserved."
    echo "      Delete it manually if you want a clean slate."
}

case "$ACTION" in
    install)
        install_hook "$REF_HOOK_SRC" "$REF_HOOK_DST" "reference-transaction"
        install_hook "$POST_HOOK_SRC" "$POST_HOOK_DST" "post-checkout"
        install_replay
        echo
        echo "Recorder installed. Test it:"
        echo "  git checkout -b /tmp/test-recorder-branch && git checkout - && git branch -D /tmp/test-recorder-branch"
        echo "  $REPLAY_DST --tail 5"
        ;;
    verify)
        verify
        ;;
    remove)
        remove
        ;;
esac
