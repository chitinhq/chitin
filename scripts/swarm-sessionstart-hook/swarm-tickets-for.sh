#!/usr/bin/env bash
# swarm-tickets-for — SessionStart hook output for a named agent.
#
# Per sw-007 Lane C (red lane): the wake-up mechanism for Claude Code
# sessions. When a new session starts, this script queries every
# kanban board for ready tickets assigned to the agent (default: red)
# and emits a markdown summary that the SessionStart hook injects
# into the conversation.
#
# Usage:
#   swarm-tickets-for.sh              # default agent: red, all boards
#   swarm-tickets-for.sh clawta       # specific agent
#   swarm-tickets-for.sh red swarm    # specific agent + specific board
#
# Output: silent when there's nothing to surface (per agent-bus inbox
# convention); otherwise markdown with ticket id, board, title.
#
# Wired by: ~/.claude/hooks/swarm-tickets.sh (operator-installed)
# Schema:   kanban.db.tasks(id, status, assignee, title)

set -uo pipefail

AGENT="${1:-red}"
BOARD_FILTER="${2:-}"
BOARDS_DIR="${HERMES_KANBAN_BOARDS_DIR:-${HOME}/.hermes/kanban/boards}"

[[ -d "$BOARDS_DIR" ]] || exit 0

# Collect: (board, id, status, title) per matching ticket
mapfile -t rows < <(
    for board_dir in "$BOARDS_DIR"/*/; do
        board="$(basename "$board_dir")"
        [[ -n "$BOARD_FILTER" && "$board" != "$BOARD_FILTER" ]] && continue
        db="$board_dir/kanban.db"
        [[ -r "$db" ]] || continue
        sqlite3 "$db" "
            SELECT '$board' || '|' || id || '|' || status || '|' || title
            FROM tasks
            WHERE assignee = '$AGENT'
              AND status IN ('ready', 'in_progress', 'blocked')
            ORDER BY priority, id;
        " 2>/dev/null
    done
)

# Silent when nothing
(( ${#rows[@]} == 0 )) && exit 0

printf '## swarm inbox — %d active ticket' "${#rows[@]}"
(( ${#rows[@]} != 1 )) && printf 's'
printf ' for `%s`\n\n' "$AGENT"

# Group by status
for status in ready in_progress blocked; do
    matches=()
    for row in "${rows[@]}"; do
        IFS='|' read -r board id rstatus title <<<"$row"
        [[ "$rstatus" == "$status" ]] && matches+=("$row")
    done
    (( ${#matches[@]} == 0 )) && continue
    printf '### %s (%d)\n\n' "$status" "${#matches[@]}"
    for row in "${matches[@]}"; do
        IFS='|' read -r board id _ title <<<"$row"
        printf -- '- **%s** `%s` — %s\n' "$board" "$id" "$title"
    done
    printf '\n'
done

printf 'Claim via `mcp__swarm-kanban__claim_ticket(board="<b>", ticket_id="<id>", owner="%s")` or `hermes kanban --board <b> show <id>`.\n' "$AGENT"
