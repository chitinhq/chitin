#!/usr/bin/env bash
# install-clawta-pr-lifecycle.sh — idempotent installer for the clawta-pr-lifecycle script
# Symlinks swarm/bin/clawta-pr-lifecycle to ~/.openclaw/bin/clawta-pr-lifecycle.
# Same pattern as install-hermes-clawta-bridge.sh (#686) and install-board-watchdog-prompt.sh (#710).
# Usage: ./install-clawta-pr-lifecycle.sh [--verify]
#   --verify: check that the installed script matches the repo source; exit 0 if match, 1 if drift

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SOURCE_FILE="$REPO_ROOT/swarm/bin/clawta-pr-lifecycle"
INSTALL_DIR="$HOME/.openclaw/bin"
INSTALL_FILE="$INSTALL_DIR/clawta-pr-lifecycle"

if [[ ! -f "$SOURCE_FILE" ]]; then
    echo "ERROR: Source file not found at $SOURCE_FILE" >&2
    exit 1
fi

if [[ "${1:-}" == "--verify" ]]; then
    # Verify mode: check that the installed script matches the repo source
    if [[ ! -f "$INSTALL_FILE" ]]; then
        echo "DRIFT: Installed file not found at $INSTALL_FILE" >&2
        echo "  Source: $SOURCE_FILE" >&2
        echo "  Expected: $INSTALL_FILE" >&2
        exit 1
    fi

    # If it's a symlink, resolve and compare
    if [[ -L "$INSTALL_FILE" ]]; then
        RESOLVED="$(readlink -f "$INSTALL_FILE")"
        EXPECTED="$(readlink -f "$SOURCE_FILE")"
        if [[ "$RESOLVED" == "$EXPECTED" ]]; then
            echo "OK: clawta-pr-lifecycle symlink matches repo source"
            exit 0
        else
            echo "DRIFT: clawta-pr-lifecycle symlink resolves to $RESOLVED, expected $EXPECTED" >&2
            exit 1
        fi
    fi

    # If it's a regular file, compare content
    SOURCE_HASH="$(sha256sum "$SOURCE_FILE" | cut -d' ' -f1)"
    INSTALL_HASH="$(sha256sum "$INSTALL_FILE" | cut -d' ' -f1)"

    if [[ "$SOURCE_HASH" == "$INSTALL_HASH" ]]; then
        echo "OK: clawta-pr-lifecycle matches repo source"
        exit 0
    else
        echo "DRIFT: clawta-pr-lifecycle differs from repo source" >&2
        echo "  Source:  $SOURCE_FILE (sha256: $SOURCE_HASH)" >&2
        echo "  Installed: $INSTALL_FILE (sha256: $INSTALL_HASH)" >&2
        diff "$SOURCE_FILE" "$INSTALL_FILE" >&2 || true
        exit 1
    fi
else
    # Install mode: create symlink from install location to repo source
    mkdir -p "$INSTALL_DIR"

    # If already a correct symlink, noop
    if [[ -L "$INSTALL_FILE" ]]; then
        RESOLVED="$(readlink -f "$INSTALL_FILE")"
        EXPECTED="$(readlink -f "$SOURCE_FILE")"
        if [[ "$RESOLVED" == "$EXPECTED" ]]; then
            echo "OK: clawta-pr-lifecycle symlink already correct -> $RESOLVED"
            exit 0
        fi
    fi

    # Backup existing file if present
    if [[ -f "$INSTALL_FILE" && ! -L "$INSTALL_FILE" ]]; then
        BACKUP="${INSTALL_FILE}.bak.$(date +%Y%m%d-%H%M%S)"
        echo "Backing up existing file to $BACKUP"
        mv "$INSTALL_FILE" "$BACKUP"
    fi

    # Remove old symlink if present
    if [[ -L "$INSTALL_FILE" ]]; then
        rm "$INSTALL_FILE"
    fi

    # Create symlink
    ln -s "$SOURCE_FILE" "$INSTALL_FILE"
    chmod +x "$SOURCE_FILE"

    echo "OK: clawta-pr-lifecycle symlinked -> $SOURCE_FILE"

    # Verify the symlink works
    if "$INSTALL_FILE" --help &>/dev/null || python3 "$INSTALL_FILE" --help &>/dev/null; then
        echo "OK: clawta-pr-lifecycle runs successfully"
    else
        echo "WARN: clawta-pr-lifecycle symlinked but --help check failed (may be expected for arg-required scripts)"
    fi
fi