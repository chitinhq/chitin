#!/usr/bin/env bash
# peer-copilot-chat.sh — non-interactive Copilot peer invocation for
# the kernel-gate-escalation peer-spawn path.
#
# History:
#   v1 (2026-05-06 morning): tried `gh copilot suggest -t shell` —
#       interactive, exits 1 in headless context. Fail-open in the
#       kernel masked the failure but no useful peer output.
#   v2 (2026-05-06 noon): tried `curl ... api.githubcopilot.com/chat/
#       completions` directly. Returns "Access to this endpoint is
#       forbidden" for non-Copilot-CLI clients on most prompts.
#   v3 (2026-05-06 PM, current): use `copilot -p <prompt>` — the
#       supported headless mode of the Copilot CLI. Costs 0 Premium
#       Interactions when --model gpt-4.1 is passed (x0 multiplier
#       for paid Copilot plans).
#
# Usage:
#   echo "prompt text" | scripts/peer-copilot-chat.sh <model>
#
# Stdout: the model's response content (with the
#   "Changes / Requests / Tokens" trailer stripped).
# Exit codes:
#   0  success
#   1  bad usage / missing model arg
#   3  copilot CLI unreachable / non-zero exit

set -uo pipefail

if [[ $# -lt 1 || -z "$1" ]]; then
  echo "usage: $0 <model>" >&2
  exit 1
fi
MODEL="$1"

PROMPT=$(cat)
if [[ -z "$PROMPT" ]]; then
  echo "no prompt on stdin" >&2
  exit 1
fi

# `copilot -p <prompt>` runs the Copilot CLI headless. --allow-all-tools
# lets it complete its work without prompting (the kernel's gate is
# the actual policy boundary anyway). --no-banner suppresses the
# startup banner. Output goes to stdout; the trailing 4-line footer
# ("Changes ... / Requests ... / Tokens ...") is stripped before
# returning to the worker.
RAW=$(copilot -p "$PROMPT" --model "$MODEL" --allow-all-tools 2>&1)
RC=$?
if [[ $RC -ne 0 ]]; then
  echo "copilot -p exit=$RC" >&2
  echo "$RAW" >&2
  exit 3
fi

# Strip the trailer: 4 lines starting with the blank line that
# precedes "Changes ", down to the "Tokens ..." line at the very end.
# awk is robust to varying token-count formatting; sed would be
# fragile to the multi-space separators copilot uses.
echo "$RAW" | awk '
  /^Tokens[[:space:]]/ { in_footer=1; next }
  /^Requests[[:space:]]/ { in_footer=1; next }
  /^Changes[[:space:]]/ { in_footer=1; next }
  in_footer { next }
  { print }
' | sed -e :a -e '/^[[:space:]]*$/N;/\n[[:space:]]*$/ba' -e 's/[[:space:]]*$//'
