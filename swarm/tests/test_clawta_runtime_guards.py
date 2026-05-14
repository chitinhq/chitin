from __future__ import annotations

import importlib.util
import os
import sqlite3
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path
from unittest import mock


REPO_ROOT = Path(__file__).resolve().parents[2]
BLOCKED_ESCALATOR = REPO_ROOT / "swarm" / "bin" / "clawta-blocked-escalator"
STALE_WATCHDOG = REPO_ROOT / "swarm" / "bin" / "clawta-stale-worker-watchdog"


def load_module(path: Path, name: str):
    spec = importlib.util.spec_from_loader(name, SourceFileLoader(name, str(path)))
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


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
          payload TEXT
        );
        """
    )
    conn.commit()
    conn.close()
    return db_path


class BlockedEscalatorTests(unittest.TestCase):
    def test_lists_only_non_red_blocked(self) -> None:
        module = load_module(BLOCKED_ESCALATOR, "clawta_blocked_escalator")
        with tempfile.TemporaryDirectory() as tmp:
            db_path = make_db(Path(tmp))
            conn = sqlite3.connect(db_path)
            conn.executemany(
                """
                INSERT INTO tasks(id, status, assignee, title, priority, created_at)
                VALUES (?, ?, ?, ?, ?, ?)
                """,
                [
                    ("t_keep", "blocked", "red", "already owned", 10, 10),
                    ("t_move", "blocked", "codex", "needs operator", 20, 5),
                    ("t_skip", "ready", "codex", "not blocked", 30, 1),
                ],
            )
            conn.commit()
            conn.close()

            with mock.patch.object(module, "KANBAN_DB", db_path), mock.patch.object(
                module, "RED_ASSIGNEE", "red"
            ):
                candidates = module.list_candidates()

        self.assertEqual([ticket.id for ticket in candidates], ["t_move"])
        self.assertEqual(candidates[0].assignee, "codex")

    def test_reassigns_and_comments(self) -> None:
        module = load_module(BLOCKED_ESCALATOR, "clawta_blocked_escalator_apply")
        seen: list[list[str]] = []

        def fake_run(cmd, **kwargs):
            seen.append(list(cmd))
            return None

        with mock.patch.object(module, "run", side_effect=fake_run):
            module.reassign(module.BlockedTicket("t_1", "codex", "title"))

        self.assertEqual(
            seen[0],
            [
                module.HERMES_BIN,
                "kanban",
                "--board",
                module.BOARD,
                "assign",
                "t_1",
                module.RED_ASSIGNEE,
            ],
        )
        self.assertEqual(
            seen[1][0:6],
            [
                module.HERMES_BIN,
                "kanban",
                "--board",
                module.BOARD,
                "comment",
                "t_1",
            ],
        )
        self.assertIn("Previous assignee: codex.", seen[1][-1])


class StaleWatchdogTests(unittest.TestCase):
    def test_requires_all_stale_conditions(self) -> None:
        module = load_module(STALE_WATCHDOG, "clawta_stale_watchdog")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            db_path = make_db(root)
            logs = root / "logs"
            logs.mkdir()
            now = 10_000

            conn = sqlite3.connect(db_path)
            conn.executemany(
                """
                INSERT INTO tasks(id, status, assignee, title, priority, created_at, started_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                [
                    ("t_stale", "in_progress", "codex", "stale", 10, 1_000, 6_000),
                    ("t_pr", "in_progress", "codex", "has pr", 10, 1_000, 6_000),
                    ("t_busy", "in_progress", "codex", "still running", 10, 1_000, 6_000),
                    ("t_fresh", "in_progress", "codex", "not old enough", 10, 1_000, 9_200),
                ],
            )
            conn.execute(
                "INSERT INTO task_events(task_id, kind, payload) VALUES (?, ?, ?)",
                ("t_pr", "pr_opened", "{}"),
            )
            conn.commit()
            conn.close()

            stale_log = logs / "dispatch-t_stale.log"
            busy_log = logs / "dispatch-t_busy.log"
            fresh_log = logs / "dispatch-t_fresh.log"
            stale_log.write_text("quiet\n")
            busy_log.write_text("active\n")
            fresh_log.write_text("fresh\n")
            os.utime(stale_log, (now - 1300, now - 1300))
            os.utime(busy_log, (now - 1300, now - 1300))
            os.utime(fresh_log, (now - 100, now - 100))

            with mock.patch.object(module, "KANBAN_DB", db_path), mock.patch.object(
                module, "OPENCLAW_LOG_DIR", logs
            ), mock.patch.object(module, "STALE_AFTER", 2700), mock.patch.object(
                module, "QUIET_AFTER", 1200
            ), mock.patch.object(
                module, "has_active_worker", side_effect=lambda ticket_id: ticket_id == "t_busy"
            ), mock.patch.object(
                module.time, "time", return_value=now
            ):
                checked, stale = module.find_stale_candidates(now=now)

        self.assertEqual(len(checked), 4)
        self.assertEqual([item.id for item in stale], ["t_stale"])
        self.assertEqual(stale[0].quiet_seconds, 1300)
        self.assertIn("stale in_progress without PR or active worker", stale[0].reason)

    def test_blocks_assigns_and_comments(self) -> None:
        module = load_module(STALE_WATCHDOG, "clawta_stale_watchdog_apply")
        seen: list[list[str]] = []

        def fake_run(cmd, **kwargs):
            seen.append(list(cmd))
            return None

        escalation = module.Escalation(
            id="t_2",
            assignee="codex",
            title="title",
            age_seconds=3000,
            quiet_seconds=1500,
            reason="stale in_progress without PR or active worker after 3000s; dispatch log quiet 1500s",
        )
        with mock.patch.object(module, "run", side_effect=fake_run):
            module.block_ticket(escalation)

        self.assertEqual(seen[0][0], str(module.KANBAN_FLOW_BIN))
        self.assertEqual(seen[0][1:5], ["block", "t_2", escalation.reason, "--author"])
        self.assertEqual(
            seen[1],
            [
                module.HERMES_BIN,
                "kanban",
                "--board",
                module.BOARD,
                "assign",
                "t_2",
                module.RED_ASSIGNEE,
            ],
        )
        self.assertIn("Stale-worker watchdog escalated to red:", seen[2][-1])


if __name__ == "__main__":
    unittest.main()
