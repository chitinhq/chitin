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

set -uo pipefail
# NOTE: deliberately NO `-e`. Bash's `set -e` + `pipefail` causes
# `x=$(failing-cmd | tail -1)` to abort the script silently when
# `failing-cmd` exits non-zero — `tail` succeeds on empty input, but
# pipefail surfaces the leftmost failure to the assignment, which
# `set -e` then treats as fatal. The earlier shape made the
# `emit fail envelope-create-failed; exit 3` branch dead code: a
# kernel error produced exit=1 with NO log entry. Explicit error
# checking via captured exit codes is more honest here.

# Resolve chitin's state dir the same way the kernel does:
#   1. CHITIN_HOME env (kernel's `chitinDir()` honors this)
#   2. CHITIN_DIR env (rotator-only convention; defers to CHITIN_HOME
#      when both set)
#   3. $HOME/.chitin (default)
# If only CHITIN_DIR was honored and chitin.env sets CHITIN_HOME=/x,
# the rotator updates /home/<user>/.chitin/current-envelope but the
# kernel reads /x/current-envelope — rotation silently no-ops.
CHITIN_HOME="${CHITIN_HOME:-${CHITIN_DIR:-$HOME/.chitin}}"
CURRENT_ENVELOPE_FILE="$CHITIN_HOME/current-envelope"

# Default caps. Override via env vars if the operator wants
# tighter or looser limits per-rotation.
ENV_CALLS="${CHITIN_ENVELOPE_CALLS:-10000}"
ENV_BYTES="${CHITIN_ENVELOPE_BYTES:-33554432}"   # 32 MB
ENV_USD="${CHITIN_ENVELOPE_USD:-2.0}"

LOG="${CHITIN_ENVELOPE_ROTATE_LOG:-$HOME/.cache/chitin/envelope-rotate.jsonl}"
mkdir -p "$(dirname "$LOG")" "$CHITIN_HOME"

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

# Hard-fail on missing prerequisites — without these the rotator
# silently passes through and rotation effectively never happens.
if ! command -v chitin-kernel >/dev/null; then
  emit fail kernel-binary-missing
  exit 2
fi
if ! command -v jq >/dev/null; then
  emit fail jq-missing "rotator depends on jq for envelope state introspection"
  exit 2
fi

# Read the current pointer (may be missing).
current_id=""
if [[ -f "$CURRENT_ENVELOPE_FILE" ]]; then
  current_id=$(tr -d '[:space:]' < "$CURRENT_ENVELOPE_FILE")
fi

# Check whether the current envelope is still open. Use --limit=0
# (no limit) so the lookup doesn't drop the current envelope out
# of the response after enough rotation history accumulates —
# `envelope list` defaults to 20, so without --limit=0 this check
# would silently false-fail after ~20 rotations and trigger a
# fresh create every tick (spend leak that compounds).
needs_rotation=1
list_failed=0
if [[ -n "$current_id" ]]; then
  list_out=$(chitin-kernel envelope list --limit=0 2>&1)
  list_rc=$?
  if [[ "$list_rc" -ne 0 ]]; then
    emit warn envelope-list-failed "rc=$list_rc tail=$(echo "$list_out" | tail -c 300 | tr '\n' ' ')"
    list_failed=1
  else
    if echo "$list_out" | jq -e --arg id "$current_id" '
          .[] | select(.id == $id) | (.closed_at == null)
        ' >/dev/null 2>&1; then
      needs_rotation=0
    fi
  fi
fi

if [[ "$needs_rotation" -eq 0 ]]; then
  emit noop "current envelope $current_id is open — no rotation needed"
  exit 0
fi

# Don't create a new envelope if we couldn't introspect — the
# existing pointer might still be fine; we just couldn't verify.
# Better to wait for the next tick than to leak spend.
if [[ "$list_failed" -eq 1 ]]; then
  emit skip "envelope list failed; preserving existing pointer to avoid spend leak"
  exit 0
fi

# Rotate: create a fresh envelope, update the pointer. Capture
# exit code explicitly so a kernel failure produces a real fail
# emit (not silent script abort under set -e).
create_out=$(chitin-kernel envelope create \
  --calls="$ENV_CALLS" \
  --bytes="$ENV_BYTES" \
  --usd="$ENV_USD" 2>&1)
create_rc=$?
new_id=$(echo "$create_out" | tail -1 | tr -d '[:space:]')

if [[ "$create_rc" -ne 0 ]] || [[ -z "$new_id" ]] || ! [[ "$new_id" =~ ^[A-Z0-9]+$ ]]; then
  emit fail envelope-create-failed "rc=$create_rc raw_output=$(echo "$create_out" | tail -c 300 | tr '\n' ' ')"
  exit 3
fi

# Update the pointer via the kernel's `envelope use` so the write
# matches kernel semantics: write to a tmp file in the same dir,
# fsync, rename. Pre-2026-05-04 this was bash `echo > tmp && mv` —
# atomic for the rename but no fsync, so a crash between the
# rename and the page cache flush could surface either an empty
# `current-envelope` or torn content to a concurrent reader. The
# kernel's writeCurrentEnvelope (cmd/chitin-kernel/envelope.go)
# is the authoritative implementation; defer to it.
use_out=$(chitin-kernel envelope use "$new_id" 2>&1)
use_rc=$?
if [[ "$use_rc" -ne 0 ]]; then
  emit fail envelope-use-failed "rc=$use_rc raw_output=$(echo "$use_out" | tail -c 300 | tr '\n' ' ')"
  exit 4
fi

emit ok rotated "old_id=${current_id:-<none>}" "new_id=$new_id" "calls=$ENV_CALLS" "bytes=$ENV_BYTES" "usd=$ENV_USD"
