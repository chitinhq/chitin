#!/usr/bin/env bash
# install-analyzer-cron.sh — register the analyzer workflow with OpenClaw cron.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TARGET_DIR="$HOME/.openclaw/workflows"
mkdir -p "$TARGET_DIR"
ln -sfn "$REPO_ROOT/swarm/workflows/analyzer-cron.lobster" "$TARGET_DIR/analyzer-cron.lobster"
echo "linked: $TARGET_DIR/analyzer-cron.lobster → $REPO_ROOT/swarm/workflows/analyzer-cron.lobster"

if ! command -v openclaw >/dev/null 2>&1; then
  echo "openclaw: CLI not on PATH; workflow linked but cron not registered." >&2
  exit 0
fi

if openclaw cron list 2>/dev/null | grep -q "^[a-f0-9-]\\+ chitin-analyzer-daily "; then
  echo "openclaw cron: job 'chitin-analyzer-daily' already registered."
  exit 0
fi

openclaw cron add \
  --name "chitin-analyzer-daily" \
  --description "Daily chitin analyzer pass over recent sessions; writes ~/.chitin/analyzer.db suggestions." \
  --every "24h" \
  --agent glm-agent \
  --session isolated \
  --light-context \
  --tools exec \
  --timeout-seconds 300 \
  --no-deliver \
  --message "Run exactly one command, no commentary:

cd \"$REPO_ROOT\" && pnpm exec lobster run --file ~/.openclaw/workflows/analyzer-cron.lobster --args-json '{\"window\":\"24h\"}'

After the exec tool returns, reply with exactly the word 'ok' and nothing else." \
  >/dev/null

echo "openclaw cron: 'chitin-analyzer-daily' registered (24h)."
echo "  inspect: openclaw cron show chitin-analyzer-daily"
echo "  runs:    openclaw cron runs --name chitin-analyzer-daily"
