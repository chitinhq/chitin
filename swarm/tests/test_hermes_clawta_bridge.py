from __future__ import annotations

import importlib.util
import sqlite3
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path


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

    def test_installer_targets_hermes_scripts_path(self) -> None:
        installer = INSTALLER.read_text()
        self.assertIn("HERMES_SCRIPTS_DIR", installer)
        self.assertIn("$HOME/.hermes/scripts", installer)
        self.assertIn("hermes-clawta-bridge.py", installer)
        self.assertIn("ln -sfn", installer)


if __name__ == "__main__":
    unittest.main()
