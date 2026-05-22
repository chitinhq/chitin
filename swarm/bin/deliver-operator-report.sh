#!/usr/bin/env bash
# deliver-operator-report.sh — compose an operator report via the chitin
# kernel and deliver it to the operator's Discord (spec 085).
#
# Usage: deliver-operator-report.sh {heartbeat|digest} [--on-demand]
#
# Composition is `chitin-kernel report <kind>` — side-effect-free. Delivery is
# `openclaw message send` — the kernel never posts (Constitution §1). Every run
# appends exactly one ReportDeliveryRecord to the audit log so a missed report
# is never silent (spec 085 FR-010, contract C2).
#
# Exit codes:
#   0  a message reached the operator (the report, or a failure notice)
#   1  delivery failed — compose succeeded but the report did not reach Discord
#   2  compose failed AND the fallback failure notice could not be delivered
#
# Config (env):
#   CHITIN_KERNEL_BIN                kernel binary (default: chitin-kernel on PATH)
#   CHITIN_OPERATOR_DISCORD_ACCOUNT  openclaw account (default: default)
#   CHITIN_OPERATOR_DISCORD_TARGET   channel:<id> — operator's report channel (required)
#   CHITIN_OPERATOR_REPORT_LOG       audit log (default: ~/.cache/chitin/operator-report.jsonl)
#   CHITIN_REPORT_COOLDOWN_SECONDS   on-demand coalescing window (default: 120)
set -uo pipefail

KIND="${1:-}"
shift || true
TRIGGER="scheduled"
while (($#)); do
  case "$1" in
    --on-demand) TRIGGER="on-demand" ;;
    *) ;;
  esac
  shift
done

case "$KIND" in
  heartbeat | digest) ;;
  *)
    echo "usage: deliver-operator-report.sh {heartbeat|digest} [--on-demand]" >&2
    exit 2
    ;;
esac

KERNEL="${CHITIN_KERNEL_BIN:-chitin-kernel}"
ACCOUNT="${CHITIN_OPERATOR_DISCORD_ACCOUNT:-default}"
TARGET="${CHITIN_OPERATOR_DISCORD_TARGET:-}"
LOG="${CHITIN_OPERATOR_REPORT_LOG:-$HOME/.cache/chitin/operator-report.jsonl}"
COOLDOWN="${CHITIN_REPORT_COOLDOWN_SECONDS:-120}"
mkdir -p "$(dirname "$LOG")"

# audit appends one ReportDeliveryRecord (spec 085 data-model) per run.
audit() {
  local outcome="$1" detail="$2"
  detail="${detail//\\/\\\\}"
  detail="${detail//\"/\\\"}"
  detail="${detail//$'\n'/ }"
  printf '{"ts":"%s","kind":"%s","outcome":"%s","trigger":"%s","detail":"%s"}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$KIND" "$outcome" "$TRIGGER" "$detail" >>"$LOG"
}

# On-demand coalescing: if an on-demand report of this kind was delivered
# within the cooldown window, skip — the operator already has a fresh one
# (FR-014). Scheduled runs are never coalesced.
if [[ "$TRIGGER" == "on-demand" && -f "$LOG" ]]; then
  now=$(date +%s)
  last=$(grep "\"kind\":\"$KIND\"" "$LOG" 2>/dev/null | grep '"outcome":"delivered"' | tail -1)
  if [[ -n "$last" ]]; then
    last_ts=$(printf '%s' "$last" | sed -n 's/.*"ts":"\([^"]*\)".*/\1/p')
    last_epoch=$(date -d "$last_ts" +%s 2>/dev/null || echo 0)
    if ((last_epoch > 0 && now - last_epoch < COOLDOWN)); then
      echo "deliver-operator-report: coalesced — a $KIND was delivered within the ${COOLDOWN}s cooldown" >&2
      exit 0
    fi
  fi
fi

# Compose. The kernel command is side-effect-free. If it fails, still deliver a
# minimal notice so a broken report pipeline is itself visible to the operator.
if MESSAGE=$("$KERNEL" report "$KIND" 2>/dev/null) && [[ -n "$MESSAGE" ]]; then
  COMPOSE_OK=1
else
  COMPOSE_OK=0
  MESSAGE="⚠ chitin $KIND report could not be composed — the report pipeline needs attention."
fi

# Deliver — to the operator-configured destination only (FR-013).
if [[ -z "$TARGET" ]]; then
  audit failed "CHITIN_OPERATOR_DISCORD_TARGET is unset — nowhere to deliver"
  echo "deliver-operator-report: CHITIN_OPERATOR_DISCORD_TARGET is unset" >&2
  exit 1
fi

if openclaw message send --channel discord --account "$ACCOUNT" --target "$TARGET" --text "$MESSAGE" >/dev/null 2>&1; then
  if [[ "$COMPOSE_OK" -eq 1 ]]; then
    audit delivered "$TARGET"
  else
    # The operator was informed of the failure — FR-010 satisfied — but the
    # real report did not go out, so the audit records a failure.
    audit failed "compose failed; delivered fallback notice to $TARGET"
  fi
  exit 0
fi

audit failed "openclaw delivery to $TARGET failed"
echo "deliver-operator-report: openclaw delivery failed" >&2
if [[ "$COMPOSE_OK" -eq 1 ]]; then
  exit 1
fi
exit 2
