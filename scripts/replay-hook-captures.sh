#!/usr/bin/env bash
# replay-hook-captures.sh — counterfactual gate replay over the
# 17-day Curie experiment capture dir.
#
# Background: ~/.chitin/hook-capture/ contains 28k+ PreToolUse
# payloads recorded between 2026-04-19 and 2026-05-06 while the
# operator-side chitin gate was bypassed. This script replays each
# capture through `chitin-kernel gate evaluate --hook-stdin --no-record`
# (no-record flag added 2026-05-06 specifically for this kind of
# audit-without-side-effects work) and produces a structured report:
#   - allow vs deny totals
#   - deny breakdown by rule
#   - action_type distribution (find normalize gaps via ActUnknown)
#   - top blocked tool/target patterns
#
# Pinned to chitin's policy via --policy-file so the question is
# "what would chitin's gate have said about this exact call?"
# (rather than "would the policy at the cwd at the time have…").
#
# Output: docs/observations/<date>-hook-capture-replay.md +
# /tmp/replay-results.jsonl (raw decision rows for further analysis).

set -uo pipefail
# NB: `set -e` intentionally OFF — find | head pipeline produces SIGPIPE
# when head closes early (SAMPLE mode), and per-iteration jq/gate calls
# can fail without aborting the whole replay. We handle errors row-by-row.

CAPTURE_DIR="${CAPTURE_DIR:-$HOME/.chitin/hook-capture}"
POLICY_FILE="${POLICY_FILE:-$HOME/workspace/chitin/chitin.yaml}"
OUT_DIR="${OUT_DIR:-/tmp}"
RESULTS="$OUT_DIR/replay-results.jsonl"
REPORT="$OUT_DIR/replay-report.md"
SAMPLE="${SAMPLE:-0}"  # if >0, sample this many captures (else all)

if [[ ! -x "$(command -v chitin-kernel)" ]]; then
  echo "replay: chitin-kernel not on PATH" >&2; exit 2
fi
if [[ ! -d "$CAPTURE_DIR" ]]; then
  echo "replay: $CAPTURE_DIR not found" >&2; exit 2
fi
if [[ ! -f "$POLICY_FILE" ]]; then
  echo "replay: $POLICY_FILE not found" >&2; exit 2
fi

mkdir -p "$OUT_DIR"

# Use a scratch CHITIN_HOME so even with --no-record any incidental
# state writes (envelope DB open, etc.) land in /tmp not prod.
SCRATCH_HOME=$(mktemp -d)
trap "rm -rf $SCRATCH_HOME" EXIT

echo "replay: capture dir = $CAPTURE_DIR"
echo "replay: policy      = $POLICY_FILE"
echo "replay: results     = $RESULTS"
echo "replay: scratch home= $SCRATCH_HOME"

> "$RESULTS"

count=0
start=$(date +%s)

# Iterate over PreToolUse captures only (the meaty ones — tool calls).
# Find produces newline-separated paths; xargs would re-quote and
# break on weird filenames, so use while-read.
find "$CAPTURE_DIR" -name "PreToolUse-*.json" -type f -print0 \
  | { if (( SAMPLE > 0 )); then head -z -n "$SAMPLE"; else cat; fi; } \
  | while IFS= read -r -d '' f; do
    count=$((count + 1))
    if (( count % 1000 == 0 )); then
      elapsed=$(( $(date +%s) - start ))
      echo "  replayed $count (${elapsed}s elapsed)" >&2
    fi
    # Pipe each capture through the gate. --no-record skips persistent
    # writes; --policy-file pins to chitin's policy; redirect stderr to
    # avoid noise from no_policy_found warnings (we don't care here
    # since policy is pinned). The hook is *expected* to exit 2 on
    # block (claudecode.ExitBlock) and 0 on allow — we trust stdout
    # in both cases. Run with `set +e` so the non-zero block exit
    # doesn't trigger the script's `set -e`.
    set +e
    decision=$(CHITIN_HOME="$SCRATCH_HOME" \
      chitin-kernel gate evaluate --hook-stdin --no-record \
        --policy-file "$POLICY_FILE" --agent=claude-code \
        < "$f" 2>/dev/null)
    set -e

    # The hook output is a `{"decision":"block",...}` JSON for denials,
    # empty stdout for allows. Convert to a uniform row joined with
    # source capture metadata so the report can group by tool.
    src_tool=$(jq -r '.tool_name // ""' "$f" 2>/dev/null)
    src_cwd=$(jq -r '.cwd // ""' "$f" 2>/dev/null | head -c 120)

    if [[ -z "$decision" ]]; then
      # Empty stdout = allow path
      jq -cn --arg t "$src_tool" --arg c "$src_cwd" \
        '{outcome: "allow", tool: $t, cwd: $c}' >> "$RESULTS"
    else
      # Block path — extract the reason
      reason=$(echo "$decision" | jq -r '.reason // ""' 2>/dev/null || echo "$decision")
      jq -cn --arg t "$src_tool" --arg c "$src_cwd" --arg r "$reason" \
        '{outcome: "block", tool: $t, cwd: $c, reason: $r}' >> "$RESULTS"
    fi
  done

elapsed=$(( $(date +%s) - start ))
total=$(wc -l < "$RESULTS")

echo "replay: complete — $total decisions in ${elapsed}s"

# Build the markdown report.
{
  echo "# Hook capture replay report"
  echo
  echo "Replayed $total PreToolUse captures through chitin's gate (--no-record)"
  echo "against \`chitin/chitin.yaml\` policy. Generated $(date -u +%Y-%m-%dT%H:%M:%SZ)."
  echo
  echo "## Allow vs deny"
  echo
  echo '```'
  jq -s 'group_by(.outcome) | map({outcome: .[0].outcome, count: length})' "$RESULTS"
  echo '```'
  echo
  echo "## Deny breakdown by reason (top 20)"
  echo
  echo '```'
  jq -s '[.[] | select(.outcome == "block")] | group_by(.reason) | map({reason: (.[0].reason | .[0:140]), count: length}) | sort_by(.count) | reverse | .[0:20]' "$RESULTS"
  echo '```'
  echo
  echo "## Tool distribution (top 20)"
  echo
  echo '```'
  jq -s 'group_by(.tool) | map({tool: .[0].tool, total: length, blocked: (map(select(.outcome == "block")) | length)}) | sort_by(.total) | reverse | .[0:20]' "$RESULTS"
  echo '```'
  echo
  echo "## Tools with highest block rate (min 10 calls)"
  echo
  echo '```'
  jq -s 'group_by(.tool) | map({tool: .[0].tool, total: length, blocked: (map(select(.outcome == "block")) | length), block_pct: ((map(select(.outcome == "block")) | length) * 100 / length)}) | map(select(.total >= 10)) | sort_by(.block_pct) | reverse | .[0:20]' "$RESULTS"
  echo '```'
} > "$REPORT"

echo "replay: report → $REPORT"
echo "replay: raw results → $RESULTS"
