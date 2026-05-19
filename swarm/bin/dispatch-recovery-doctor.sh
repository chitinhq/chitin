#!/usr/bin/env bash
# Spec 036 Inv-4 — dispatch recovery doctor.
#
# spec: 036-dispatch-fault-tolerance-invariants
#
# Detects + recovers from the most common dispatch-dead conditions:
#   1. openclaw-gateway crashed (port 18789 not listening) → systemctl restart
#   2. Stale agent/<driver>-* local branches accumulated → recommend cleanup
#      (does NOT delete automatically — operator-attended)
#
# Idempotent. Safe to run from cron.
#
# Usage:
#   dispatch-recovery-doctor.sh            # diagnose + repair
#   dispatch-recovery-doctor.sh --check    # diagnose only (exit 0 if healthy, 1 if degraded)
#   dispatch-recovery-doctor.sh --gateway-only  # only check + restart the gateway

set -uo pipefail

GATEWAY_HEALTH_URL="${GATEWAY_HEALTH_URL:-http://127.0.0.1:18789/health}"
GATEWAY_SERVICE="${GATEWAY_SERVICE:-openclaw-gateway}"
SYSTEMCTL_BIN="${SYSTEMCTL_BIN:-systemctl}"
CURL_BIN="${CURL_BIN:-curl}"

CHECK_ONLY=0
GATEWAY_ONLY=0
for arg in "$@"; do
    case "$arg" in
        --check) CHECK_ONLY=1 ;;
        --gateway-only) GATEWAY_ONLY=1 ;;
        -h|--help)
            sed -n '2,/^set -uo/p' "$0" | sed 's/^# \{0,1\}//;$d'
            exit 0 ;;
        *) echo "unknown arg: $arg" >&2; exit 2 ;;
    esac
done

problems=0
fixes_applied=0

# --- Inv-4 check: openclaw-gateway health ---
check_gateway() {
    local body
    body=$("$CURL_BIN" -s --max-time 5 "$GATEWAY_HEALTH_URL" 2>/dev/null || true)
    if [[ "$body" == *'"ok":true'* ]] || [[ "$body" == *'"status":"live"'* ]]; then
        echo "[doctor] openclaw-gateway: healthy"
        return 0
    fi
    echo "[doctor] openclaw-gateway: UNHEALTHY (body=${body:-<empty>})" >&2
    problems=$((problems + 1))
    if [[ "$CHECK_ONLY" -eq 1 ]]; then
        return 1
    fi
    echo "[doctor] restarting $GATEWAY_SERVICE…"
    if "$SYSTEMCTL_BIN" --user restart "$GATEWAY_SERVICE" 2>&1; then
        sleep 3
        body=$("$CURL_BIN" -s --max-time 5 "$GATEWAY_HEALTH_URL" 2>/dev/null || true)
        if [[ "$body" == *'"ok":true'* ]] || [[ "$body" == *'"status":"live"'* ]]; then
            echo "[doctor] openclaw-gateway: recovered"
            fixes_applied=$((fixes_applied + 1))
            return 0
        fi
        echo "[doctor] openclaw-gateway: restart returned but health still failing" >&2
        return 1
    fi
    echo "[doctor] systemctl restart FAILED" >&2
    return 1
}

# --- Inv-3 check: stale local agent branches in bench-devs-platform ---
check_stale_branches() {
    [[ "$GATEWAY_ONLY" -eq 1 ]] && return 0
    local bench=~/workspace/bench-devs-platform
    [[ -d "$bench/.git" ]] || return 0
    local stale
    stale=$(cd "$bench" && git branch 2>/dev/null | grep -c 'agent/codex-' || true)
    if [[ "$stale" -gt 5 ]]; then
        echo "[doctor] stale agent/codex-* branches: $stale (threshold 5)" >&2
        echo "[doctor]   operator-attended cleanup:" >&2
        echo "[doctor]   for br in \$(git -C $bench branch | grep agent/codex-); do git -C $bench worktree remove --force ~/.cache/chitin/swarm-worktrees/codex-\${br#*-}; git -C $bench branch -D \"\$br\"; done" >&2
        problems=$((problems + 1))
    else
        echo "[doctor] agent/codex-* branches: $stale (ok)"
    fi
}

check_gateway || true
check_stale_branches

echo "[doctor] summary: problems=$problems fixes_applied=$fixes_applied"
if [[ "$CHECK_ONLY" -eq 1 ]] && [[ "$problems" -gt 0 ]]; then
    exit 1
fi
exit 0
