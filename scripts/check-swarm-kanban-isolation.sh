#!/usr/bin/env bash
# check-swarm-kanban-isolation — reject direct swarm-side writes to kanban tables.
#
# Contract:
# - scans swarm/ only
# - excludes swarm/tests/
# - flags direct SQL writes to tasks/task_comments/task_events
# - allows scripts/kanban-flow to remain the single sanctioned write path

set -euo pipefail

ROOT="${1:-.}"
cd "$ROOT"

if ! command -v rg >/dev/null 2>&1; then
  echo "check-swarm-kanban-isolation: rg is required" >&2
  exit 2
fi

python_hits="$(rg -n -U --pcre2 \
  '(?s)\.execute\(\s*(?:[rubfRUBF]{0,4})?("""|'"'"''"'"''"'"'|"|'"'"')\s*(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(?:tasks|task_comments|task_events)\b' \
  swarm --glob '!swarm/tests/**' || true)"

shell_hits="$(rg -n --pcre2 \
  'sqlite3\b.*\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(?:tasks|task_comments|task_events)\b' \
  swarm --glob '!swarm/tests/**' || true)"

if [[ -n "$python_hits" || -n "$shell_hits" ]]; then
  echo "swarm kanban isolation violation: direct write SQL found outside scripts/kanban-flow" >&2
  [[ -n "$python_hits" ]] && printf '%s\n' "$python_hits" >&2
  [[ -n "$shell_hits" ]] && printf '%s\n' "$shell_hits" >&2
  exit 1
fi

echo "check-swarm-kanban-isolation: PASS"
