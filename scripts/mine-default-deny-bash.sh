#!/usr/bin/env bash
# mine-default-deny-bash.sh — second-pass analysis on the hook capture
# replay. The first pass (replay-hook-captures.sh) gave us aggregate
# counts but not the actual commands. This pass:
#   1. Walks every Bash-tool PreToolUse capture
#   2. Replays through chitin's gate (--no-record, pinned policy)
#   3. For default-deny outcomes, extracts the .tool_input.command
#   4. Deduplicates + groups by command prefix (the verb — `git`, `ls`,
#      `npm`, etc.) so we can propose allow rules at the right grain.
#
# Output: /tmp/default-deny-bash-commands.txt + a frequency report.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CAPTURE_DIR="${CAPTURE_DIR:-$HOME/.chitin/hook-capture}"
DEFAULT_POLICY_PATH="$(python3 - <<'PY' "$REPO_ROOT"
import os
import sys
from pathlib import Path

sys.path.insert(0, str(Path(sys.argv[1]) / "swarm" / "bin"))
from board_resolver import board_config, board_workspace

cfg = board_config()
value = cfg.get("chitin_yaml", "chitin.yaml")
print(Path(board_workspace()) / value)
PY
)"
POLICY_FILE="${POLICY_FILE:-$DEFAULT_POLICY_PATH}"
OUT="${OUT:-/tmp/default-deny-bash-commands.txt}"
FREQ="${FREQ:-/tmp/default-deny-bash-frequency.txt}"

SCRATCH_HOME=$(mktemp -d)
trap "rm -rf $SCRATCH_HOME" EXIT

> "$OUT"
count=0
deny_count=0

find "$CAPTURE_DIR" -name "PreToolUse-*.json" -type f -print0 \
  | while IFS= read -r -d '' f; do
    count=$((count + 1))
    if (( count % 2000 == 0 )); then
      echo "  scanned $count" >&2
    fi
    tool=$(jq -r '.tool_name // ""' "$f" 2>/dev/null)
    [[ "$tool" == "Bash" ]] || continue
    cmd=$(jq -r '.tool_input.command // ""' "$f" 2>/dev/null)
    [[ -n "$cmd" ]] || continue

    decision=$(CHITIN_HOME="$SCRATCH_HOME" \
      chitin-kernel gate evaluate --hook-stdin --no-record \
        --policy-file "$POLICY_FILE" --agent=claude-code \
        < "$f" 2>/dev/null)

    if [[ -n "$decision" ]]; then
      reason=$(echo "$decision" | jq -r '.reason // ""' 2>/dev/null)
      if [[ "$reason" == *"no matching allow rule"* ]]; then
        # Truncate to first 200 chars to keep output manageable
        echo "${cmd:0:200}" >> "$OUT"
      fi
    fi
  done

echo "scanned $(wc -l < "$OUT") default-deny Bash commands → $OUT" >&2

# Frequency report: extract the leading verb (first whitespace-
# delimited token, strip env-var-prefix forms like `FOO=bar cmd`),
# count, sort.
awk '
  {
    line = $0
    # Strip leading env assignments (VAR=val)
    while (line ~ /^[A-Za-z_][A-Za-z0-9_]*=/) {
      sub(/^[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]+[[:space:]]+/, "", line)
    }
    # Split on whitespace, take first token
    n = split(line, parts, /[[:space:]]/)
    if (n > 0) print parts[1]
  }
' "$OUT" | sort | uniq -c | sort -rn > "$FREQ"

echo ""
echo "=== top 30 verbs (by frequency) ==="
head -30 "$FREQ"
