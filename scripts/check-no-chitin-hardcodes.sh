#!/usr/bin/env bash
# check-no-chitin-hardcodes — block repo/workspace literals from creeping
# back into swarm/bin and script entrypoints after board-config migration.
#
# Allowlist rationale:
# - scripts/install-swarm.sh seeds the canonical chitin board config on first run.
# - this script contains the forbidden literals in the pattern itself.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

PATTERN='chitinhq/chitin|/workspace/chitin'

ALLOWLIST=(
  'scripts/install-swarm.sh: seed config for the canonical chitin board'
  'scripts/check-no-chitin-hardcodes.sh: the drift guard pattern and self-doc'
)

skip_allowed() {
  local path="$1"
  case "$path" in
    scripts/install-swarm.sh|scripts/check-no-chitin-hardcodes.sh) return 0 ;;
    *) return 1 ;;
  esac
}

while IFS= read -r entry; do
  printf 'allow: %s\n' "$entry"
done <<< "$(printf '%s\n' "${ALLOWLIST[@]}")" >&2

hits=()
while IFS=: read -r path line text; do
  [ -n "$path" ] || continue
  if skip_allowed "$path"; then
    continue
  fi
  hits+=("$path:$line:$text")
done < <(rg -n "$PATTERN" swarm/bin scripts)

if ((${#hits[@]} > 0)); then
  printf 'check-no-chitin-hardcodes: found disallowed hardcodes:\n' >&2
  printf '  %s\n' "${hits[@]}" >&2
  exit 1
fi

echo "check-no-chitin-hardcodes: no disallowed repo/workspace hardcodes under swarm/bin or scripts"
