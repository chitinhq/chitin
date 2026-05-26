#!/usr/bin/env bash
# install-chitin-orchestrator.sh — idempotent installer for the Chitin
# Orchestrator worker host.
#
# Builds go/orchestrator/cmd/chitin-orchestrator into ~/.local/bin and
# installs the tracked user unit under ~/.config/systemd/user/. Also installs
# the report freshness canary default config when the operator has not already
# customized one.
#
# Usage:
#   ./install-chitin-orchestrator.sh           # build + install/update
#   ./install-chitin-orchestrator.sh --verify  # check installed state
#   ./install-chitin-orchestrator.sh --remove  # uninstall

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ORCH_DIR="$REPO_ROOT/go/orchestrator"
LOCAL_BIN="${HOME}/.local/bin"
SYSD_USER_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

BIN_NAME="chitin-orchestrator"
BIN_PATH="$LOCAL_BIN/$BIN_NAME"
UNIT_NAME="chitin-orchestrator.service"
UNIT_SRC="$REPO_ROOT/swarm/systemd/$UNIT_NAME"
UNIT_DEST="$SYSD_USER_DIR/$UNIT_NAME"
REPORT_FRESHNESS_CONFIG_SRC="$ORCH_DIR/internal/reportfreshness/default-config.yaml"
REPORT_FRESHNESS_CONFIG_DEST="${CHITIN_REPORT_FRESHNESS_CONFIG:-$HOME/.chitin/report-freshness.yaml}"

require_go() {
    if ! command -v go >/dev/null 2>&1; then
        echo "FATAL: go is required to build $BIN_NAME" >&2
        exit 2
    fi
}

check_installed() {
    local ok=true

    if [[ -x "$BIN_PATH" ]]; then
        echo "INSTALLED binary: $BIN_PATH"
    else
        echo "MISSING binary: $BIN_PATH"
        ok=false
    fi

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

    if [[ -f "$REPORT_FRESHNESS_CONFIG_DEST" ]]; then
        echo "INSTALLED report freshness config: $REPORT_FRESHNESS_CONFIG_DEST"
    else
        echo "MISSING report freshness config: $REPORT_FRESHNESS_CONFIG_DEST"
        ok=false
    fi

    $ok
}

build_binary() {
    mkdir -p "$LOCAL_BIN"
    (
        cd "$ORCH_DIR"
        go build -o "$BIN_PATH" ./cmd/chitin-orchestrator
    )
    chmod 0755 "$BIN_PATH"
    echo "[install] built $BIN_PATH"
}

install_unit() {
    mkdir -p "$SYSD_USER_DIR"
    install -m 0644 "$UNIT_SRC" "$UNIT_DEST"
    echo "[install] installed $UNIT_DEST"
}

install_report_freshness_config() {
    mkdir -p "$(dirname "$REPORT_FRESHNESS_CONFIG_DEST")"
    if [[ -f "$REPORT_FRESHNESS_CONFIG_DEST" ]]; then
        echo "[install] kept existing $REPORT_FRESHNESS_CONFIG_DEST"
        return
    fi
    install -m 0644 "$REPORT_FRESHNESS_CONFIG_SRC" "$REPORT_FRESHNESS_CONFIG_DEST"
    echo "[install] installed $REPORT_FRESHNESS_CONFIG_DEST"
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
        rm -f "$UNIT_DEST" "$BIN_PATH"
        echo "[remove] removed $UNIT_DEST"
        echo "[remove] removed $BIN_PATH"
        ;;

    install|"")
        require_go

        if [[ ! -d "$ORCH_DIR" ]]; then
            echo "FATAL: orchestrator module not found at $ORCH_DIR" >&2
            exit 1
        fi
        if [[ ! -f "$UNIT_SRC" ]]; then
            echo "FATAL: tracked unit not found at $UNIT_SRC" >&2
            exit 1
        fi

        build_binary
        install_unit
        install_report_freshness_config

        if command -v systemctl >/dev/null 2>&1; then
            systemctl --user daemon-reload
            systemctl --user enable --now "$UNIT_NAME"
        else
            echo "WARN: systemctl not found; installed files but did not enable $UNIT_NAME" >&2
        fi

        echo "[install] verify with: $0 --verify"
        echo ""
        echo "Service commands:"
        echo "  systemctl --user status $UNIT_NAME"
        echo "  journalctl --user -u $UNIT_NAME -n 100 --no-pager"
        ;;

    *)
        echo "Usage: $0 [install|--verify|--remove]" >&2
        exit 1
        ;;
esac
