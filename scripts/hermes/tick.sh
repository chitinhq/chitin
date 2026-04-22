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

# Deterministic timestamps for tests; real runs compute fresh.
ts="${HERMES_TICK_TS:-$(date -u +%Y%m%dT%H%M%SZ)}"
date_str="${HERMES_TICK_DATE:-$(date -u +%Y-%m-%d)}"

TICK_DIR="$CHITIN_SINK_ROOT/ticks/$date_str/$ts"
mkdir -p "$TICK_DIR"

log() { echo "[$(date -u +%H:%M:%SZ)] $*" >> "$TICK_DIR/tick.log"; }

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

  if ! hermes chat --model "$MODEL_CODE" --system "$PROMPT_CODE" \
         --context "plan=$plan_body files=$file_dump" \
         > "$TICK_DIR/diff.patch" 2> "$TICK_DIR/code-stderr.txt"; then
    log "stage 2 failed (hermes non-zero)"
    return 1
  fi

  if [[ ! -s "$TICK_DIR/diff.patch" ]]; then
    log "code_empty_output: stage 2 emitted empty diff"
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

  if ! hermes chat --model "$MODEL_ACT" --system "$PROMPT_ACT" \
         --context "plan=$plan_body diff=$diff_body" \
         > "$TICK_DIR/act-log.txt" 2> "$TICK_DIR/act-stderr.txt"; then
    log "stage 3 failed (hermes non-zero)"
    return 0
  fi
  log "stage 3 done"
}

log "tick starting; dir=$TICK_DIR"

# Capture env snapshot (scrubbed).
env | grep -E '^(CHITIN|HERMES|OLLAMA|PATH)=' > "$TICK_DIR/env.txt" || true

# ---- Queue fetch ----------------------------------------------------------
queue_labeled="$(gh issue list --repo chitinhq/chitin --label hermes-autonomous --state open --json number,title,body 2>/dev/null || echo '[]')"
queue_unlabeled="$(gh issue list --repo chitinhq/chitin --search 'no:label is:open' --json number,title,body 2>/dev/null || echo '[]')"
pr_inflight="$(gh pr list --repo chitinhq/chitin --search 'is:open linked:issue' --json number,title 2>/dev/null || echo '[]')"
jq -n \
  --argjson labeled "$queue_labeled" \
  --argjson unlabeled "$queue_unlabeled" \
  --argjson prs "$pr_inflight" \
  '{labeled: $labeled, unlabeled: $unlabeled, in_flight_prs: $prs}' \
  > "$TICK_DIR/queue.json"
log "queue captured"

# ---- STAGE 1: PLAN (glm-5.1) ----------------------------------------------
log "stage 1 (plan) starting"
if ! hermes chat --model "$MODEL_PLAN" --system "$PROMPT_PLAN" \
       --context "$(cat "$TICK_DIR/queue.json")" \
       > "$TICK_DIR/plan.json" 2> "$TICK_DIR/plan-stderr.txt"; then
  log "stage 1 failed (hermes non-zero)"
  exit 0
fi

if ! jq -e 'type == "object" and has("action")' "$TICK_DIR/plan.json" >/dev/null 2>&1; then
  log "plan_parse_error: stage 1 output is not a plan object"
  exit 0
fi

action="$(jq -r '.action' "$TICK_DIR/plan.json")"
log "stage 1 done; action=$action"

case "$action" in
  skip)
    log "skip: exit without invoking stages 2 or 3"
    exit 0
    ;;
  code)
    if ! probe_ollama; then
      log "ollama_unreachable: skip stages 2 and 3"
      exit 0
    fi
    if ! run_stage_code; then
      exit 0
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
