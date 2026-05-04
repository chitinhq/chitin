#!/usr/bin/env bash
# chitin-envelope-rotate — keep ~/.chitin/current-envelope pointing
# at an OPEN budget envelope so the kernel never deny-cascades on
# `envelope-closed`.
#
# Triggered by chitin-envelope-rotate.timer (every 5 minutes).
# Idempotent: if the active envelope is open, no-op. If it's
# closed (or missing), create a fresh one and update the pointer.
#
# Background:
# `gov.Gate.Evaluate` resolves an envelope via:
#   1. --envelope=<id> flag
#   2. CHITIN_BUDGET_ENVELOPE env
#   3. ~/.chitin/current-envelope file
#   4. None — gate evaluates without spend
#
# In production, agents resolve via #3. When the envelope hits its
# tool-call cap, it sticky-closes; subsequent calls return
# `envelope-closed`. Without this rotator, the operator must
# manually run `chitin-kernel envelope create` and update
# current-envelope — which we forgot to do, and the swarm
# silently stopped producing PRs for ~4 hours on 2026-05-04.
#
# Caps default to "generous enough that a healthy day's swarm +
# operator activity doesn't trip them, low enough that runaway
# spend gets caught fast." Operator can override via env vars.

set -euo pipefail

CHITIN_DIR="${CHITIN_DIR:-$HOME/.chitin}"
CURRENT_ENVELOPE_FILE="$CHITIN_DIR/current-envelope"

# Default caps. Override via env vars if the operator wants
# tighter or looser limits per-rotation.
ENV_CALLS="${CHITIN_ENVELOPE_CALLS:-10000}"
ENV_BYTES="${CHITIN_ENVELOPE_BYTES:-33554432}"   # 32 MB
ENV_USD="${CHITIN_ENVELOPE_USD:-2.0}"

LOG="${CHITIN_ENVELOPE_ROTATE_LOG:-$HOME/.cache/chitin/envelope-rotate.jsonl}"
mkdir -p "$(dirname "$LOG")" "$CHITIN_DIR"

emit() {
  local kind="$1" msg="$2"
  shift 2
  local extras=""
  while (($#)); do
    local k="${1%%=*}" v="${1#*=}"
    v="${v//\\/\\\\}"; v="${v//\"/\\\"}"
    extras+=",\"${k}\":\"${v}\""
    shift
  done
  printf '{"ts":"%s","kind":"%s","msg":"%s"%s}\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$kind" "$msg" "$extras" \
    | tee -a "$LOG" >&2
}

if ! command -v chitin-kernel >/dev/null; then
  emit fail kernel-binary-missing
  exit 2
fi

# Read the current pointer (may be missing).
current_id=""
if [[ -f "$CURRENT_ENVELOPE_FILE" ]]; then
  current_id=$(tr -d '[:space:]' < "$CURRENT_ENVELOPE_FILE")
fi

# Check whether the current envelope is still open. We grep
# `chitin-kernel envelope list` (returns one line of JSON);
# the envelope is open iff its closed_at is null.
needs_rotation=1
if [[ -n "$current_id" ]]; then
  if chitin-kernel envelope list 2>/dev/null \
    | jq -e --arg id "$current_id" '
        .[] | select(.id == $id) | (.closed_at == null)
      ' >/dev/null 2>&1; then
    needs_rotation=0
  fi
fi

if [[ "$needs_rotation" -eq 0 ]]; then
  emit noop "current envelope $current_id is open — no rotation needed"
  exit 0
fi

# Rotate: create a fresh envelope, update the pointer.
new_id=$(chitin-kernel envelope create \
  --calls="$ENV_CALLS" \
  --bytes="$ENV_BYTES" \
  --usd="$ENV_USD" 2>&1 | tail -1 | tr -d '[:space:]')

if [[ -z "$new_id" ]] || ! [[ "$new_id" =~ ^[A-Z0-9]+$ ]]; then
  emit fail envelope-create-failed "raw_output=$new_id"
  exit 3
fi

# Atomic-ish write: tmpfile + mv. Avoids a partial write under
# concurrent readers (every gate-evaluate hits this file).
tmp="$CURRENT_ENVELOPE_FILE.tmp.$$"
echo "$new_id" > "$tmp"
mv "$tmp" "$CURRENT_ENVELOPE_FILE"

emit ok rotated "old_id=${current_id:-<none>}" "new_id=$new_id" "calls=$ENV_CALLS" "bytes=$ENV_BYTES" "usd=$ENV_USD"
