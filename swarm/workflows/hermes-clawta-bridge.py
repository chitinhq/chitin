#!/usr/bin/env python3
"""Hermes ↔ Clawta bridge for local operator cron.

Source lives in the Chitin repo at `swarm/workflows/hermes-clawta-bridge.py`.
Install with `swarm/bin/install-hermes-clawta-bridge.sh`, which symlinks this
file to `~/.hermes/scripts/hermes-clawta-bridge.py` for the existing cron job.
"""
import json
import os
import sqlite3
import subprocess
import re
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "bin"))
from board_resolver import spec_dir_for_board

BOARD = os.environ.get("KANBAN_BOARD", "chitin")
KANBAN_DB = Path.home() / f".hermes/kanban/boards/{BOARD}/kanban.db"
KANBAN_FLOW = "kanban-flow"  # Use from PATH, not stale worktree path
DRY_RUN = "--dry-run" in sys.argv

# Priority threshold: P50+ = Hermes lane, below = Clawta lane
HERMES_PRIORITY_THRESHOLD = 50

# Assignees that represent operator decisions — never auto-unblock these
OPERATOR_ASSIGNEES = {"red", "jp"}

# Block reasons that indicate operator/governance decision — never auto-unblock
OPERATOR_BLOCK_REASONS = {"watchdog", "spec", "invariants", "no_spec", "needs_spec", "loop_detected"}

# Failure classes (Clawta contract point 4)
FAILURE_CLASSES = [
    "explicit_failure",      # Worker exited with error code
    "silent_death",          # Worker process vanished without output
    "retry_exhausted",       # Max retries hit, still failing
    "no_pr",                 # Worker completed but no PR opened
    "pr_rejected",           # PR opened but CI/review failed
    "ci_fail",               # CI checks red
    "rebase_conflict",       # Merge conflict with main
    "deploy_drift",          # Branch diverged from expected state
]


def get_db():
    conn = sqlite3.connect(str(KANBAN_DB))
    conn.row_factory = sqlite3.Row
    return conn


def run_cmd(args, **kwargs):
    """Run a command, return stdout."""
    result = subprocess.run(args, capture_output=True, text=True, timeout=30, **kwargs)
    return result.stdout.strip(), result.returncode


def emit_telemetry(stats):
    """Emit a summary of what the bridge did this run."""
    summary = (
        f"📊 Bridge telemetry ({BOARD}):\n"
        f"  Claimed for Hermes: {stats['claimed_for_hermes']}\n"
        f"  Left for Clawta: {stats['left_for_clawta']}\n"
        f"  Escalated from Clawta failure: {stats['escalated_from_clawta']}\n"
        f"  Skipped (already active/claimed): {stats['skipped']}\n"
        f"  Auto-unblocked (PR exists/dep cleared): {stats['auto_unblocked']}\n"
        f"  Skipped (operator-blocked): {stats['operator_blocked']}"
    )
    print(summary)
    if not DRY_RUN:
        run_cmd([
            "openclaw", "message", "send",
            "--channel", "discord", "--account", "default",
            "--text", summary,
        ])


def classify_failure(comments: str) -> str:
    """Classify why a worker failed based on ticket comments."""
    if "explicit" in comments.lower() and "fail" in comments.lower():
        return "explicit_failure"
    if "silent" in comments.lower() and ("death" in comments.lower() or "worker" in comments.lower()):
        return "silent_death"
    if "retry" in comments.lower() and "exhaust" in comments.lower():
        return "retry_exhausted"
    if "gh pr create failed" in comments:
        return "no_pr"
    if "ci" in comments.lower() and "fail" in comments.lower():
        return "ci_fail"
    if "rebase" in comments.lower() or "merge conflict" in comments.lower():
        return "rebase_conflict"
    if "drift" in comments.lower() or "diverged" in comments.lower():
        return "deploy_drift"
    if "pr" in comments.lower() and "reject" in comments.lower():
        return "pr_rejected"
    return "unknown"


def build_failure_packet(ticket_id: str, title: str, priority: int, comments: str) -> dict:
    """Build a structured failure escalation packet (Clawta contract point 4)."""
    # Extract worker/model from comments
    worker = "unknown"
    model = "unknown"
    import re
    worker_match = re.search(r'Rout(?:ed|ing):?\s+(\w+)/(?:([^\s,]+))', comments)
    if worker_match:
        worker = worker_match.group(1)
        model = worker_match.group(2)

    return {
        "ticket_id": ticket_id,
        "title": title[:80],
        "priority": priority,
        "worker": worker,
        "model": model,
        "failure_class": classify_failure(comments),
        "recommended_action": _recommend_action(classify_failure(comments)),
    }


def _recommend_action(failure_class: str) -> str:
    """Recommend next action based on failure class."""
    return {
        "explicit_failure": "re-dispatch with different driver",
        "silent_death": "retry (auto-retried by watchdog), escalate if 3+ attempts",
        "retry_exhausted": "escalate to hermes for diagnosis",
        "no_pr": "auto-open PR from branch",
        "pr_rejected": "re-dispatch with fix instructions",
        "ci_fail": "auto-retry once, then escalate",
        "rebase_conflict": "rebase and re-dispatch",
        "deploy_drift": "re-dispatch from clean branch",
        "unknown": "escalate to hermes for diagnosis",
    }.get(failure_class, "escalate to hermes")


def is_operator_blocked(conn, tid: str, assignee: str | None, block_reason: str | None) -> bool:
    """Check if a blocked ticket represents an operator decision that should not be auto-unblocked.

    Returns True if the ticket should be left alone.
    """
    # 1. Assigned to operator
    if assignee and assignee.lower() in OPERATOR_ASSIGNEES:
        print(f"      🔒 Skipping: assigned to operator ({assignee})")
        return True

    # 2. Blocked by watchdog or spec-related reason
    if block_reason:
        reason_lower = block_reason.lower()
        for keyword in OPERATOR_BLOCK_REASONS:
            if keyword in reason_lower:
                print(f"      🔒 Skipping: operator block_reason ({block_reason})")
                return True

    # 3. Latest comment is from board-watchdog (overrides stale failure comments)
    latest = conn.execute("""
        SELECT body, author FROM task_comments
        WHERE task_id = ?
        ORDER BY created_at DESC LIMIT 1
    """, (tid,)).fetchone()
    if latest:
        body_lower = (latest["body"] or "").lower()
        author = latest["author"] or ""
        if author == "board-watchdog" or "spec" in body_lower or "blocked by watchdog" in body_lower:
            print(f"      🔒 Skipping: latest comment is watchdog block (author={author})")
            return True

    # 4. Loop detection — if any comment mentions a promote-demote loop, never auto-unblock
    loop_comments = conn.execute("""
        SELECT COUNT(*) FROM task_comments
        WHERE task_id = ? AND (body LIKE '%loop_detected%' OR body LIKE '%promote-demote loop%')
    """, (tid,)).fetchone()[0]
    if loop_comments > 0:
        print(f"      🔒 Skipping: loop_detected=True ({loop_comments} loop comments)")
        return True

    return False


SPEC_KIT_REF_RE = re.compile(
    r"(?P<path>(?:[A-Za-z0-9_.~/-]*/)?\.specify/specs/"
    r"(?P<slug>[0-9A-Za-z][0-9A-Za-z_.-]*)/spec\.md)"
)


def has_spec_kit_entry(conn, tid: str) -> bool:
    """Check if the ticket references an existing board-appropriate spec.md."""
    row = conn.execute("SELECT body FROM tasks WHERE id = ?", (tid,)).fetchone()
    body = (row["body"] or "") if row else ""
    spec_root = spec_dir_for_board(BOARD).expanduser().resolve()
    for match in SPEC_KIT_REF_RE.finditer(body):
        candidate = (spec_root / match.group("slug") / "spec.md").resolve()
        try:
            candidate.relative_to(spec_root)
        except ValueError:
            continue
        if candidate.is_file():
            return True
    return False


def claim_priority_tickets(conn):
    """Claim P0/P1 tickets for hermes before clawta poller sees them.

    Uses claim_lock for atomicity — if clawta already claimed, we skip.
    """
    stats = {"claimed_for_hermes": 0, "left_for_clawta": 0, "skipped": 0}

    rows = conn.execute("""
        SELECT id, title, priority, assignee, claim_lock
        FROM tasks
        WHERE status = 'ready'
          AND priority >= ?
        ORDER BY priority DESC, id ASC
    """, (HERMES_PRIORITY_THRESHOLD,)).fetchall()

    for row in rows:
        tid = row["id"]
        title = row["title"][:60]
        pri = row["priority"]
        assignee = row["assignee"] or ""
        claim_lock = row["claim_lock"]

        # Skip if already claimed by someone else
        if claim_lock and assignee != "hermes":
            print(f"  ⏭️  Skipping {tid} (claimed by {assignee})")
            stats["skipped"] += 1
            continue

        print(f"  🎯 Claiming {tid} (P{pri}): {title}")
        if not DRY_RUN:
            # Start = claim + move to in_progress
            stdout, rc = run_cmd([KANBAN_FLOW, "start", tid, "--author", "hermes"])
            if rc == 0:
                # Assign to hermes explicitly
                run_cmd(["hermes", "kanban", "--board", BOARD, "assign", tid, "hermes"])
                run_cmd([
                    "hermes", "kanban", "--board", BOARD, "comment", tid,
                    "--author", "hermes",
                    f"🎯 Hermes claimed (P{pri} priority). Clawta dispatch will skip this ticket.",
                ])
                stats["claimed_for_hermes"] += 1
            else:
                print(f"  ⚠️  Failed to claim {tid}: {stdout}")
                stats["skipped"] += 1
        else:
            stats["claimed_for_hermes"] += 1

    # Count ready tickets left for clawta
    clawta_ready = conn.execute("""
        SELECT COUNT(*) as cnt FROM tasks
        WHERE status = 'ready' AND priority < ?
    """, (HERMES_PRIORITY_THRESHOLD,)).fetchone()["cnt"]
    stats["left_for_clawta"] = clawta_ready

    return stats


def escalate_failures(conn):
    """Escalate failed clawta workers to hermes with structured failure packets.

    Guardrails:
    - Never auto-unblock tickets assigned to operator (red/jp)
    - Never auto-unblock tickets blocked by watchdog or for spec reasons
    - Only classify based on the LATEST comment, not stale history
    - Skip tickets without an existing spec-kit entry
    """
    stats = {"escalated_from_clawta": 0, "auto_unblocked": 0, "operator_blocked": 0}

    # Find blocked tickets — but only use the LATEST comment for classification,
    # not stale history. Old "silent_death" comments should not trigger unblock
    # if the latest comment is a watchdog block.
    blocked = conn.execute("""
        SELECT t.id, t.title, t.priority, t.assignee, t.block_reason,
               (SELECT tc_latest.body FROM task_comments tc_latest
                WHERE tc_latest.task_id = t.id
                ORDER BY tc_latest.created_at DESC LIMIT 1) as latest_comment
        FROM tasks t
        WHERE t.status = 'blocked'
        ORDER BY t.priority DESC
    """).fetchall()

    for row in blocked:
        tid = row["id"]
        title = row["title"][:65]
        pri = row["priority"]
        assignee = row["assignee"]
        block_reason = row["block_reason"]
        latest_comment = row["latest_comment"] or ""

        # Guard 1: Never auto-unblock operator-blocked tickets
        if is_operator_blocked(conn, tid, assignee, block_reason):
            stats["operator_blocked"] += 1
            continue

        # Guard 2: Only classify based on LATEST comment, not stale history
        # If latest comment doesn't look like a worker failure, skip
        latest_lower = latest_comment.lower()
        has_failure_signal = any([
            "worker" in latest_lower and "fail" in latest_lower,
            "gh pr create failed" in latest_lower,
            "stale" in latest_lower and "worker" in latest_lower,
            "silent" in latest_lower and "death" in latest_lower,
            "explicit" in latest_lower and "fail" in latest_lower,
            "ci" in latest_lower and "fail" in latest_lower,
        ])
        if not has_failure_signal:
            print(f"  ⏭️  {tid}: latest comment is not a worker failure signal — skipping")
            stats["operator_blocked"] += 1
            continue

        packet = build_failure_packet(tid, title, pri, latest_comment)
        action = packet["recommended_action"]
        fclass = packet["failure_class"]

        print(f"  ⚠️  {tid} (P{pri}, {fclass}): {title}")
        print(f"      Worker: {packet['worker']}/{packet['model']}")
        print(f"      Recommended: {action}")

        if DRY_RUN:
            stats["escalated_from_clawta"] += 1
            continue

        # Guard 3: Never unblock tickets without a board-appropriate spec-kit entry
        if not has_spec_kit_entry(conn, tid):
            print(f"      ⏭️  Skipping auto-unblock: no spec-kit entry for board {BOARD}")
            conn.execute("UPDATE tasks SET block_reason = ? WHERE id = ?", (fclass, tid))
            conn.commit()
            stats["operator_blocked"] += 1
            continue

        # For no_pr failures, auto-unblock (invariants + operator checks passed)
        if fclass == "no_pr":
            run_cmd([KANBAN_FLOW, "unblock", tid, "--author", "hermes", "--assignee", "hermes"])
            conn.execute("UPDATE tasks SET block_reason = ? WHERE id = ?", (fclass, tid))
            conn.commit()
            run_cmd([
                "hermes", "kanban", "--board", BOARD, "comment", tid,
                "--author", "hermes",
                f"🔄 Auto-unblocked: block_reason={fclass}. {action}.",
            ])
            stats["auto_unblocked"] += 1
            continue

        # For other failures, escalate to hermes with the failure packet
        run_cmd([KANBAN_FLOW, "unblock", tid, "--author", "hermes", "--assignee", "hermes"])
        conn.execute("UPDATE tasks SET block_reason = ? WHERE id = ?", (fclass, tid))
        conn.commit()
        run_cmd([
            "hermes", "kanban", "--board", BOARD, "comment", tid,
            "--author", "hermes",
            f"🔄 Escalated to Hermes: worker={packet['worker']}, model={packet['model']}, "
            f"failure={fclass}. Recommended action: {action}.",
        ])
        run_cmd([
            "openclaw", "message", "send", "--channel", "discord", "--account", "default",
            "--text", f"⚠️ Escalated {tid} (P{pri}) to Hermes: {fclass}. {action}.",
        ])
        stats["escalated_from_clawta"] += 1

    return stats


def main():
    if DRY_RUN:
        print("(dry-run mode — no mutations)")
    print(f"hermes-clawta-bridge: {datetime.now(timezone.utc).isoformat()} board={BOARD}")

    if not KANBAN_DB.exists():
        print(f"ERROR: no kanban DB at {KANBAN_DB}")
        sys.exit(1)

    conn = get_db()

    stats = {"claimed_for_hermes": 0, "left_for_clawta": 0,
             "escalated_from_clawta": 0, "skipped": 0, "auto_unblocked": 0,
             "operator_blocked": 0}

    print("=== Claiming P0/P1 for Hermes ===")
    claim_stats = claim_priority_tickets(conn)
    stats.update(claim_stats)

    print("\n=== Escalating failed Clawta workers ===")
    escalate_stats = escalate_failures(conn)
    # Merge escalated count (don't overwrite claimed/left/skipped)
    stats["escalated_from_clawta"] = escalate_stats["escalated_from_clawta"]
    stats["auto_unblocked"] = escalate_stats["auto_unblocked"]
    stats["operator_blocked"] = escalate_stats["operator_blocked"]

    print()
    emit_telemetry(stats)

    conn.close()


if __name__ == "__main__":
    main()