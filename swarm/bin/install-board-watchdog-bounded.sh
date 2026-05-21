#!/usr/bin/env bash
# install-board-watchdog-bounded.sh — idempotent installer for the
# board-watchdog runtime script.
#
# The board-watchdog cron (job 388e38b20bd5, `no_agent: true`) executes
# this script on every tick (default every 10 minutes). Hermes' no-agent
# guard hard-confines such scripts to ~/.hermes/scripts/ and REJECTS
# symlinks pointing outside that directory (see thread 5 msg 2378 —
# Clawta's diagnosis). So this installer COPIES the tracked source into
# place rather than symlinking it; --verify uses content compare (cmp).
#
# Per Constitution §6: tracked source over local patches. Drift between
# repo source and deployed runtime is a bug.
#
# Usage:
#   ./install-board-watchdog-bounded.sh            copy tracked source → runtime
#   ./install-board-watchdog-bounded.sh --verify   exit 0 if content matches
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TRACKED_SOURCE="$REPO_ROOT/swarm/bin/board-watchdog-bounded.py"
RUNTIME_TARGET="$HOME/.hermes/scripts/board-watchdog-bounded.py"
RUNTIME_DIR="$(dirname "$RUNTIME_TARGET")"

if [[ ! -f "$TRACKED_SOURCE" ]]; then
    echo "ERROR: Tracked source not found at $TRACKED_SOURCE" >&2
    exit 1
fi

if [[ "${1:-}" == "--verify" ]]; then
    if [[ ! -e "$RUNTIME_TARGET" ]]; then
        echo "DRIFT: Runtime script missing at $RUNTIME_TARGET" >&2
        exit 1
    fi
    if [[ -L "$RUNTIME_TARGET" ]]; then
        echo "DRIFT: $RUNTIME_TARGET is a symlink; Hermes no-agent guard rejects symlinks." >&2
        echo "HINT: re-run without --verify to replace with a real-file copy." >&2
        exit 1
    fi
    if cmp -s "$TRACKED_SOURCE" "$RUNTIME_TARGET"; then
        echo "OK: $RUNTIME_TARGET matches tracked source"
        exit 0
    fi
    echo "DRIFT: $RUNTIME_TARGET differs from tracked source" >&2
    diff -u "$TRACKED_SOURCE" "$RUNTIME_TARGET" | head -40 >&2 || true
    exit 1
fi

mkdir -p "$RUNTIME_DIR"

# Back up if there's an existing real file that differs (preserves operator's
# in-flight tweaks for forensics). Symlinks are replaced silently.
if [[ -f "$RUNTIME_TARGET" && ! -L "$RUNTIME_TARGET" ]] && ! cmp -s "$TRACKED_SOURCE" "$RUNTIME_TARGET"; then
    backup="$RUNTIME_TARGET.bak.$(date +%Y%m%d-%H%M%S)"
    cp "$RUNTIME_TARGET" "$backup"
    echo "Backed up differing runtime to $backup"
fi

# Use install(1) for atomic replace + mode preservation. Falls back to cp + chmod
# if install isn't available (rare on Linux).
if command -v install >/dev/null 2>&1; then
    install -m 0755 "$TRACKED_SOURCE" "$RUNTIME_TARGET"
else
    cp "$TRACKED_SOURCE" "$RUNTIME_TARGET"
    chmod 0755 "$RUNTIME_TARGET"
fi
echo "OK: $RUNTIME_TARGET ← $TRACKED_SOURCE (real-file copy, not symlink)"
