#!/usr/bin/env bash
# install-chitin-console.sh — idempotent installer for the Chitin Console
# operator web UI as a persistent systemd user service (spec 080 US3).
#
# Builds the chitin-console bundle (a build verification + cache warm), then
# installs the tracked user unit under ~/.config/systemd/user/ and enables it.
# The service itself serves the console via `nx serve` under systemd
# supervision, so it is always-on, restarts on failure, and starts on boot.
#
# Usage:
#   ./install-chitin-console.sh           # build + install/update + (re)start
#   ./install-chitin-console.sh --verify  # check installed state
#   ./install-chitin-console.sh --remove  # uninstall

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SYSD_USER_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

UNIT_NAME="chitin-console.service"
UNIT_SRC="$REPO_ROOT/swarm/systemd/$UNIT_NAME"
UNIT_DEST="$SYSD_USER_DIR/$UNIT_NAME"

require_npx() {
    if ! command -v npx >/dev/null 2>&1; then
        echo "FATAL: npx is required to build and serve chitin-console" >&2
        exit 2
    fi
}

check_installed() {
    local ok=true

    if [[ -f "$UNIT_DEST" ]]; then
        if cmp -s "$UNIT_SRC" "$UNIT_DEST"; then
            echo "INSTALLED unit: $UNIT_DEST"
        else
            echo "DRIFT unit: $UNIT_DEST"
            ok=false
        fi
    else
        echo "MISSING unit: $UNIT_DEST"
        ok=false
    fi

    if command -v systemctl >/dev/null 2>&1 && systemctl --user is-enabled "$UNIT_NAME" >/dev/null 2>&1; then
        echo "ENABLED unit: $UNIT_NAME"
    else
        echo "DISABLED unit: $UNIT_NAME"
        ok=false
    fi

    if command -v systemctl >/dev/null 2>&1 && systemctl --user is-active "$UNIT_NAME" >/dev/null 2>&1; then
        echo "ACTIVE unit: $UNIT_NAME"
    else
        echo "INACTIVE unit: $UNIT_NAME"
        ok=false
    fi

    $ok
}

build_console() {
    # A build verification — `nx serve` rebuilds at runtime, but building here
    # fails the install early on a broken bundle and warms the Nx cache.
    ( cd "$REPO_ROOT" && npx nx build chitin-console )
    echo "[install] chitin-console bundle built"
}

install_unit() {
    mkdir -p "$SYSD_USER_DIR"
    install -m 0644 "$UNIT_SRC" "$UNIT_DEST"
    echo "[install] installed $UNIT_DEST"
}

case "${1:-install}" in
    --verify)
        check_installed
        ;;

    --remove)
        if command -v systemctl >/dev/null 2>&1; then
            systemctl --user disable --now "$UNIT_NAME" >/dev/null 2>&1 || true
            systemctl --user daemon-reload || true
        fi
        rm -f "$UNIT_DEST"
        echo "[remove] removed $UNIT_DEST"
        ;;

    install|"")
        require_npx

        if [[ ! -f "$UNIT_SRC" ]]; then
            echo "FATAL: tracked unit not found at $UNIT_SRC" >&2
            exit 1
        fi

        build_console
        install_unit

        if command -v systemctl >/dev/null 2>&1; then
            systemctl --user daemon-reload
            systemctl --user enable "$UNIT_NAME"
            # restart (not just enable --now) so a re-install always applies a
            # changed unit file to the running service.
            systemctl --user restart "$UNIT_NAME"
        else
            echo "WARN: systemctl not found; installed the unit but did not enable it" >&2
        fi

        echo "[install] verify with: $0 --verify"
        echo ""
        echo "Console will be at: http://127.0.0.1:4280"
        echo "Service commands:"
        echo "  systemctl --user status $UNIT_NAME"
        echo "  journalctl --user -u $UNIT_NAME -n 100 --no-pager"
        ;;

    *)
        echo "Usage: $0 [install|--verify|--remove]" >&2
        exit 1
        ;;
esac
