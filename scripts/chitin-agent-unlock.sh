#!/usr/bin/env bash
# chitin-agent-unlock.sh — automated lockdown age-out recovery.
#
# Selection invariant: an agent auto-resets if and only if ALL three conditions hold:
#   (A) agent_state.locked = 1 in gov.db
#   (B) locked_ts is older than CHITIN_UNLOCK_LOCK_AGE_MIN (default: 60) minutes
#   (C) No policy-violation denials for this agent in the last
#       CHITIN_UNLOCK_QUIET_MIN (default: 30) minutes in gov-decisions-*.jsonl
#
# "Policy-violation denial" = allowed=false AND rule_id NOT in the infra set:
#   envelope-closed, envelope-exhausted, envelope-not-found,
#   no_policy_found, policy_invalid, lockdown
#
# Locks with recent policy-violation denials are operator-only; this script
# leaves them untouched. Infra-class denials (budget exhaustion, missing
# envelope, policy load errors) are recoverable by design — the operator
# expects the rotator or this unlock script to handle them automatically.
#
# Why check the audit log and not only gov.db?
#   The gov.db denials table only stores non-infra denials — gate.go step 6
#   exempts all envelope-class rule_ids from the counter. So the table alone
#   cannot distinguish "locked due to policy violations" from "locked due to
#   infrastructure failures that also happen to have accumulated 10 weighted
#   denials via a different code path." The audit log (gov-decisions-*.jsonl)
#   carries the full rule_id for every decision, which is the authoritative
#   source for infra vs policy classification.
#
# On auto-reset:
#   1. chitin-kernel gate reset --agent=<id>  (clears denials + agent_state)
#   2. chitin-kernel emit writes event_type=agent_unlock_auto to its own chain
#      with payload: agent, locked_ago_min, denial class counts, rationale
#
# Runs every 15 min via chitin-agent-unlock.timer.
# Cost: one sqlite read per run (agent_state + denials) + jq over today's and
# yesterday's gov-decisions JSONL files.
#
# Exit codes:
#   0  clean run (no eligible agents, or all resets succeeded)
#   1  partial failure (stderr carries structured error lines)

set -euo pipefail

CHITIN_DIR="${CHITIN_HOME:-$HOME/.chitin}"
KERNEL="${CHITIN_KERNEL_BIN:-$HOME/.local/bin/chitin-kernel}"
LOG="${CHITIN_UNLOCK_LOG:-$HOME/.cache/chitin/agent-unlock.jsonl}"
LOCK_AGE_MIN="${CHITIN_UNLOCK_LOCK_AGE_MIN:-60}"
QUIET_WIN_MIN="${CHITIN_UNLOCK_QUIET_MIN:-30}"
GOV_DB="$CHITIN_DIR/gov.db"

# Infra rule_ids that do NOT indicate agent misbehavior. Kept as a bash array
# so it's defined once and referenced by name rather than duplicated across
# two jq filters.
INFRA_RULE_IDS=(
  "envelope-closed"
  "envelope-exhausted"
  "envelope-not-found"
  "no_policy_found"
  "policy_invalid"
  "lockdown"
)

mkdir -p "$(dirname "$LOG")"

# ── helpers ───────────────────────────────────────────────────────────────────

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

# minutes_since_ts <iso8601> — minutes elapsed since the given timestamp.
# Returns 999999 on parse failure so the caller skips the agent conservatively.
minutes_since_ts() {
  local ts="$1"
  local then_s now_s
  then_s=$(date -u -d "$ts" +%s 2>/dev/null) || { echo 999999; return; }
  now_s=$(date -u +%s)
  echo $(( (now_s - then_s) / 60 ))
}

# cutoff_ts <minutes_ago> — RFC3339 timestamp N minutes in the past.
cutoff_ts() {
  date -u -d "now -${1} minutes" +%Y-%m-%dT%H:%M:%SZ
}

# jq_infra_set — emit a jq array literal of infra rule_ids for embedding in
# filters. Builds the literal from INFRA_RULE_IDS to keep the definition in
# one place.
jq_infra_set() {
  local parts=()
  for id in "${INFRA_RULE_IDS[@]}"; do
    parts+=("\"$id\"")
  done
  printf '[%s]' "$(IFS=,; echo "${parts[*]}")"
}

# recent_denial_counts <agent> — output two integers on a single line:
#   <policy_count> <infra_count>
# Counts denied decisions for <agent> in the last QUIET_WIN_MIN minutes from
# today's and yesterday's gov-decisions JSONL files, split by infra vs policy
# rule_id.
recent_denial_counts() {
  local agent="$1"
  local cutoff infra_set policy_total infra_total n logfile day today yesterday

  cutoff=$(cutoff_ts "$QUIET_WIN_MIN")
  infra_set=$(jq_infra_set)
  policy_total=0
  infra_total=0
  today=$(date -u +%Y-%m-%d)
  yesterday=$(date -u -d "yesterday" +%Y-%m-%d 2>/dev/null) || yesterday="$today"

  for day in "$today" "$yesterday"; do
    logfile="$CHITIN_DIR/gov-decisions-${day}.jsonl"
    [[ -f "$logfile" ]] || continue

    # Policy-violation denials: denied + rule_id NOT in infra set.
    n=$(jq -c --arg agent "$agent" --arg cutoff "$cutoff" \
      --argjson infra "$infra_set" \
      'select(.agent == $agent and .allowed == false and .ts >= $cutoff) |
       .rule_id as $rid |
       select($infra | index($rid) == null)' \
      "$logfile" 2>/dev/null | wc -l) || n=0
    policy_total=$(( policy_total + n ))

    # Infra-class denials: denied + rule_id in infra set.
    n=$(jq -c --arg agent "$agent" --arg cutoff "$cutoff" \
      --argjson infra "$infra_set" \
      'select(.agent == $agent and .allowed == false and .ts >= $cutoff) |
       .rule_id as $rid |
       select($infra | index($rid) != null)' \
      "$logfile" 2>/dev/null | wc -l) || n=0
    infra_total=$(( infra_total + n ))
  done

  echo "$policy_total $infra_total"
}

# total_policy_denial_count <agent> — sum of denial counts from gov.db denials
# table. The denials table only contains non-envelope denials (gate.go step 6
# exempts envelope-class rule_ids from RecordDenial), so everything here is a
# policy-violation action by definition.
total_policy_denial_count() {
  local agent="$1"
  sqlite3 "$GOV_DB" \
    "SELECT COALESCE(SUM(count),0) FROM denials WHERE agent='$(printf '%s' "$agent" | sed "s/'/''/g")'" \
    2>/dev/null || echo 0
}

# emit_chain_event <agent> <locked_ago_min> <total_policy> <recent_policy> <recent_infra>
emit_chain_event() {
  local agent="$1" locked_ago_min="$2" total_policy="$3" recent_policy="$4" recent_infra="$5"
  local chain_id ts event_file payload

  chain_id=$(gen_uuid)
  ts=$(ts_now)
  event_file=$(mktemp /tmp/chitin-agent-unlock-XXXXXX.json)

  payload=$(jq -cn \
    --arg agent "$agent" \
    --argjson locked_ago_min "$locked_ago_min" \
    --argjson total_policy_denial_count "$total_policy" \
    --argjson recent_policy_denial_count "$recent_policy" \
    --argjson recent_infra_denial_count "$recent_infra" \
    --argjson lock_age_threshold_min "$LOCK_AGE_MIN" \
    --argjson quiet_window_min "$QUIET_WIN_MIN" \
    '{agent: $agent,
      locked_ago_min: $locked_ago_min,
      total_policy_denial_count: $total_policy_denial_count,
      recent_policy_denial_count: $recent_policy_denial_count,
      recent_infra_denial_count: $recent_infra_denial_count,
      lock_age_threshold_min: $lock_age_threshold_min,
      quiet_window_min: $quiet_window_min,
      rationale: "lock older than threshold, no recent policy violations in quiet window"}')

  jq -n \
    --arg schema_version "2" \
    --arg run_id "$chain_id" \
    --arg session_id "$chain_id" \
    --arg surface "system" \
    --arg agent_instance_id "$agent" \
    --arg event_type "agent_unlock_auto" \
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

if ! command -v sqlite3 >/dev/null 2>&1; then
  emit_log fail "sqlite3-not-found"
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  emit_log fail "jq-not-found"
  exit 1
fi

if [[ ! -f "$GOV_DB" ]]; then
  emit_log noop "no-gov-db" "path=$GOV_DB"
  exit 0
fi

# ── main ──────────────────────────────────────────────────────────────────────

# Single sqlite read for all locked agents (cost target: one read per run).
mapfile -t locked_rows < <(sqlite3 "$GOV_DB" \
  "SELECT agent || '|' || COALESCE(locked_ts,'') FROM agent_state WHERE locked=1" \
  2>/dev/null || true)

if [[ ${#locked_rows[@]} -eq 0 ]]; then
  emit_log noop "no-locked-agents"
  exit 0
fi

had_error=0
for row in "${locked_rows[@]}"; do
  agent="${row%%|*}"
  locked_ts="${row#*|}"

  if [[ -z "$locked_ts" ]]; then
    emit_log skip "no-locked-ts" "agent=$agent"
    continue
  fi

  # Condition B: lock must be old enough to be meaningful.
  locked_ago_min=$(minutes_since_ts "$locked_ts")
  if (( locked_ago_min < LOCK_AGE_MIN )); then
    emit_log skip "lock-too-fresh" \
      "agent=$agent" "locked_ago_min=$locked_ago_min" "threshold=$LOCK_AGE_MIN"
    continue
  fi

  # Condition C: classify recent denials in the quiet window.
  # If any recent denial is policy-class, the lock is legitimate — leave it.
  read -r recent_policy recent_infra < <(recent_denial_counts "$agent") || \
    { recent_policy=0; recent_infra=0; }

  if (( recent_policy > 0 )); then
    emit_log skip "recent-policy-denials" \
      "agent=$agent" "locked_ago_min=$locked_ago_min" \
      "recent_policy=$recent_policy" "recent_infra=$recent_infra"
    continue
  fi

  # All conditions met. Collect total denial count for the chain event payload.
  total_policy=$(total_policy_denial_count "$agent")

  # Reset the agent's escalation state.
  if ! "$KERNEL" gate reset --agent="$agent" >/dev/null 2>&1; then
    emit_log fail "reset-failed" "agent=$agent" "locked_ago_min=$locked_ago_min"
    had_error=1
    continue
  fi

  # Emit chain event so operators can audit recoveries.
  emit_chain_event "$agent" "$locked_ago_min" "${total_policy:-0}" "${recent_policy:-0}" "${recent_infra:-0}"

  emit_log ok "agent-unlocked-auto" \
    "agent=$agent" \
    "locked_ago_min=$locked_ago_min" \
    "total_policy_denial_count=${total_policy:-0}" \
    "recent_policy_denial_count=${recent_policy:-0}" \
    "recent_infra_denial_count=${recent_infra:-0}"
done

exit $had_error
