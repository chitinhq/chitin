#!/usr/bin/env bash
# install-board-watchdog-prompt.sh — idempotent installer for the board-watchdog prompt
# Copies swarm/prompts/board-watchdog.md to the hermes cron jobs.json prompt field.
# Usage: ./install-board-watchdog-prompt.sh [--verify]
#   --verify: check that the installed prompt matches the tracked file; exit 0 if match, 1 if drift

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
PROMPT_FILE="$REPO_ROOT/swarm/prompts/board-watchdog.md"
CRON_JOBS_FILE="$HOME/.hermes/cron/jobs.json"
WATCHDOG_JOB_ID="388e38b20bd5"

if [[ ! -f "$PROMPT_FILE" ]]; then
    echo "ERROR: Tracked prompt file not found at $PROMPT_FILE" >&2
    exit 1
fi

TRACKED_PROMPT="$(cat "$PROMPT_FILE")"

if [[ "${1:-}" == "--verify" ]]; then
    # Verify mode: check that the installed prompt matches the tracked file
    if [[ ! -f "$CRON_JOBS_FILE" ]]; then
        echo "ERROR: Cron jobs file not found at $CRON_JOBS_FILE" >&2
        exit 1
    fi

    # Extract the prompt field from the watchdog job using python
    INSTALLED_PROMPT="$(python3 -c "
import json, sys
with open('$CRON_JOBS_FILE') as f:
    cron = json.load(f)
jobs = cron.get('jobs', cron) if isinstance(cron, dict) else cron
for job in jobs:
    if isinstance(job, dict) and job.get('id') == '$WATCHDOG_JOB_ID':
        print(job.get('prompt', ''))
        break
else:
    print('NOT_FOUND', file=sys.stderr)
    sys.exit(1)
")"

    if [[ $? -ne 0 ]]; then
        echo "FAIL: Could not find watchdog job $WATCHDOG_JOB_ID in cron jobs" >&2
        exit 1
    fi

    # Compare (strip trailing whitespace for robustness)
    TRACKED_NORMALIZED="$(echo "$TRACKED_PROMPT" | sed 's/[[:space:]]*$//' )"
    INSTALLED_NORMALIZED="$(echo "$INSTALLED_PROMPT" | sed 's/[[:space:]]*$//')"

    if [[ "$TRACKED_NORMALIZED" == "$INSTALLED_NORMALIZED" ]]; then
        echo "OK: Board-watchdog prompt matches tracked file"
        exit 0
    else
        echo "DRIFT: Board-watchdog prompt differs from tracked file" >&2
        echo "  Tracked file: $PROMPT_FILE" >&2
        echo "  Cron jobs:    $CRON_JOBS_FILE" >&2
        echo "  Job ID:       $WATCHDOG_JOB_ID" >&2
        diff <(echo "$TRACKED_NORMALIZED") <(echo "$INSTALLED_NORMALIZED") >&2 || true
        exit 1
    fi
else
    # Install mode: update the cron job prompt from the tracked file
    if [[ ! -f "$CRON_JOBS_FILE" ]]; then
        echo "ERROR: Cron jobs file not found at $CRON_JOBS_FILE" >&2
        exit 1
    fi

    python3 -c "
import json

with open('$CRON_JOBS_FILE') as f:
    cron = json.load(f)

jobs = cron.get('jobs', cron) if isinstance(cron, dict) else cron

updated = False
for job in jobs:
    if isinstance(job, dict) and job.get('id') == '$WATCHDOG_JOB_ID':
        job['prompt'] = '''$TRACKED_PROMPT'''
        updated = True
        break

if not updated:
    print('ERROR: Watchdog job $WATCHDOG_JOB_ID not found', file=__import__('sys').stderr)
    __import__('sys').exit(1)

with open('$CRON_JOBS_FILE', 'w') as f:
    json.dump(cron, f, indent=2)

print('OK: Board-watchdog prompt updated from tracked file')
"

    # Make the script itself executable
    chmod +x "$SCRIPT_DIR/install-board-watchdog-prompt.sh"
fi