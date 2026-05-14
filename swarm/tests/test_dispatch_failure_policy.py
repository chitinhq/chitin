from __future__ import annotations

import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from swarm.lib import dispatch_failure_policy as policy


def make_db(root: Path) -> Path:
    db_path = root / "kanban.db"
    conn = sqlite3.connect(db_path)
    conn.executescript(
        """
        CREATE TABLE tasks (
          id TEXT PRIMARY KEY,
          status TEXT NOT NULL,
          assignee TEXT,
          title TEXT NOT NULL,
          priority INTEGER DEFAULT 0,
          created_at INTEGER NOT NULL,
          started_at INTEGER
        );
        CREATE TABLE task_events (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          task_id TEXT NOT NULL,
          kind TEXT NOT NULL,
          payload TEXT,
          created_at INTEGER
        );
        CREATE TABLE task_comments (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          task_id TEXT NOT NULL,
          author TEXT,
          body TEXT,
          created_at INTEGER
        );
        """
    )
    conn.execute(
        """
        INSERT INTO tasks(id, status, assignee, title, priority, created_at, started_at)
        VALUES (?, ?, ?, ?, ?, ?, ?)
        """,
        ("t_retry", "in_progress", "codex", "retry me", 10, 1_000, 1_100),
    )
    conn.executemany(
        """
        INSERT INTO task_comments(task_id, author, body, created_at)
        VALUES (?, ?, ?, ?)
        """,
        [
            ("t_retry", "clawta", "clawta-poller dispatching to codex. Sequence reason: first", 1_200),
            ("t_retry", "clawta", "clawta-poller dispatching to codex. Sequence reason: second", 1_500),
            ("t_retry", "clawta", "clawta-poller dispatching to codex. Sequence reason: third", 1_800),
        ],
    )
    conn.commit()
    conn.close()
    return db_path


class DispatchFailurePolicyTests(unittest.TestCase):
    def test_third_silent_failure_escalates_with_structured_history(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))

            first = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="silent_worker_death",
                reason="stale in_progress without PR or active worker after 3000s; dispatch log quiet 3136s",
                details={"assignee": "codex", "quiet_seconds": 3136},
                retry_limit=3,
                now=2_000,
            )
            second = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="silent_worker_death",
                reason="stale in_progress without PR or active worker after 3200s; dispatch log quiet 3210s",
                details={"assignee": "codex", "quiet_seconds": 3210},
                retry_limit=3,
                now=2_100,
            )
            third = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="silent_worker_death",
                reason="stale in_progress without PR or active worker after 1800s; dispatch log quiet 1788s",
                details={"assignee": "codex", "quiet_seconds": 1788},
                retry_limit=3,
                now=2_200,
            )

        self.assertEqual(first.action, "retry")
        self.assertEqual(second.action, "retry")
        self.assertEqual(third.action, "escalate")
        self.assertEqual(third.dispatch_failure_count, 3)
        self.assertIn("Watchdog: ticket bounced 3× with silent worker death.", third.comment)
        self.assertIn("Dispatch history:", third.comment)
        self.assertIn("codex -> silent at 3136s", third.comment)
        self.assertIn("codex -> silent at 3210s", third.comment)
        self.assertIn("codex -> silent at 1788s", third.comment)
        self.assertIn("3 silent worker deaths", third.block_reason)

    def test_explicit_failure_escalates_immediately_after_silent_retry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))

            silent = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="silent_worker_death",
                reason="stale in_progress without PR or active worker after 3000s; dispatch log quiet 3136s",
                details={"assignee": "codex", "quiet_seconds": 3136},
                retry_limit=3,
                now=2_000,
            )
            explicit = policy.plan_explicit_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="worker_nonzero_exit",
                reason="worker failed: exit code 23: pytest exploded",
                details={"assignee": "codex", "returncode": 23},
                retry_limit=3,
                now=2_100,
            )

        self.assertEqual(silent.action, "retry")
        self.assertEqual(explicit.action, "escalate")
        self.assertFalse(explicit.retry_eligible)
        self.assertEqual(explicit.dispatch_failure_count, 1)
        self.assertEqual(explicit.block_reason, "worker failed: exit code 23: pytest exploded")
        self.assertEqual(explicit.comment, "worker failed: exit code 23: pytest exploded")

    def test_mixed_retryable_failures_use_generic_escalation_history(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))

            first = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="missing_worktree",
                reason="spawn_worker completed but no worktree found",
                details={"assignee": "codex"},
                retry_limit=2,
                now=2_000,
            )
            second = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="gh_pr_create_failed",
                reason="branch pushed, but gh pr create failed",
                details={"assignee": "codex", "branch": "swarm/codex-t_retry"},
                retry_limit=2,
                now=2_100,
            )

        self.assertEqual(first.action, "retry")
        self.assertEqual(second.action, "escalate")
        self.assertIn(
            "Dispatch watchdog: ticket bounced 2× on retry-eligible infrastructure failures.",
            second.comment,
        )
        self.assertIn("codex -> no worktree", second.comment)
        self.assertIn("codex -> gh pr create failed (swarm/codex-t_retry)", second.comment)
        self.assertIn("2 retry-eligible dispatch failures", second.block_reason)

    def test_boundary_empty_branch_records_unknown_branch_summary(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))

            decision = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="empty_branch",
                reason="worker finished but no feature branch checked out",
                details={"assignee": "codex", "branch": ""},
                retry_limit=1,
                now=2_000,
            )

        self.assertEqual(decision.action, "escalate")
        self.assertIn("empty branch (unknown)", decision.comment)

    def test_boundary_max_retry_limit_escalates_at_exact_limit(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))

            first = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="silent_worker_death",
                reason="first silent failure",
                details={"assignee": "codex"},
                retry_limit=2,
                now=2_000,
            )
            second = policy.plan_retryable_failure(
                db_path,
                ticket_id="t_retry",
                failure_class="silent_worker_death",
                reason="second silent failure",
                details={"assignee": "codex"},
                retry_limit=2,
                now=2_100,
            )

        self.assertEqual(first.action, "retry")
        self.assertEqual(second.action, "escalate")
        self.assertEqual(second.dispatch_failure_count, 2)
        self.assertIn("Watchdog: ticket bounced 2× with silent worker death.", second.comment)

    def test_boundary_error_rejects_invalid_details_json(self) -> None:
        result = subprocess.run(
            [
                sys.executable,
                str(Path(policy.__file__)),
                "retryable",
                "--ticket-id",
                "t_retry",
                "--failure-class",
                "empty_branch",
                "--reason",
                "bad json",
                "--details-json",
                '{"branch":',
            ],
            check=False,
            capture_output=True,
            text=True,
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid --details-json", result.stderr)
