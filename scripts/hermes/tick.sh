#!/usr/bin/env bash
# Hermes staged-tick cron entrypoint.
# Spec: docs/superpowers/specs/2026-04-22-hermes-staged-tick-design.md
#
# Three isolated stages: PLAN (glm-5.1) → CODE (qwen3-coder, iff
# action=="code" and local ollama reachable) → ACT (glm-5.1).
# Each stage's model is hardcoded; no same-session delegation.
# Artifacts persist at $CHITIN_SINK_ROOT/ticks/<date>/<ts>/.
# Always exits 0 except on shell crash — stage failures are data.

set -euo pipefail

HERMES_TICK_DRY_RUN="${HERMES_TICK_DRY_RUN:-0}"
for arg in "$@"; do
  case "$arg" in
    --dry-run) HERMES_TICK_DRY_RUN=1 ;;
    -h|--help)
      echo "Usage: tick.sh [--dry-run]"
      echo "  Runs one staged-tick cycle. Artifacts at \$CHITIN_SINK_ROOT/ticks/<date>/<ts>/."
      echo "  --dry-run : Stage 3 describes tool calls without executing them (handled by prompt-act.md)."
      exit 0
      ;;
  esac
done
export HERMES_TICK_DRY_RUN

# ---- Config (env-overridable for tests) -----------------------------------
CHITIN_SINK_ROOT="${CHITIN_SINK_ROOT:-$HOME/chitin-sink}"
REPO_ROOT="${HERMES_TICK_REPO_ROOT:-$HOME/workspace/chitin}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROMPT_PLAN="$SCRIPT_DIR/prompt-plan.md"
PROMPT_CODE="$SCRIPT_DIR/prompt-code.md"
PROMPT_ACT="$SCRIPT_DIR/prompt-act.md"
SCHEMA="$SCRIPT_DIR/plan-schema.json"
MODEL_PLAN="${HERMES_TICK_MODEL_PLAN:-glm-5.1:cloud}"
MODEL_CODE="${HERMES_TICK_MODEL_CODE:-qwen3-coder:30b}"
MODEL_ACT="${HERMES_TICK_MODEL_ACT:-glm-5.1:cloud}"

# Per-stage timeouts. Stage 2 budget covers cold local-model load (~7 min
# observed for qwen3-coder:30b after 38h idle). Stage 3 budget covers
# multi-tool execution (git apply → commit → push → gh pr create).
TIMEOUT_PLAN="${HERMES_TICK_TIMEOUT_PLAN:-120}"
TIMEOUT_CODE="${HERMES_TICK_TIMEOUT_CODE:-900}"
TIMEOUT_ACT="${HERMES_TICK_TIMEOUT_ACT:-600}"

# Worktree base for in-flight detection (see code-action branch).
WORKTREE_BASE="${HERMES_TICK_WORKTREE_BASE:-$HOME/workspace}"

# Retry budget. After N consecutive stage-2 failures on the same issue,
# the issue is re-labeled `hermes-autonomous` → `hermes-gate-blocked`
# so the planner stops re-picking it every tick. Counter resets when
# stage 2 produces a valid diff.
RETRY_BUDGET="${HERMES_TICK_RETRY_BUDGET:-3}"
FAILURE_COUNTS_FILE="${HERMES_TICK_FAILURE_COUNTS_FILE:-${CHITIN_SINK_ROOT}/issue-failure-counts.json}"

# Precondition: flock + timeout are required. Without them, the guards
# and per-stage budgets below would fail under `set -e` mid-run. Exit 0
# with an explicit stderr message to preserve the script contract
# ("Always exits 0 except on shell crash") on minimal boxes.
for bin in flock timeout; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "[$(date -u +%H:%M:%SZ)] tick aborted — required binary not found: $bin" >&2
    exit 0
  fi
done

# Deterministic timestamps for tests; real runs compute fresh.
ts="${HERMES_TICK_TS:-$(date -u +%Y%m%dT%H%M%SZ)}"
date_str="${HERMES_TICK_DATE:-$(date -u +%Y-%m-%d)}"

# ---- Concurrency guard ----------------------------------------------------
# Prevent stacked ticks when a previous run is still in flight (eg. stage 2
# took longer than the cron cadence). A second cron fire exits cleanly
# rather than racing for the same queue item.
LOCK_FILE="${HERMES_TICK_LOCK_FILE:-/tmp/hermes-tick.lock}"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  echo "[$(date -u +%H:%M:%SZ)] tick skipped — lock held by another run ($LOCK_FILE)" >&2
  exit 0
fi

TICK_DIR="$CHITIN_SINK_ROOT/ticks/$date_str/$ts"
mkdir -p "$TICK_DIR"

log() { echo "[$(date -u +%H:%M:%SZ)] $*" >> "$TICK_DIR/tick.log"; }

# ---- Retry-budget helpers -------------------------------------------------
# Read-modify-write is safe because the tick-level flock serializes all
# mutations. The counts file is JSON: {"<issue_number>": <count>}.

failure_counts_init() {
  [[ -f "$FAILURE_COUNTS_FILE" ]] || echo '{}' > "$FAILURE_COUNTS_FILE"
}

failure_count_bump() {
  # Increment and print the new count for the given issue number.
  local issue="$1" count
  failure_counts_init
  count=$(jq -r --arg k "$issue" '.[$k] // 0' "$FAILURE_COUNTS_FILE")
  [[ "$count" =~ ^[0-9]+$ ]] || count=0
  count=$((count + 1))
  local tmp="$FAILURE_COUNTS_FILE.tmp"
  jq --arg k "$issue" --argjson v "$count" '.[$k] = $v' \
    "$FAILURE_COUNTS_FILE" > "$tmp"
  mv "$tmp" "$FAILURE_COUNTS_FILE"
  echo "$count"
}

failure_count_reset() {
  local issue="$1"
  failure_counts_init
  local tmp="$FAILURE_COUNTS_FILE.tmp"
  jq --arg k "$issue" 'del(.[$k])' "$FAILURE_COUNTS_FILE" > "$tmp"
  mv "$tmp" "$FAILURE_COUNTS_FILE"
}

blockade_issue() {
  # Swap the hermes-autonomous label for hermes-gate-blocked so the
  # planner stops picking this issue. Label failures are logged but do
  # not fail the tick — the issue already failed; extra noise is fine.
  local issue="$1"
  if gh issue edit "$issue" --repo chitinhq/chitin \
       --remove-label hermes-autonomous \
       --add-label hermes-gate-blocked 2>> "$TICK_DIR/tick.log"; then
    log "blockade_applied: #$issue relabeled hermes-gate-blocked"
  else
    log "blockade_failed: could not relabel #$issue (see stderr above)"
  fi
}

probe_ollama() {
  # Non-atomic read-modify-write on streak_file; safe only under cron max_parallel_jobs=1.
  local streak_file="$CHITIN_SINK_ROOT/ollama-unreachable-streak.txt"
  local current=0
  [[ -f "$streak_file" ]] && current="$(cat "$streak_file")"
  [[ "$current" =~ ^[0-9]+$ ]] || current=0

  if curl -sf --max-time 2 http://127.0.0.1:11434/api/tags >/dev/null 2>&1; then
    echo "ok" > "$TICK_DIR/ollama-probe.txt"
    echo "0" > "$streak_file"
    return 0
  fi

  echo "unreachable" > "$TICK_DIR/ollama-probe.txt"
  echo "$((current + 1))" > "$streak_file"
  return 1
}

run_stage_code() {
  log "stage 2 (code) starting"
  local plan_body file_dump files
  plan_body="$(cat "$TICK_DIR/plan.json")"
  # Read the files listed in diff_request.files (best-effort — missing
  # files become empty context strings).
  files="$(jq -r '.diff_request.files[]?' "$TICK_DIR/plan.json")"
  file_dump=""
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    if [[ -f "$REPO_ROOT/$f" ]]; then
      file_dump+=$'\n--- FILE: '"$f"$' ---\n'
      file_dump+="$(cat "$REPO_ROOT/$f")"
    fi
  done <<< "$files"

  local stage2_prompt
  stage2_prompt="$(cat "$PROMPT_CODE")

--- CONTEXT ---
plan=$plan_body

files=$file_dump"
  # Stage 2 v2: the model emits SEARCH/REPLACE blocks (see prompt-code.md).
  # Unified-diff generation by an LLM is fragile — line-number math drifts
  # across many hunks even when the edit intent is correct. Shifting to
  # string-based SEARCH/REPLACE and computing the diff in Python via
  # difflib removes that failure mode entirely.
  local rc=0
  timeout "${TIMEOUT_CODE}s" hermes chat -Q --model "$MODEL_CODE" -q "$stage2_prompt" \
      > "$TICK_DIR/sr-output.txt" 2> "$TICK_DIR/code-stderr.txt" || rc=$?
  if [[ $rc -ne 0 ]]; then
    if [[ $rc -eq 124 ]]; then
      log "stage 2 timeout (${TIMEOUT_CODE}s)"
    else
      log "stage 2 failed (rc=$rc)"
    fi
    return 1
  fi

  if [[ ! -s "$TICK_DIR/sr-output.txt" ]]; then
    log "code_empty_output: stage 2 emitted empty output"
    return 1
  fi

  # Strip leading/trailing triple-backtick fences before handing to the
  # parser. Models still wrap responses in ``` despite prompt rules.
  # Temp-file + mv pattern avoids `sed -i` which is non-portable across
  # GNU and BSD (BSD treats `-E` as the required -i backup extension).
  local tmp_sr="$TICK_DIR/sr-output.txt.tmp"
  sed -E -e '1{/^```[a-zA-Z]*[[:space:]]*$/d;}' \
         -e '${/^```[[:space:]]*$/d;}' \
         "$TICK_DIR/sr-output.txt" > "$tmp_sr"
  mv "$tmp_sr" "$TICK_DIR/sr-output.txt"

  # Convert SEARCH/REPLACE blocks → unified diff. This is deterministic:
  # blocks are applied to the REPO_ROOT file via exact-string match, and
  # the diff is computed by Python's difflib. Exits:
  #   0 = valid non-empty diff, 1 = parse/apply error, 2 = no-op.
  local sr_rc=0
  python3 "$SCRIPT_DIR/apply-search-replace.py" \
    --repo-root "$REPO_ROOT" \
    --plan "$TICK_DIR/plan.json" \
    --out-diff "$TICK_DIR/diff.patch" \
    < "$TICK_DIR/sr-output.txt" \
    2> "$TICK_DIR/sr-apply.err" || sr_rc=$?
  case "$sr_rc" in
    0) ;;
    2) log "code_no_op_edits: SEARCH/REPLACE blocks produced no effective change"
       return 1 ;;
    *) log "stage 2 search_replace_failed (rc=$sr_rc)"
       return 1 ;;
  esac

  # Belt-and-suspenders: validate the computed diff applies cleanly.
  # Should always succeed when apply-search-replace.py returned 0
  # (difflib emits valid unified diffs), but confirms the REPO_ROOT
  # hasn't drifted out from under us between generation and stage 3.
  if ! (cd "$REPO_ROOT" && git apply --check "$TICK_DIR/diff.patch") \
       2> "$TICK_DIR/diff-check.err"; then
    log "stage 2 invalid diff (git apply --check failed)"
    return 1
  fi
  log "stage 2 done"
}

run_stage_act() {
  log "stage 3 (act) starting"
  local plan_body diff_body
  plan_body="$(cat "$TICK_DIR/plan.json")"
  diff_body=""
  [[ -f "$TICK_DIR/diff.patch" ]] && diff_body="$(cat "$TICK_DIR/diff.patch")"

  local stage3_prompt
  stage3_prompt="$(cat "$PROMPT_ACT")

--- CONTEXT ---
plan=$plan_body

diff=$diff_body"
  local rc=0
  timeout "${TIMEOUT_ACT}s" hermes chat -Q --model "$MODEL_ACT" -q "$stage3_prompt" \
      > "$TICK_DIR/act-log.txt" 2> "$TICK_DIR/act-stderr.txt" || rc=$?
  if [[ $rc -ne 0 ]]; then
    if [[ $rc -eq 124 ]]; then
      log "stage 3 timeout (${TIMEOUT_ACT}s)"
    else
      log "stage 3 failed (rc=$rc)"
    fi
    return 0
  fi
  log "stage 3 done"
}

log "tick starting; dir=$TICK_DIR"

# Capture env snapshot (scrubbed).
# Narrow allowlist — avoid catching any secret-like vars by prefix accident.
: > "$TICK_DIR/env.txt"
for var in PATH CHITIN_SINK_ROOT HERMES_TICK_DRY_RUN HERMES_TICK_TS HERMES_TICK_DATE; do
  if [[ -v "$var" ]]; then
    printf '%s=%s\n' "$var" "${!var}" >> "$TICK_DIR/env.txt"
  fi
done

# ---- Queue fetch ----------------------------------------------------------
queue_labeled="$(gh issue list --repo chitinhq/chitin --label hermes-autonomous --state open --json number,title,body,createdAt,updatedAt 2>/dev/null || echo '[]')"
queue_unlabeled="$(gh issue list --repo chitinhq/chitin --search 'no:label is:open' --json number,title,body,createdAt,updatedAt 2>/dev/null || echo '[]')"
pr_inflight="$(gh pr list --repo chitinhq/chitin --search 'is:open linked:issue' --json number,title,closingIssuesReferences 2>/dev/null || echo '[]')"
jq -n \
  --argjson labeled "$queue_labeled" \
  --argjson unlabeled "$queue_unlabeled" \
  --argjson prs "$pr_inflight" \
  '{labeled: $labeled, unlabeled: $unlabeled, in_flight_prs: $prs}' \
  > "$TICK_DIR/queue.json"
log "queue captured"

# ---- STAGE 1: PLAN (glm-5.1) ----------------------------------------------
log "stage 1 (plan) starting"
stage1_prompt="$(cat "$PROMPT_PLAN")

--- CONTEXT ---
$(cat "$TICK_DIR/queue.json")"
rc=0
timeout "${TIMEOUT_PLAN}s" hermes chat -Q --model "$MODEL_PLAN" -q "$stage1_prompt" \
    > "$TICK_DIR/plan.json" 2> "$TICK_DIR/plan-stderr.txt" || rc=$?
if [[ $rc -ne 0 ]]; then
  if [[ $rc -eq 124 ]]; then
    log "stage 1 timeout (${TIMEOUT_PLAN}s)"
  else
    log "stage 1 failed (rc=$rc)"
  fi
  exit 0
fi

if ! jq -e 'type == "object" and has("action")' "$TICK_DIR/plan.json" >/dev/null 2>&1; then
  log "plan_parse_error: stage 1 output is not a plan object"
  exit 0
fi

# Validate plan.json against the formal schema when ajv is available.
# ajv is installed in CI; on the runtime box it may or may not be present.
# If missing, we fall back to the structural check above.
# NOTE: the schema is draft 2020-12; ajv-cli defaults to draft-07 so we must
# pass --spec=draft2020 explicitly (same flag used by validate-plans.sh).
if command -v ajv >/dev/null 2>&1; then
  if ! ajv validate -s "$SCHEMA" -d "$TICK_DIR/plan.json" --spec=draft2020 >/dev/null 2>&1; then
    log "plan_schema_violation: plan.json failed ajv validation against $SCHEMA"
    exit 0
  fi
fi

action="$(jq -r '.action' "$TICK_DIR/plan.json")"
log "stage 1 done; action=$action"

case "$action" in
  skip)
    log "skip: exit without invoking stages 2 or 3"
    exit 0
    ;;
  code)
    # In-flight guard: if a worktree for this issue already exists, a
    # previous tick is (or was) working it. Stage 1 doesn't see local
    # state; without this check, two ticks duplicate the same PR attempt.
    issue_num="$(jq -r '.issue_number' "$TICK_DIR/plan.json")"
    if [[ -n "$issue_num" && "$issue_num" != "0" && "$issue_num" != "null" && -d "$WORKTREE_BASE/chitin-$issue_num" ]]; then
      log "in_flight_local: worktree $WORKTREE_BASE/chitin-$issue_num exists for #$issue_num; skip"
      exit 0
    fi
    if ! probe_ollama; then
      log "ollama_unreachable: skip stages 2 and 3"
      exit 0
    fi
    if ! run_stage_code; then
      # Retry budget: track consecutive stage-2 failures per issue. Once
      # the budget is spent, re-label the issue hermes-gate-blocked so
      # the planner stops re-picking the same unsolvable item each tick.
      if [[ -n "$issue_num" && "$issue_num" != "0" && "$issue_num" != "null" ]]; then
        count="$(failure_count_bump "$issue_num")"
        log "failure_count: #$issue_num is now $count/$RETRY_BUDGET"
        if (( count >= RETRY_BUDGET )); then
          blockade_issue "$issue_num"
          failure_count_reset "$issue_num"
        fi
      fi
      exit 0
    fi
    # Stage 2 produced a valid diff — clear any prior failures for this
    # issue so a previously-flaky issue isn't held against forever.
    if [[ -n "$issue_num" && "$issue_num" != "0" && "$issue_num" != "null" ]]; then
      failure_count_reset "$issue_num"
    fi
    run_stage_act
    exit 0
    ;;
  external)
    run_stage_act
    exit 0
    ;;
  *)
    log "plan_schema_violation: unknown action=$action"
    exit 0
    ;;
esac
