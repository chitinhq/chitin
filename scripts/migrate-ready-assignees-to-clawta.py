#!/usr/bin/env python3
"""
Backfill migration: reassign all ready-status tickets with non-terminal assignees to clawta.

Idempotent — running twice produces the same state as running once.

Terminal lanes: codex, copilot, claude-code, gemini, clawta
Non-terminal: NULL, empty string, or any other value (e.g., 'red')

Only touches tickets in 'ready' status; skips triage/in_progress/done/blocked/archived.
"""

import os
import sqlite3
import sys
from datetime import datetime
from pathlib import Path

TERMINAL_LANES = {"codex", "copilot", "claude-code", "gemini", "clawta"}

# board_resolver lives under swarm/bin/, which is not on sys.path when this
# script runs from the repo root. Add it explicitly, relative to this file.
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "swarm" / "bin"))
from board_resolver import resolve_db

DB = str(resolve_db())


def is_terminal_lane(assignee):
    """Check if assignee is a terminal lane."""
    return assignee in TERMINAL_LANES


def main():
    if not os.path.exists(DB):
        print(f"ERROR: Database not found: {DB}", file=sys.stderr)
        sys.exit(1)

    conn = sqlite3.connect(DB)
    cursor = conn.cursor()

    # Find all ready tickets with non-terminal assignees
    cursor.execute(
        """
        SELECT id, assignee FROM tasks
        WHERE status = 'ready'
        AND (assignee IS NULL OR assignee = '' OR assignee NOT IN (?, ?, ?, ?, ?))
        ORDER BY id
        """,
        tuple(TERMINAL_LANES),
    )

    rows = cursor.fetchall()

    if not rows:
        print("✓ No tickets to migrate (all ready tickets have terminal assignees)")
        conn.close()
        return 0

    print(f"Found {len(rows)} ticket(s) to migrate:")
    for ticket_id, old_assignee in rows:
        print(f"  {ticket_id}: {old_assignee or '(none)'} → clawta")

    # Perform migration
    updated_count = 0
    for ticket_id, _ in rows:
        cursor.execute("UPDATE tasks SET assignee = 'clawta' WHERE id = ?", (ticket_id,))
        updated_count += 1

    conn.commit()

    print(f"\n✓ Migrated {updated_count} ticket(s)")

    # Verify idempotence: running again should find zero rows to update
    cursor.execute(
        """
        SELECT COUNT(*) FROM tasks
        WHERE status = 'ready'
        AND (assignee IS NULL OR assignee = '' OR assignee NOT IN (?, ?, ?, ?, ?))
        """,
        tuple(TERMINAL_LANES),
    )
    remaining = cursor.fetchone()[0]

    if remaining == 0:
        print("✓ Idempotence check passed: re-running would update 0 rows")
    else:
        print(
            f"WARNING: Idempotence check failed: {remaining} row(s) still need migration",
            file=sys.stderr,
        )
        conn.close()
        return 1

    conn.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
