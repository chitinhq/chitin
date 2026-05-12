#!/usr/bin/env bash
# clawta — dispatch wrapper.
#
# DISPATCH MODE: when invoked with a "dispatch [ticket] t_XXXXXX [to <agent>]"
# pattern, runs the kanban-dispatch.lobster workflow directly with auto-approve.
# This is the deterministic path: lobster announces start (kanban + Discord),
# spawns the leaf CLI, finalizes (push, PR, comment, broadcast).
#
# CHAT MODE: for any other message shape, falls through to openclaw glm-agent
# (Clawta the LLM persona) for free-form chat.
#
# Usage:
#   clawta "dispatch ticket t_XXXXX to claude-code"   # dispatch path
#   clawta "Summarize PRs from the last 24h"          # chat path
#   clawta --text "<instruction>"                     # plain-text output
#   clawta --agent <name> "<instruction>"             # override chat agent
#   clawta --help                                     # this message
#
# Talks to: OpenClaw gateway @ ws://127.0.0.1:18789 via the openclaw CLI client.
# Gateway must be running.

set -euo pipefail

OPENCLAW_BIN="/home/red/.vite-plus/bin/openclaw"
AGENT="glm-agent"
FORMAT="json"
LOBSTER_WORKFLOW="${LOBSTER_WORKFLOW:-$HOME/.openclaw/workflows/kanban-dispatch.lobster}"
LOBSTER_REPO="${LOBSTER_REPO:-$HOME/workspace/chitin}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --help|-h)
      sed -n '2,/^set -euo/p' "$0" | sed 's/^# \?//; /^set -euo/d'
      exit 0
      ;;
    --agent)
      AGENT="$2"
      shift 2
      ;;
    --text)
      FORMAT="text"
      shift
      ;;
    --json)
      FORMAT="json"
      shift
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "clawta: unknown flag: $1" >&2
      exit 2
      ;;
    *)
      break
      ;;
  esac
done

if [[ $# -eq 0 ]]; then
  echo "clawta: missing message argument. Usage: clawta \"<instruction>\"" >&2
  exit 2
fi

MESSAGE="$*"

# Dispatch-pattern detection. Captures:
#   $2 = ticket id (e.g., t_9e427360)
#   $4 = optional driver name (claude-code | codex | gemini | copilot)
if [[ "$MESSAGE" =~ [Dd]ispatch[[:space:]]+(ticket[[:space:]]+)?(t_[a-z0-9]+)([[:space:]]+(to[[:space:]]+)?([a-zA-Z0-9_-]+))? ]]; then
  TICKET_ID="${BASH_REMATCH[2]}"
  FORCE_DRIVER="${BASH_REMATCH[5]:-}"

  # Build args JSON for lobster
  if [[ -n "$FORCE_DRIVER" ]]; then
    ARGS_JSON=$(printf '{"ticket_id":"%s","force_driver":"%s"}' "$TICKET_ID" "$FORCE_DRIVER")
  else
    ARGS_JSON=$(printf '{"ticket_id":"%s"}' "$TICKET_ID")
  fi

  # Lobster needs openclaw gateway url + token to reach the gateway.
  export OPENCLAW_URL="${OPENCLAW_URL:-http://127.0.0.1:18789}"
  if [[ -z "${OPENCLAW_TOKEN:-}" ]]; then
    OPENCLAW_TOKEN=$(python3 -c "import json; print(json.load(open('$HOME/.openclaw/openclaw.json'))['gateway']['auth']['token'])" 2>/dev/null || true)
    export OPENCLAW_TOKEN
  fi

  cd "$LOBSTER_REPO"

  # First run. --mode tool returns structured JSON envelopes so we can detect
  # approval gates and auto-resume. set +e around lobster calls so a workflow
  # failure (which IS the signal we want to surface) doesn't abort the wrapper
  # under errexit — we capture and print the envelope instead.
  set +e
  RESULT=$(pnpm exec lobster run --mode tool --file "$LOBSTER_WORKFLOW" --args-json "$ARGS_JSON" 2>&1)
  RESUME_LIMIT=8  # prevent runaway resume loops if a step keeps re-requesting

  while echo "$RESULT" | jq -e '.requiresApproval.resumeToken // empty' >/dev/null 2>&1 && (( RESUME_LIMIT > 0 )); do
    TOKEN=$(echo "$RESULT" | jq -r '.requiresApproval.resumeToken // empty')
    [[ -z "$TOKEN" ]] && break
    RESULT=$(pnpm exec lobster resume --mode tool --token "$TOKEN" --approve yes 2>&1)
    RESUME_LIMIT=$((RESUME_LIMIT - 1))
  done
  set -e

  if [[ "$FORMAT" == "json" ]]; then
    echo "$RESULT"
  else
    # Best-effort plain-text extract; fall back to raw on parse failure.
    echo "$RESULT" | jq -r '.output[]? // .message // empty' 2>/dev/null || echo "$RESULT"
  fi
  exit 0
fi

# Non-dispatch message: fall through to glm-agent (Clawta LLM persona)
if [[ "$FORMAT" == "json" ]]; then
  exec "$OPENCLAW_BIN" agent --agent "$AGENT" --message "$MESSAGE" --json
else
  exec "$OPENCLAW_BIN" agent --agent "$AGENT" --message "$MESSAGE"
fi
