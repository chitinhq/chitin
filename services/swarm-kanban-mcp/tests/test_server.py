"""Integration tests for swarm-kanban-mcp.

# spec: 037-swarm-kanban-mcp (forthcoming)

Tests the JSON-RPC handlers + tool implementations against the live
kanban DBs (read-only path). Mutating tests use a tempdir-backed fake.
"""
from __future__ import annotations

import sqlite3
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import server  # noqa: E402


class HandleRequestTests(unittest.TestCase):
    def test_initialize(self):
        resp = server.handle_request({"jsonrpc": "2.0", "id": 1, "method": "initialize"})
        self.assertEqual(resp["result"]["protocolVersion"], "2024-11-05")
        self.assertEqual(resp["result"]["serverInfo"]["name"], "swarm-kanban-mcp")

    def test_tools_list_includes_board_tools(self):
        resp = server.handle_request({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
        names = {t["name"] for t in resp["result"]["tools"]}
        self.assertEqual(names, {
            "list_boards", "list_tickets", "get_ticket",
            "claim_ticket", "update_status", "create_ticket",
        })

    def test_unknown_method_errors(self):
        resp = server.handle_request({"jsonrpc": "2.0", "id": 3, "method": "frobnicate"})
        self.assertEqual(resp["error"]["code"], -32601)

    def test_unknown_tool_errors(self):
        resp = server.handle_request({"jsonrpc": "2.0", "id": 4, "method": "tools/call",
                                       "params": {"name": "frob", "arguments": {}}})
        self.assertEqual(resp["error"]["code"], -32601)

    def test_notifications_initialized_returns_none(self):
        resp = server.handle_request({"jsonrpc": "2.0",
                                       "method": "notifications/initialized"})
        self.assertIsNone(resp)


class FakeBoardTests(unittest.TestCase):
    """Mutating tests against a tempdir kanban fake."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp(prefix="swarm-kanban-mcp-")
        self.boards = Path(self.tmp) / "boards"
        self.board = self.boards / "testboard"
        self.board.mkdir(parents=True)
        self.db_path = self.board / "kanban.db"
        # Minimal schema matching the real boards
        conn = sqlite3.connect(self.db_path)
        conn.executescript("""
            CREATE TABLE tasks (id TEXT PRIMARY KEY, title TEXT, status TEXT,
                                assignee TEXT, priority INTEGER, body TEXT);
            CREATE TABLE task_comments (id INTEGER PRIMARY KEY, task_id TEXT,
                                        author TEXT, body TEXT, created_at INTEGER);
        """)
        conn.execute("INSERT INTO tasks VALUES('t_test1','test ticket','ready','red',1,'x')")
        conn.execute("INSERT INTO tasks VALUES('t_test2','other','triage','clawta',2,'y')")
        conn.commit()
        conn.close()
        self.patcher = mock.patch.object(server, "KANBAN_ROOT", self.boards)
        self.patcher.start()

    def tearDown(self):
        self.patcher.stop()

    def test_list_boards_includes_fake(self):
        out = server.list_boards()
        self.assertIn("testboard", out["boards"])

    def test_list_tickets_returns_both(self):
        out = server.list_tickets("testboard")
        self.assertEqual(len(out["tickets"]), 2)

    def test_list_tickets_filters_by_status(self):
        out = server.list_tickets("testboard", status="ready")
        self.assertEqual(len(out["tickets"]), 1)
        self.assertEqual(out["tickets"][0]["id"], "t_test1")

    def test_get_ticket_returns_detail(self):
        out = server.get_ticket("testboard", "t_test1")
        self.assertEqual(out["ticket"]["id"], "t_test1")
        self.assertEqual(out["comments"], [])

    def test_get_ticket_raises_on_unknown(self):
        with self.assertRaises(ValueError):
            server.get_ticket("testboard", "t_nonexistent")

    def test_list_tickets_raises_on_unknown_board(self):
        with self.assertRaises(ValueError):
            server.list_tickets("notaboard")


if __name__ == "__main__":
    unittest.main()
