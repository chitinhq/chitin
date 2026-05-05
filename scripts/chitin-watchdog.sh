#!/usr/bin/env bash
# chitin-watchdog.sh — desktop-notification surface for chain-driven signals.
#
# Invariant: when any of (locked agents | failed chitin-* units | latest
# rollup alarms) is non-empty AND the signal-set hash differs from the
# last-notified state, emit a notify-send digest. Identical state across
# ticks → silence (suppression).
#
# Three signals monitored:
#   1. Locked agents          — gov.db agent_state.locked = 1
#   2. Failed chitin-* units  — systemctl --user list-units --state=failed
#   3. Rollup alarms          — latest swarm-rollups/*.json -> .alarms[]
#
# State persisted at ~/.cache/chitin/watchdog-state.json (last-notified hash).
# Designed for the operator AT the box (notify-send via DBus). For off-box
# routing, extend notify_user() with a slack/webhook sink — callers don't
# change.
#
# Why this exists: 2026-05-04 incident — copilot-cli sat in lockdown for
# 20.5h. The chain knew, gov.db knew, the rollup detected the resulting
# LOW SUCCESS / HIGH SHORT-RUN alarms. Nothing reached the operator until
# they happened to ask. This closes the visibility gap.
#
# Cost: 1 sqlite read + 1 systemctl + 1 jq over ~2KB JSON. <1s per tick.
# Cadence: 15 min via chitin-watchdog.timer.

set -euo pipefail

CHITIN_DIR="${CHITIN_HOME:-$HOME/.chitin}"
GOV_DB="$CHITIN_DIR/gov.db"
ROLLUPS_DIR="${CHITIN_ROLLUPS_DIR:-$HOME/.cache/chitin/swarm-rollups}"
STATE_FILE="${CHITIN_WATCHDOG_STATE:-$HOME/.cache/chitin/watchdog-state.json}"
LOG="${CHITIN_WATCHDOG_LOG:-$HOME/.cache/chitin/watchdog.jsonl}"

mkdir -p "$(dirname "$STATE_FILE")" "$(dirname "$LOG")"

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

# notify_user — single sink for operator-facing alerts. Today: notify-send.
# Extend by adding additional channels (slack post, email) without touching
# callers. Errors are non-fatal: a missing notify-send is logged once and
# the watchdog continues to write its state file so the suppression
# machinery still works.
notify_user() {
  local title="$1" body="$2" urgency="${3:-critical}"
  if command -v notify-send >/dev/null 2>&1; then
    notify-send "$title" "$body" -u "$urgency" -i dialog-warning -t 0 \
      >/dev/null 2>&1 || emit_log warn "notify-send-failed" "title=$title"
  else
    emit_log warn "notify-send-not-found" "title=$title"
  fi
}

# ── signals ──────────────────────────────────────────────────────────────

# locked_agents — emits "<count>|<csv-of-agent-ids>". "0|" when none.
# Schema reference: go/execution-kernel/internal/gov/escalation.go.
# Column is `agent` (not `agent_id`); the v1 schema uses TEXT primary key.
locked_agents() {
  if [[ ! -f "$GOV_DB" ]]; then echo "0|"; return; fi
  if ! command -v sqlite3 >/dev/null 2>&1; then echo "0|"; return; fi
  local rows
  rows=$(sqlite3 "$GOV_DB" \
    "SELECT agent FROM agent_state WHERE locked=1 ORDER BY agent;" \
    2>/dev/null || echo "")
  if [[ -z "$rows" ]]; then echo "0|"; return; fi
  local count csv
  count=$(printf '%s\n' "$rows" | wc -l | tr -d ' ')
  csv=$(printf '%s\n' "$rows" | tr '\n' ',' | sed 's/,$//')
  printf '%s|%s\n' "$count" "$csv"
}

# failed_units — chitin-* user services in failed state.
failed_units() {
  local out
  out=$(systemctl --user list-units --type=service --state=failed --no-legend --plain 2>/dev/null \
        | awk '/^chitin-/ {print $1}' \
        | sort)
  if [[ -z "$out" ]]; then echo "0|"; return; fi
  local count csv
  count=$(printf '%s\n' "$out" | wc -l | tr -d ' ')
  csv=$(printf '%s\n' "$out" | tr '\n' ',' | sed 's/,$//')
  printf '%s|%s\n' "$count" "$csv"
}

# rollup_alarms — alarm count from latest rollup snapshot.
rollup_alarms() {
  local latest
  latest=$(ls -t "$ROLLUPS_DIR"/*.json 2>/dev/null | head -1)
  if [[ -z "$latest" ]]; then echo "0|"; return; fi
  if ! command -v jq >/dev/null 2>&1; then echo "0|"; return; fi
  local count
  count=$(jq '.alarms // [] | length' "$latest" 2>/dev/null || echo 0)
  printf '%s|\n' "$count"
}

# ── compose + notify ─────────────────────────────────────────────────────

la=$(locked_agents); fu=$(failed_units); ra=$(rollup_alarms)
la_count=${la%%|*}; la_list=${la#*|}
fu_count=${fu%%|*}; fu_list=${fu#*|}
ra_count=${ra%%|*}

total=$((la_count + fu_count + ra_count))
# State hash combines all signal counts AND the listed agent/unit names so
# a same-count-different-set transition (e.g. one agent unlocked, another
# locked) re-notifies rather than getting suppressed.
state_hash="la=${la_count}:${la_list}|fu=${fu_count}:${fu_list}|ra=${ra_count}"

prior_hash=""
if [[ -f "$STATE_FILE" ]]; then
  prior_hash=$(jq -r '.hash // ""' "$STATE_FILE" 2>/dev/null || echo "")
fi

# Persist current hash up front. Don't lose the watermark on a notify-send
# failure — that would re-fire the alert on every tick.
printf '{"ts":"%s","hash":"%s","total":%d}\n' "$(ts_now)" "$state_hash" "$total" > "$STATE_FILE"

if (( total == 0 )); then
  # Recovery transition: was alerting on prior tick, now clean.
  if [[ -n "$prior_hash" && "$prior_hash" != "$state_hash" ]]; then
    notify_user "chitin watchdog: all clear" \
      "no locked agents, no failed units, no alarms" low
    emit_log ok "all-clear" "prior_hash=$prior_hash"
  else
    emit_log noop "no-signals"
  fi
  exit 0
fi

if [[ "$prior_hash" == "$state_hash" ]]; then
  emit_log noop "same-state-as-prior" "total=$total"
  exit 0
fi

body=""
(( la_count > 0 )) && body+="🔒 ${la_count} locked: ${la_list}"$'\n'
(( fu_count > 0 )) && body+="❌ ${fu_count} failed: ${fu_list}"$'\n'
(( ra_count > 0 )) && body+="🚨 ${ra_count} alarm(s) in latest rollup"$'\n'

notify_user "chitin watchdog: ${total} signal(s)" "$body" critical
emit_log alert "${total}-signals" \
  "la=${la_count}" "fu=${fu_count}" "ra=${ra_count}"
