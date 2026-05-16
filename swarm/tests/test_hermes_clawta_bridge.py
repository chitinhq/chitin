from __future__ import annotations

import importlib.util
import sqlite3
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
BRIDGE = ROOT / "swarm" / "workflows" / "hermes-clawta-bridge.py"
INSTALLER = ROOT / "swarm" / "bin" / "install-hermes-clawta-bridge.sh"


def load_module():
    spec = importlib.util.spec_from_loader(
        "hermes_clawta_bridge_test",
        SourceFileLoader("hermes_clawta_bridge_test", str(BRIDGE)),
    )
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules["hermes_clawta_bridge_test"] = module
    spec.loader.exec_module(module)
    return module


def make_conn() -> sqlite3.Connection:
    conn = sqlite3.connect(":memory:")
    conn.row_factory = sqlite3.Row
    conn.executescript(
        """
        CREATE TABLE tasks (
          id TEXT PRIMARY KEY,
          title TEXT,
          body TEXT,
          status TEXT,
          priority INTEGER,
          assignee TEXT,
          block_reason TEXT,
          claim_lock TEXT
        );
        CREATE TABLE task_comments (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          task_id TEXT NOT NULL,
          author TEXT,
          body TEXT NOT NULL,
          created_at INTEGER NOT NULL
        );
        """
    )
    return conn


class HermesClawtaBridgeTests(unittest.TestCase):
    def test_loop_detected_comment_is_operator_blocked(self) -> None:
        module = load_module()
        conn = make_conn()
        conn.execute(
            """
            INSERT INTO task_comments(task_id, author, body, created_at)
            VALUES (?, ?, ?, ?)
            """,
            (
                "t_looped",
                "board-watchdog",
                "Blocked: promote-demote loop detected: needs manual spec",
                1,
            ),
        )
        self.assertTrue(module.is_operator_blocked(conn, "t_looped", None, None))

    def test_loop_detected_block_reason_is_operator_blocked(self) -> None:
        module = load_module()
        conn = make_conn()
        self.assertTrue(
            module.is_operator_blocked(
                conn,
                "t_looped",
                None,
                "loop_detected=true: needs manual spec",
            )
        )

    def test_has_spec_kit_entry_uses_owned_repo_spec_root(self) -> None:
        module = load_module()
        conn = make_conn()
        with tempfile.TemporaryDirectory() as tmp:
            spec = Path(tmp) / "001-owned" / "spec.md"
            spec.parent.mkdir(parents=True)
            spec.write_text("# spec\n")
            conn.execute(
                "INSERT INTO tasks(id, title, body, status, priority) VALUES (?, ?, ?, ?, ?)",
                ("t_owned", "owned", "Spec: .specify/specs/001-owned/spec.md", "blocked", 50),
            )
            with mock.patch.object(module, "BOARD", "chitin"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertTrue(module.has_spec_kit_entry(conn, "t_owned"))

    def test_has_spec_kit_entry_rejects_missing_spec_file(self) -> None:
        module = load_module()
        conn = make_conn()
        with tempfile.TemporaryDirectory() as tmp:
            conn.execute(
                "INSERT INTO tasks(id, title, body, status, priority) VALUES (?, ?, ?, ?, ?)",
                ("t_shared", "shared", "Spec: .specify/specs/777-shared/spec.md", "blocked", 50),
            )
            with mock.patch.object(module, "BOARD", "readybench"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertFalse(module.has_spec_kit_entry(conn, "t_shared"))

    def test_has_spec_kit_entry_accepts_spec_that_references_ticket_id(self) -> None:
        module = load_module()
        conn = make_conn()
        with tempfile.TemporaryDirectory() as tmp:
            spec = Path(tmp) / "009-poller-respects-spec-kit" / "spec.md"
            spec.parent.mkdir(parents=True)
            spec.write_text("# Poller respects spec-kit\n\n**Refs**: t_75c8c8c1\n")
            conn.execute(
                "INSERT INTO tasks(id, title, body, status, priority) VALUES (?, ?, ?, ?, ?)",
                ("t_75c8c8c1", "owned", "Acceptance only", "blocked", 50),
            )
            with mock.patch.object(module, "BOARD", "chitin"), mock.patch.object(
                module, "spec_dir_for_board", return_value=Path(tmp)
            ):
                self.assertTrue(module.has_spec_kit_entry(conn, "t_75c8c8c1"))

    def test_claim_priority_tickets_skips_ready_ticket_without_spec(self) -> None:
        module = load_module()
        conn = make_conn()
        conn.execute(
            """
            INSERT INTO tasks(id, title, body, status, priority, assignee, claim_lock)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                "t_nospec",
                "high priority without spec",
                "Acceptance: do the thing",
                "ready",
                80,
                None,
                None,
            ),
        )

        calls: list[list[str]] = []

        def fake_run_cmd(args, **kwargs):
            calls.append(list(args))
            return "", 0

        with mock.patch.object(module, "DRY_RUN", False), mock.patch.object(
            module, "run_cmd", side_effect=fake_run_cmd
        ), mock.patch.object(module, "spec_dir_for_board", return_value=Path("/tmp/no-specs-here")):
            stats = module.claim_priority_tickets(conn)

        self.assertEqual(stats["claimed_for_hermes"], 0)
        self.assertEqual(stats["skipped"], 1)
        self.assertFalse(
            any(call[:2] == [module.KANBAN_FLOW, "start"] for call in calls),
            f"missing-spec ticket must not be started; calls={calls}",
        )
        self.assertTrue(
            any("missing spec-kit entry" in " ".join(call) for call in calls),
            f"missing-spec skip should be commented; calls={calls}",
        )

    def test_installer_targets_hermes_scripts_path(self) -> None:
        installer = INSTALLER.read_text()
        self.assertIn("HERMES_SCRIPTS_DIR", installer)
        self.assertIn("$HOME/.hermes/scripts", installer)
        self.assertIn("hermes-clawta-bridge.py", installer)
        self.assertIn("ln -sfn", installer)


if __name__ == "__main__":
    unittest.main()
