#!/usr/bin/env bash
# chitin-chain-watch.sh — defense-in-depth detector for runaway lockdown
# loops in the gov-decisions chain.
#
# Background: the primary lockdown-loop fix is the claudecode driver's
# `continue:false` hook response (commit fb2f4db, 2026-05-06), which
# tells Claude Code to stop the agent loop instead of just blocking the
# current tool call. That fix covers all four hook drivers
# (claudecode/codex/gemini/hermes) since gate_hook.go uses one shared
# Format(). This script catches the residual cases:
#
#   1. A future driver (or harness change) that doesn't honor `continue:false`.
#   2. A regression in the formatter that drops the field silently.
#   3. An agent process that ignores the harness's stop signal.
#
# All three would manifest the same way: many lockdown decisions fired
# in tight succession against the same agent. The original 2026-05-06
# incident showed 26 fires from one envelope across 7+ hours; with the
# harness honoring `continue:false`, that count should be 1 per crossing
# of the threshold.
#
# Selection invariant: WARN if and only if
#   (A) >= CHITIN_CHAIN_WATCH_THRESHOLD (default 3) chain rows where
#   (B) rule_id == "lockdown" AND
#   (C) ts within the last CHITIN_CHAIN_WATCH_WINDOW_SEC (default 90s) AND
#   (D) all share the same agent name.
#
# When the optional CHITIN_CHAIN_WATCH_ACTION=reset is set, ALSO call
# `chitin-kernel gate reset --agent=<agent>` to break the loop. Default
# is warn-only because a manually-set lockdown
# (`chitin-kernel gate lockdown --agent=X`) plus a few rapid retries
# would otherwise auto-reset operator state. The slow-path recovery
# (chitin-agent-unlock, 60min lock-age threshold) handles legitimate
# threshold-triggered lockdowns.
#
# Exit codes:
#   0  no runaway detected, or all warns/resets emitted cleanly
#   1  partial failure (stderr carries structured error lines)

set -euo pipefail

CHITIN_DIR="${CHITIN_HOME:-$HOME/.chitin}"
KERNEL="${CHITIN_KERNEL_BIN:-$HOME/.local/bin/chitin-kernel}"
LOG="${CHITIN_CHAIN_WATCH_LOG:-$HOME/.cache/chitin/chain-watch.jsonl}"
WINDOW_SEC="${CHITIN_CHAIN_WATCH_WINDOW_SEC:-90}"
THRESHOLD="${CHITIN_CHAIN_WATCH_THRESHOLD:-3}"
ACTION="${CHITIN_CHAIN_WATCH_ACTION:-warn}"

mkdir -p "$(dirname "$LOG")"

ts_now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

emit_log() {
  local kind="$1" msg="$2"; shift 2
  local extras=""
  while (($#)); do
    local k="${1%%=*}" v="${1#*=}"
    v="${v//\\/\\\\}"; v="${v//\"/\\\"}"
    extras+=",\"${k}\":\"${v}\""
    shift
  done
  printf '{"ts":"%s","kind":"%s","msg":"%s"%s}\n' "$(ts_now)" "$kind" "$msg" "$extras" \
    | tee -a "$LOG" >&2
}

gen_uuid() {
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr '[:upper:]' '[:lower:]'
  else
    cat /proc/sys/kernel/random/uuid 2>/dev/null || \
      python3 -c "import uuid; print(uuid.uuid4())"
  fi
}

# emit_chain_event <agent> <count> <window_sec> <action_taken>
emit_chain_event() {
  local agent="$1" count="$2" window="$3" action_taken="$4"
  local chain_id ts event_file payload

  chain_id=$(gen_uuid)
  ts=$(ts_now)
  event_file=$(mktemp /tmp/chitin-chain-watch-XXXXXX.json)

  payload=$(jq -cn \
    --arg agent "$agent" \
    --argjson count "$count" \
    --argjson window_sec "$window" \
    --argjson threshold "$THRESHOLD" \
    --arg action "$action_taken" \
    '{agent: $agent,
      lockdown_count: $count,
      window_sec: $window_sec,
      threshold: $threshold,
      action: $action,
      rationale: "lockdown rule fired count >= threshold within window — primary continue:false stop signal likely not honored; operator should investigate harness/driver"}')

  jq -n \
    --arg schema_version "2" \
    --arg run_id "$chain_id" \
    --arg session_id "$chain_id" \
    --arg surface "system" \
    --arg agent_instance_id "$agent" \
    --arg event_type "lockdown_loop_detected" \
    --arg chain_id "$chain_id" \
    --arg chain_type "session" \
    --arg ts "$ts" \
    --argjson payload "$payload" \
    '{schema_version: $schema_version,
      run_id: $run_id,
      session_id: $session_id,
      surface: $surface,
      driver_identity: {user: "", machine_id: "", machine_fingerprint: ""},
      agent_instance_id: $agent_instance_id,
      agent_fingerprint: "",
      event_type: $event_type,
      chain_id: $chain_id,
      chain_type: $chain_type,
      seq: 0,
      ts: $ts,
      labels: {},
      payload: $payload}' > "$event_file"

  if "$KERNEL" emit --dir "$CHITIN_DIR" --event-file "$event_file" >/dev/null 2>&1; then
    :
  else
    emit_log warn "chain-event-emit-failed" "agent=$agent"
  fi
  rm -f "$event_file"
}

# ── preflight ─────────────────────────────────────────────────────────────────

if ! command -v jq >/dev/null 2>&1; then
  emit_log fail "jq-not-found"
  exit 1
fi

case "$ACTION" in
  warn|reset) ;;
  *) emit_log fail "invalid-action" "action=$ACTION" "valid=warn|reset"; exit 1 ;;
esac

today=$(date -u +%Y-%m-%d)
chain_log="$CHITIN_DIR/gov-decisions-${today}.jsonl"

if [[ ! -f "$chain_log" ]]; then
  emit_log noop "no-chain-log" "path=$chain_log"
  exit 0
fi

# ── main ──────────────────────────────────────────────────────────────────────

# Cutoff = now - WINDOW_SEC, RFC3339 UTC. The chain rows store ts in
# the same format so a string comparison is correct.
cutoff=$(date -u -d "now -${WINDOW_SEC} seconds" +%Y-%m-%dT%H:%M:%SZ)

# Aggregate lockdown events in the window, grouped by agent. jq emits
# one "agent count" line per agent that crossed the threshold.
mapfile -t breaches < <(
  jq -r --arg cutoff "$cutoff" --argjson threshold "$THRESHOLD" '
    select(.rule_id == "lockdown" and .ts >= $cutoff)
    | .agent // "(none)"
  ' "$chain_log" 2>/dev/null \
  | sort | uniq -c | awk -v t="$THRESHOLD" '$1 >= t { print $2, $1 }'
)

if [[ ${#breaches[@]} -eq 0 ]]; then
  emit_log noop "no-lockdown-bursts" "window_sec=$WINDOW_SEC" "threshold=$THRESHOLD"
  exit 0
fi

had_error=0
for breach in "${breaches[@]}"; do
  agent="${breach% *}"
  count="${breach##* }"

  action_taken="warned"
  if [[ "$ACTION" == "reset" ]]; then
    if "$KERNEL" gate reset --agent="$agent" >/dev/null 2>&1; then
      action_taken="reset"
    else
      emit_log fail "reset-failed" "agent=$agent" "count=$count"
      had_error=1
      action_taken="warned-reset-failed"
    fi
  fi

  emit_chain_event "$agent" "$count" "$WINDOW_SEC" "$action_taken"

  emit_log alert "lockdown-burst-detected" \
    "agent=$agent" \
    "count=$count" \
    "window_sec=$WINDOW_SEC" \
    "threshold=$THRESHOLD" \
    "action=$action_taken"
done

exit $had_error
