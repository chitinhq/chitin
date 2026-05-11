#!/usr/bin/env bash
# smoke-hermes-clawta-chain.sh — end-to-end smoke for the
# hermes → clawta → Lobster → leaf-CLI dispatch chain.
#
# For each of the four frontier-coder cards (claude-code, codex,
# gemini, copilot), seed a tiny test ticket on the hermes kanban
# board, run the kanban-dispatch workflow with force_driver=<card>,
# capture the chain-ledger delta, and assert:
#
#   I1. Every event in the delta names an actor (driver OR agent
#       field non-empty). Per the plan this is the "driver_identity"
#       invariant; in the ledger schema today the actor is split
#       across driver+agent flat strings (see
#       go/execution-kernel/internal/gov/decision.go), so we accept
#       either as evidence the event was attributed.
#   I2. At least one event names the spawn hop — driver=="clawta" or
#       agent=="clawta".
#   I3. For claude-code/codex/gemini: at least one event names the
#       inner hop — driver==<card> or agent==<card>. For copilot:
#       zero inner events expected (no PreToolUse surface; known
#       asymmetry documented in the plan).
#
# Cost mitigation: tickets carry a tiny body ("Print only the word
# OK and exit.") so the leaf CLI does almost no work even if the
# chain reaches spawn_worker. Failed runs may not even reach spawn
# (e.g. classifier reply isn't valid JSON), in which case I3 fails
# and the failure mode is recorded.
#
# Approval gate: Lobster has no native auto-approve. We run with
# --mode tool to get a JSON envelope with .requiresApproval.resumeToken,
# parse it, and call `lobster resume --token <X> --approve yes`. This
# matches the documented contract at
# node_modules/.pnpm/@clawdbot+lobster@2026.4.6/.../src/cli.js:440 +
# .../src/resume.js:68.
#
# Exit codes:
#   0 — all four cards smoked cleanly
#   1 — at least one card's chain failed invariant validation
#   2 — environment not ready (openclaw gateway down, missing deps, etc.)
#
# Structural backbone uses set -euo pipefail, but the per-card loop
# explicitly disables -e via `||` short-circuits so one card's failure
# never halts the rest.

set -euo pipefail

CARDS=(claude-code codex gemini copilot)
BOARD="chitin"
LEDGER_DIR="${CHITIN_LEDGER_DIR:-$HOME/.chitin}"
TODAY=$(date +%Y-%m-%d)
LEDGER_FILE="$LEDGER_DIR/gov-decisions-$TODAY.jsonl"
WORKFLOW_FILE="$HOME/.openclaw/workflows/kanban-dispatch.lobster"
TINY_BODY="Use your shell/Bash tool to run exactly this command and then stop: echo CHITIN_SMOKE_OK"

# Track seeded ticket ids so we can attempt cleanup even on early exit.
declare -a SEEDED_TICKETS=()

cleanup_seeded() {
  local id
  for id in "${SEEDED_TICKETS[@]:-}"; do
    [[ -z "$id" ]] && continue
    hermes kanban --board "$BOARD" archive "$id" >/dev/null 2>&1 || true
  done
}
trap cleanup_seeded EXIT

# ─── Preflight ───────────────────────────────────────────────────────────
preflight_fail() {
  echo "smoke: PREFLIGHT FAIL — $1" >&2
  exit 2
}

command -v jq >/dev/null 2>&1 || preflight_fail "jq not on PATH"
command -v hermes >/dev/null 2>&1 || preflight_fail "hermes not on PATH"
command -v pnpm >/dev/null 2>&1 || preflight_fail "pnpm not on PATH"
[[ -d "$LEDGER_DIR" && -w "$LEDGER_DIR" ]] || \
  preflight_fail "ledger dir not writable: $LEDGER_DIR"
[[ -f "$WORKFLOW_FILE" ]] || \
  preflight_fail "workflow file not found: $WORKFLOW_FILE"

if [[ -z "${OPENCLAW_URL:-}" ]]; then
  export OPENCLAW_URL="http://127.0.0.1:18789"
fi
if [[ -z "${OPENCLAW_TOKEN:-}" ]]; then
  if [[ ! -f "$HOME/.openclaw/openclaw.json" ]]; then
    preflight_fail "OPENCLAW_TOKEN unset and ~/.openclaw/openclaw.json missing"
  fi
  OPENCLAW_TOKEN=$(python3 -c "import json,sys; print(json.load(open('$HOME/.openclaw/openclaw.json'))['gateway']['auth']['token'])" 2>/dev/null) \
    || preflight_fail "could not extract OPENCLAW_TOKEN from openclaw.json"
  export OPENCLAW_TOKEN
fi

if ! curl -fsS "$OPENCLAW_URL/health" >/dev/null 2>&1; then
  preflight_fail "openclaw gateway not reachable at $OPENCLAW_URL"
fi

echo "smoke: preflight ok — OPENCLAW_URL=$OPENCLAW_URL, ledger=$LEDGER_FILE"

# ─── Helpers ─────────────────────────────────────────────────────────────

# count_actor_match <field-pair> <expected> <file>
# Counts lines where (.driver == expected) OR (.agent == expected).
# Both fields are strings or absent; jq handles missing fields as null.
count_actor_match() {
  local expected="$1"
  local file="$2"
  jq -c --arg e "$expected" \
    'select((.driver // "") == $e or (.agent // "") == $e)' \
    "$file" 2>/dev/null | wc -l
}

# count_unattributed <file>
# Counts events where BOTH .driver and .agent are empty/missing AND the
# event isn't a router-heuristic signal (those are summaries that legitimately
# don't carry actor identity).
count_unattributed() {
  local file="$1"
  jq -c \
    'select(((.driver // "") == "" and (.agent // "") == "")
            and ((.rule_id // "") | startswith("router-heuristic:") | not))' \
    "$file" 2>/dev/null | wc -l
}

# run_workflow <ticket_id> <card> <log_file> <env_file>
# Runs lobster in --mode tool mode, captures stdout (the JSON envelope),
# parses requiresApproval.resumeToken, and if present, resumes with
# --approve yes. Returns 0 on workflow completion (success or controlled
# fail), non-zero on hard infrastructure failure.
run_workflow() {
  local ticket_id="$1"
  local card="$2"
  local log_file="$3"

  local args_json
  args_json=$(jq -nc --arg t "$ticket_id" --arg d "$card" \
    '{ticket_id: $t, force_driver: $d}')

  # Initial run.
  local envelope
  envelope=$(pnpm exec lobster run \
    --mode tool \
    --file "$WORKFLOW_FILE" \
    --args-json "$args_json" 2>>"$log_file") || true

  # The envelope itself is one-line JSON. Tee it to the log too.
  printf '%s\n' "$envelope" >> "$log_file"

  # Quick sanity: did we get JSON at all?
  if ! printf '%s' "$envelope" | jq -e . >/dev/null 2>&1; then
    echo "    run_workflow: lobster did not emit JSON envelope" >&2
    return 1
  fi

  # If lobster halted at an approval gate, resume.
  local resume_token
  resume_token=$(printf '%s' "$envelope" \
    | jq -r '.requiresApproval.resumeToken // empty' 2>/dev/null)

  while [[ -n "$resume_token" ]]; do
    echo "    auto-approving (token=${resume_token:0:24}...)" >&2
    envelope=$(pnpm exec lobster resume \
      --token "$resume_token" --approve yes 2>>"$log_file") || true
    printf '%s\n' "$envelope" >> "$log_file"
    if ! printf '%s' "$envelope" | jq -e . >/dev/null 2>&1; then
      echo "    run_workflow: resume did not emit JSON envelope" >&2
      return 1
    fi
    resume_token=$(printf '%s' "$envelope" \
      | jq -r '.requiresApproval.resumeToken // empty' 2>/dev/null)
  done

  return 0
}

# ─── Per-card loop ───────────────────────────────────────────────────────

failures=()
passed=()

for CARD in "${CARDS[@]}"; do
  echo "──── smoke: $CARD ────"
  card_log="/tmp/smoke-$CARD.log"
  card_events="/tmp/smoke-$CARD.events.jsonl"
  : > "$card_log"
  : > "$card_events"

  # 1. Seed a ticket.
  seed_json=$(hermes kanban --board "$BOARD" create \
    "smoke $CARD $(date +%H%M%S)" \
    --body "$TINY_BODY" \
    --priority 1 \
    --json 2>>"$card_log") || {
      failures+=("$CARD: kanban create failed; see $card_log")
      continue
  }
  ticket_id=$(printf '%s' "$seed_json" | jq -r '.id // empty')
  if [[ -z "$ticket_id" ]]; then
    failures+=("$CARD: kanban create returned no id; see $card_log")
    continue
  fi
  SEEDED_TICKETS+=("$ticket_id")
  echo "  seeded ticket: $ticket_id"

  # 2. Capture ledger position.
  if [[ -f "$LEDGER_FILE" ]]; then
    ledger_before=$(wc -l < "$LEDGER_FILE")
  else
    ledger_before=0
  fi

  # 3. Run the workflow.
  echo "  running workflow..."
  if ! run_workflow "$ticket_id" "$CARD" "$card_log"; then
    failures+=("$CARD: workflow run failed; see $card_log")
    continue
  fi

  # 4. Extract the delta.
  if [[ ! -f "$LEDGER_FILE" ]]; then
    failures+=("$CARD: ledger file vanished mid-run: $LEDGER_FILE")
    continue
  fi
  ledger_after=$(wc -l < "$LEDGER_FILE")
  delta=$((ledger_after - ledger_before))

  if [[ "$delta" -lt 1 ]]; then
    failures+=("$CARD: no new chain events recorded (delta=$delta)")
    continue
  fi

  tail -n "$delta" "$LEDGER_FILE" > "$card_events"
  echo "  $delta chain event(s) recorded → $card_events"

  # 5. Assert invariants.
  card_failed=0

  # I1. Every event has actor attribution (driver or agent non-empty),
  #     excluding router-heuristic summary rows.
  unattrib=$(count_unattributed "$card_events")
  if [[ "$unattrib" -gt 0 ]]; then
    failures+=("$CARD: I1 — $unattrib event(s) unattributed (no driver+agent)")
    card_failed=1
  fi

  # I2. At least one event with actor=clawta (the spawn hop).
  clawta_n=$(count_actor_match "clawta" "$card_events")
  if [[ "$clawta_n" -lt 1 ]]; then
    failures+=("$CARD: I2 — no clawta hop event (spawn hop missing)")
    card_failed=1
  fi

  # I3. Inner-hop coverage: non-copilot cards need at least one event
  #     naming the card; copilot is the documented zero-case.
  if [[ "$CARD" != "copilot" ]]; then
    inner_n=$(count_actor_match "$CARD" "$card_events")
    if [[ "$inner_n" -lt 1 ]]; then
      failures+=("$CARD: I3 — no inner-hop event with actor=$CARD (leaf CLI hook didn't fire?)")
      card_failed=1
    fi
  fi

  if [[ "$card_failed" -eq 0 ]]; then
    passed+=("$CARD")
    echo "  ok"
  fi
done

# ─── Summary ─────────────────────────────────────────────────────────────

echo
echo "──── smoke summary ────"
echo "  passed: ${#passed[@]}/${#CARDS[@]} (${passed[*]:-none})"
if [[ ${#failures[@]} -eq 0 ]]; then
  echo "smoke: all ${#CARDS[@]} cards passed"
  exit 0
fi
echo "  failures: ${#failures[@]}"
for f in "${failures[@]}"; do
  echo "    - $f"
done
echo
echo "  per-card artifacts: /tmp/smoke-<card>.log and /tmp/smoke-<card>.events.jsonl"
exit 1
