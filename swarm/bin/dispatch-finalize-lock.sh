#!/usr/bin/env bash
# Spec 025 R1 — per-ticket-id flock helper for the dispatch atomicity invariant.
#
# spec: 025-dispatch-atomicity-invariant
#
# Usage:
#   dispatch-finalize-lock.sh acquire <ticket_id>     # tries to acquire; exits 75 if held
#   dispatch-finalize-lock.sh release <ticket_id>     # explicit release (rare; fd close releases)
#   dispatch-finalize-lock.sh path    <ticket_id>     # print lockfile path
#
# Lock storage: ~/.chitin/locks/dispatch-<ticket_id>.lock
#
# Acquired with `flock --exclusive --nonblock`. Lock auto-releases on fd close
# (process exit, normal or crash). No manual cleanup needed.
#
# Callers should:
#   1. Use `flock -nE 75 "$(dispatch-finalize-lock.sh path TID)" -c "<command>"`
#      so the lock is held only for the duration of <command>.
#   2. Or open the lock file with `exec {fd}>"$LOCKFILE"`, `flock -n -E 75 $fd`,
#      and rely on fd close at script exit.
#
# Exit codes:
#   0  — acquire succeeded (in --command mode) or release succeeded
#   75 — lock not acquired (held by another process)
#   2  — usage error
#   3  — lockfile dir creation failed

set -uo pipefail

LOCK_ROOT="${LOCK_ROOT:-${HOME}/.chitin/locks}"

usage() {
    cat <<USAGE
usage: $(basename "$0") <acquire|release|path> <ticket_id> [-- <command> [args...]]

  acquire — flock-nonblock on the lockfile; exit 0 if got it, 75 if held.
            If -- <command> is provided, runs the command under the lock
            (lock released on command exit).
  release — best-effort unlink of the lockfile (rare; usually fd close suffices).
  path    — print the lockfile path for use with shell redirection.
USAGE
    exit 2
}

[[ $# -lt 2 ]] && usage

ACTION="$1"
TID="$2"
shift 2

# Sanity: ticket id must match expected shape (defense against arbitrary paths)
if ! [[ "$TID" =~ ^t_[a-zA-Z0-9_-]+$ ]]; then
    echo "dispatch-finalize-lock: invalid ticket id: $TID" >&2
    exit 2
fi

mkdir -p "$LOCK_ROOT" || { echo "dispatch-finalize-lock: cannot create $LOCK_ROOT" >&2; exit 3; }
LOCKFILE="${LOCK_ROOT}/dispatch-${TID}.lock"

case "$ACTION" in
    acquire)
        if [[ $# -ge 1 ]] && [[ "$1" == "--" ]]; then
            shift
            # Run the command under the lock; fd is auto-released on exit.
            exec flock -nE 75 "$LOCKFILE" -c "$*"
        fi
        # Bare acquire: open fd, try lock, exit 0 if held by us (caller responsible
        # for re-acquire pattern via fd redirection).
        exec {LOCKFD}>"$LOCKFILE"
        if flock -nE 75 "$LOCKFD"; then
            echo "$$" >&"$LOCKFD"
            exit 0
        else
            echo "dispatch-finalize-lock: $TID lock held by another process" >&2
            exit 75
        fi
        ;;
    release)
        # Best-effort: remove the lock file. Other holders see ENOENT on next
        # acquire which is fine; fcntl uses open fd not file path for the lock.
        rm -f "$LOCKFILE"
        exit 0
        ;;
    path)
        echo "$LOCKFILE"
        exit 0
        ;;
    *)
        usage
        ;;
esac
