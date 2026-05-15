from __future__ import annotations

import importlib.util
import sqlite3
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path
from unittest import mock


REPO_ROOT = Path(__file__).resolve().parents[2]
REPORT = REPO_ROOT / "swarm" / "bin" / "clawta-report"
SENTINEL = REPO_ROOT / "swarm" / "bin" / "clawta-worker-failure-sentinel"


def load_module(path: Path, name: str):
    spec = importlib.util.spec_from_loader(name, SourceFileLoader(name, str(path)))
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


class ControllerProjectionTests(unittest.TestCase):
    def test_projection_uses_active_worker_cap(self) -> None:
        module = load_module(REPORT, "clawta_report_projection")
        rows = module.projection_scenarios(queue_depth=5, active_count=0, max_active=2)
        self.assertEqual([row["label"] for row in rows], ["fast", "expected", "slow/sticky"])
        # 5 queued tickets at cap 2 drains in 3 waves. Expected scenario is 90m/wave.
        self.assertEqual(rows[1]["eta_seconds"], 3 * 90 * 60)

    def test_projection_accounts_for_active_workers(self) -> None:
        module = load_module(REPORT, "clawta_report_projection_active")
        # 5 queued + 2 mid-flight workers through 2 lanes: the active pair
        # occupies wave 1, so the queue needs 4 waves total (ceil(7/2)).
        rows = module.projection_scenarios(queue_depth=5, active_count=2, max_active=2)
        self.assertEqual(rows[1]["eta_seconds"], 4 * 90 * 60)
        # active_count=0 must still reduce to the plain ceil(queue/cap).
        rows0 = module.projection_scenarios(queue_depth=5, active_count=0, max_active=2)
        self.assertEqual(rows0[1]["eta_seconds"], 3 * 90 * 60)

    def test_active_dispatch_tickets_counts_lobster_and_long_lived_roots(self) -> None:
        module = load_module(REPORT, "clawta_report_active_dispatch")
        sample = "\n".join(["t_aaaabbbb", "t_ccccdddd", "t_ccccdddd"])
        with mock.patch.object(module, "sh", return_value=(0, sample)):
            self.assertEqual(module.active_dispatch_tickets(), ["t_aaaabbbb", "t_ccccdddd"])

    def test_lifecycle_orphans_flags_done_without_runs_and_runs_without_ticket(self) -> None:
        module = load_module(REPORT, "clawta_report_lifecycle_orphans")
        with tempfile.TemporaryDirectory() as tmpdir:
            db_path = Path(tmpdir) / "kanban.db"
            conn = sqlite3.connect(db_path)
            conn.executescript(
                """
                CREATE TABLE tasks (
                  id TEXT PRIMARY KEY,
                  title TEXT NOT NULL,
                  assignee TEXT,
                  status TEXT NOT NULL,
                  priority INTEGER DEFAULT 0,
                  created_at INTEGER NOT NULL,
                  started_at INTEGER,
                  completed_at INTEGER,
                  last_heartbeat_at INTEGER,
                  current_run_id INTEGER
                );
                CREATE TABLE task_comments (
                  id INTEGER PRIMARY KEY AUTOINCREMENT,
                  task_id TEXT NOT NULL,
                  author TEXT,
                  body TEXT NOT NULL,
                  created_at INTEGER NOT NULL
                );
                CREATE TABLE task_events (
                  id INTEGER PRIMARY KEY AUTOINCREMENT,
                  task_id TEXT NOT NULL,
                  kind TEXT NOT NULL,
                  payload TEXT,
                  created_at INTEGER,
                  run_id INTEGER
                );
                CREATE TABLE task_runs (
                  id INTEGER PRIMARY KEY AUTOINCREMENT,
                  task_id TEXT NOT NULL,
                  status TEXT NOT NULL,
                  started_at INTEGER NOT NULL,
                  ended_at INTEGER,
                  outcome TEXT,
                  summary TEXT,
                  last_heartbeat_at INTEGER
                );
                INSERT INTO tasks(id, title, assignee, status, created_at, completed_at)
                VALUES
                  ('t_done0001', 'done but no runs', 'codex', 'done', 1, 10),
                  ('t_done0002', 'done with run', 'codex', 'done', 1, 11);
                INSERT INTO task_runs(task_id, status, started_at, ended_at, outcome)
                VALUES
                  ('t_done0002', 'done', 2, 11, 'completed'),
                  ('t_orphan01', 'failed', 3, 4, 'failed');
                """
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "BOARD_DB", str(db_path)):
                orphan_data = module.lifecycle_orphans()

        self.assertEqual([row["id"] for row in orphan_data["done_without_runs"]], ["t_done0001"])
        self.assertEqual([row["task_id"] for row in orphan_data["runs_without_ticket"]], ["t_orphan01"])


class StuckClassificationTests(unittest.TestCase):
    def test_classify_stuck_ticket_buckets_actionable_states(self) -> None:
        module = load_module(REPORT, "clawta_report_stuck_classification")
        task = {"id": "t_deadbeef", "assignee": "codex", "consecutive_failures": 0}

        self.assertEqual(module.classify_stuck_ticket(task, pr={"number": 1})["bucket"], "has-pr")
        self.assertEqual(module.classify_stuck_ticket({**task, "assignee": "red"})["bucket"], "operator-owned")
        self.assertEqual(module.classify_stuck_ticket(task, active=True)["bucket"], "active-worker")
        self.assertEqual(module.classify_stuck_ticket({**task, "consecutive_failures": 2})["bucket"], "retry-exhausting")
        self.assertEqual(module.classify_stuck_ticket(task, stale_note="watchdog flagged")["bucket"], "watchdog-flagged")
        self.assertEqual(module.classify_stuck_ticket(task)["bucket"], "retryable-silent")

    def test_block_reason_takes_precedence_in_stuck_classification(self) -> None:
        module = load_module(REPORT, "clawta_report_block_reason_stuck")
        task = {"id": "t_deadbeef", "assignee": "codex", "consecutive_failures": 0, "block_reason": "retry-exhausted"}

        result = module.classify_stuck_ticket(task)
        self.assertEqual(result["bucket"], "block:retry-exhausted")
        self.assertEqual(result["tone"], "bad")
        self.assertIn("Hermes", result["action"])

    def test_block_reason_metadata_declares_owner(self) -> None:
        module = load_module(REPORT, "clawta_report_block_reason_meta")
        self.assertEqual(module.block_reason_meta("operator-decision")["owner"], "red")
        self.assertEqual(module.block_reason_meta("needs-rebase")["owner"], "clawta")
        self.assertEqual(module.block_reason_meta("poller-oscillation")["owner"], "hermes")
        self.assertEqual(module.block_reason_meta("new-reason")["owner"], "unknown")

    def test_load_board_state_handles_partial_retry_schema(self) -> None:
        module = load_module(REPORT, "clawta_report_partial_retry_schema")

        class FakeConn:
            row_factory = None
            def execute(self, sql):
                if sql == "PRAGMA table_info(tasks)":
                    return [(0, "id"), (1, "consecutive_failures")]
                if "FROM tasks" in sql:
                    self.task_sql = sql
                    return []
                return []
            def close(self):
                pass

        conn = FakeConn()
        with mock.patch.object(module, "board_conn", return_value=conn):
            tasks, comments, events, runs = module.load_board_state()

        self.assertEqual(tasks, [])
        self.assertIn("consecutive_failures", conn.task_sql)
        self.assertIn("NULL AS last_failure_error", conn.task_sql)
        self.assertIn("NULL AS block_reason", conn.task_sql)


class CopilotSentinelTests(unittest.TestCase):
    def test_quarantine_since_honors_healthy_checkpoint(self) -> None:
        module = load_module(SENTINEL, "clawta_worker_failure_sentinel_checkpoint")
        with mock.patch.object(module, "QUARANTINE_WINDOW", 7200):
            self.assertEqual(module.copilot_quarantine_since(10_000, {}), 2_800)
            self.assertEqual(
                module.copilot_quarantine_since(10_000, {module.COPILOT_HEALTHY_AFTER_KEY: 9_500}),
                9_500,
            )


if __name__ == "__main__":
    unittest.main()
