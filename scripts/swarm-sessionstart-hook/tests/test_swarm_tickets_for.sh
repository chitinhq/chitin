#!/usr/bin/env bash
# Test harness for swarm-tickets-for.sh — verifies the SessionStart hook
# output across the empty/single/multi/multi-board cases.
#
# Run: ./tests/test_swarm_tickets_for.sh
# Exit: 0 if all assertions pass, 1 on first failure.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOOK="$SCRIPT_DIR/swarm-tickets-for.sh"
[[ -x "$HOOK" ]] || chmod +x "$HOOK"

TMPDIRS=()
trap 'for d in "${TMPDIRS[@]}"; do rm -rf "$d"; done' EXIT

fresh_root() {
    local root
    root="$(mktemp -d)"
    TMPDIRS+=("$root")
    echo "$root"
}

mk_board() {
    local root="$1"
    local board="$2"
    local db="$root/$board/kanban.db"
    mkdir -p "$(dirname "$db")"
    sqlite3 "$db" "
        CREATE TABLE tasks (id TEXT PRIMARY KEY, title TEXT, status TEXT,
                            assignee TEXT, priority INTEGER);
    "
    echo "$db"
}

assert_eq() {
    local got="$1" expected="$2" label="$3"
    if [[ "$got" != "$expected" ]]; then
        echo "FAIL [$label]"
        echo "  got:      $got"
        echo "  expected: $expected"
        exit 1
    fi
    echo "OK   [$label]"
}

assert_contains() {
    local haystack="$1" needle="$2" label="$3"
    if [[ "$haystack" != *"$needle"* ]]; then
        echo "FAIL [$label]"
        echo "  needle:   $needle"
        echo "  haystack: $haystack"
        exit 1
    fi
    echo "OK   [$label]"
}

# --- Test 1: silent when no tickets ---
root=$(fresh_root); mk_board "$root" "empty" >/dev/null
out=$(HERMES_KANBAN_BOARDS_DIR="$root" "$HOOK" red 2>&1)
assert_eq "$out" "" "silent when no tickets"

# --- Test 2: surfaces a single ready ticket assigned to the agent ---
root=$(fresh_root); db=$(mk_board "$root" "test2")
sqlite3 "$db" "INSERT INTO tasks VALUES('t_alpha','first ticket','ready','red',1);"
out=$(HERMES_KANBAN_BOARDS_DIR="$root" "$HOOK" red 2>&1)
assert_contains "$out" "swarm inbox — 1 active ticket for \`red\`" "header for single ticket"
assert_contains "$out" "t_alpha" "ticket id surfaced"
assert_contains "$out" "first ticket" "ticket title surfaced"

# --- Test 3: groups by status when multiple ---
root=$(fresh_root); db=$(mk_board "$root" "test3")
sqlite3 "$db" "
    INSERT INTO tasks VALUES('t_one','ready one','ready','red',1);
    INSERT INTO tasks VALUES('t_two','in progress','in_progress','red',1);
    INSERT INTO tasks VALUES('t_three','blocked','blocked','red',1);
    INSERT INTO tasks VALUES('t_four','not mine','ready','clawta',1);
"
out=$(HERMES_KANBAN_BOARDS_DIR="$root" "$HOOK" red 2>&1)
assert_contains "$out" "swarm inbox — 3 active tickets for \`red\`" "count excludes other-assignee"
assert_contains "$out" "### ready (1)" "ready group"
assert_contains "$out" "### in_progress (1)" "in_progress group"
assert_contains "$out" "### blocked (1)" "blocked group"
assert_contains "$out" "t_one" "ready ticket surfaced"
assert_contains "$out" "t_two" "in_progress ticket surfaced"
assert_contains "$out" "t_three" "blocked ticket surfaced"

# --- Test 4: filters to specific board ---
root=$(fresh_root)
db1=$(mk_board "$root" "test4a")
db2=$(mk_board "$root" "test4b")
sqlite3 "$db1" "INSERT INTO tasks VALUES('t_from_a','a-ticket','ready','red',1);"
sqlite3 "$db2" "INSERT INTO tasks VALUES('t_from_b','b-ticket','ready','red',1);"
out=$(HERMES_KANBAN_BOARDS_DIR="$root" "$HOOK" red test4a 2>&1)
assert_contains "$out" "t_from_a" "board-filtered: a-ticket present"
[[ "$out" != *"t_from_b"* ]] && echo "OK   [board-filtered: b-ticket excluded]" || \
    { echo "FAIL [board-filtered: b-ticket should be excluded]"; exit 1; }

# --- Test 5: silent when agent has no matches but board has tickets ---
root=$(fresh_root); db=$(mk_board "$root" "test5")
sqlite3 "$db" "INSERT INTO tasks VALUES('t_other','not red','ready','clawta',1);"
out=$(HERMES_KANBAN_BOARDS_DIR="$root" "$HOOK" red 2>&1)
assert_eq "$out" "" "silent when agent has no matches"

echo
echo "all assertions passed."
