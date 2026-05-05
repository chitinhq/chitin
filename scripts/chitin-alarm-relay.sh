#!/usr/bin/env bash
# chitin-alarm-relay.sh: Relays high-severity chitin alarms to operator's phone via hermes
# Reads ~/.cache/chitin/watchdog-state.json, dedups via ~/.cache/chitin/alarm-relay-state.json
# Requires CHITIN_ALARM_PEER env var (operator's hermes peer/channel)

set -euo pipefail

WATCHDOG_STATE="$HOME/.cache/chitin/watchdog-state.json"
RELAY_STATE="$HOME/.cache/chitin/alarm-relay-state.json"

if [[ -z "${CHITIN_ALARM_PEER:-}" ]]; then
  echo "CHITIN_ALARM_PEER env var must be set to the operator's hermes peer/channel" >&2
  exit 1
fi

# Read current signals (empty array if missing)
if [[ -f "$WATCHDOG_STATE" ]]; then
  SIGNALS=$(jq -c '.signals // []' "$WATCHDOG_STATE")
else
  SIGNALS="[]"
fi

# Read last relayed signals (empty array if missing)
if [[ -f "$RELAY_STATE" ]]; then
  LAST=$(jq -c '.' "$RELAY_STATE")
else
  LAST="[]"
fi

# If signals are empty and last was non-empty, emit recovery message
if [[ "$SIGNALS" == "[]" && "$LAST" != "[]" ]]; then
  hermes send --to "$CHITIN_ALARM_PEER" --message "All clear: no active chitin alarms."
  echo "[]" > "$RELAY_STATE"
  exit 0
fi

# If signals are non-empty and changed since last relay, send digest
if [[ "$SIGNALS" != "[]" && "$SIGNALS" != "$LAST" ]]; then
  # Compose digest message
  DIGEST=$(jq -r 'map("- " + (.summary // .type // .id // "(unknown alarm)")) | join("\n")' <<< "$SIGNALS")
  MSG="Chitin alarm(s):\n$DIGEST"
  hermes send --to "$CHITIN_ALARM_PEER" --message "$MSG"
  echo "$SIGNALS" > "$RELAY_STATE"
  exit 0
fi

# No change: do nothing
exit 0
