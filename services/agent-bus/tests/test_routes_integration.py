"""End-to-end: bus_post_thread stamps discord_thread_id from the routes table.

Closes the canonical "Discord-dark thread" bug (mem 3695). The
boundaries:

- Empty routes table → thread created with discord_thread_id=NULL (warning to stderr; bus write still succeeds).
- Board-scoped route exists → thread's discord_thread_id = board's channel.
- Audience overrides board.
- Explicit mute (channel_id=NULL row) → thread has discord_thread_id=NULL.
- bus_routes_set / list / unset / resolve work end-to-end through the JSON-RPC dispatcher.
"""

from __future__ import annotations

import importlib
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SVC_DIR = Path(__file__).resolve().parents[1]


def _load(name: str):
    if str(SVC_DIR) not in sys.path:
        sys.path.insert(0, str(SVC_DIR))
    if name in sys.modules:
        del sys.modules[name]
    return importlib.import_module(name)


def _call(server, conn, tool: str, **args) -> dict:
    req = {"jsonrpc": "2.0", "id": 1, "method": "tools/call",
           "params": {"name": tool, "arguments": args}}
    resp = server.handle_request(conn, req)
    if "error" in resp:
        raise AssertionError(f"{tool} returned error: {resp['error']}")
    return json.loads(resp["result"]["content"][0]["text"])


class RoutesIntegrationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.db_path = Path(self.tmp.name) / "bus.db"
        os.environ["AGENT_BUS_DB"] = str(self.db_path)
        self.db = _load("db")
        self.server = _load("server")
        self.conn = self.db.connect(self.db_path)

        # Stub out the Discord HTTP push so tests stay offline. The
        # stub returns a fake snowflake when called with a channel_id,
        # None otherwise — matching production semantics.
        self._push_calls: list[dict] = []
        def _fake_push(*, channel_id, author, body):
            self._push_calls.append({"channel_id": channel_id, "author": author, "body": body})
            return f"snowflake-{channel_id}" if channel_id else None

        self._push_patch = mock.patch.object(
            self.server.discord_push, "try_push", side_effect=_fake_push,
        )
        self._push_patch.start()

    def tearDown(self) -> None:
        self._push_patch.stop()
        self.conn.close()
        self.tmp.cleanup()
        os.environ.pop("AGENT_BUS_DB", None)

    # ---- the bug we are fixing ----

    def test_thread_without_route_is_discord_dark_but_bus_write_succeeds(self):
        out = _call(self.server, self.conn, "bus_post_thread",
                    author="red", title="t", body="hello", board="chitin",
                    audience="ares,clawta")
        self.assertGreater(out["thread_id"], 0)
        self.assertIsNone(out["discord_channel_id"])
        self.assertIsNone(out["discord_message_id"])
        # try_push was NOT called because channel_id was None.
        self.assertEqual(self._push_calls, [])
        # Sanity: the bus row exists with NULL discord_thread_id.
        row = self.conn.execute(
            "SELECT discord_thread_id FROM threads WHERE id=?", (out["thread_id"],)
        ).fetchone()
        self.assertIsNone(row["discord_thread_id"])

    # ---- happy path with routes ----

    def test_board_route_stamps_channel_id_at_creation(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="chitin", channel_id="C-SWARM")
        out = _call(self.server, self.conn, "bus_post_thread",
                    author="red", title="t", body="hi", board="chitin")
        self.assertEqual(out["discord_channel_id"], "C-SWARM")
        self.assertEqual(out["discord_message_id"], "snowflake-C-SWARM")
        # bus_post_thread pushed exactly once.
        self.assertEqual(len(self._push_calls), 1)
        self.assertEqual(self._push_calls[0]["channel_id"], "C-SWARM")
        # And stamped the snowflake onto the messages row.
        row = self.conn.execute(
            "SELECT discord_message_id FROM messages WHERE id=?",
            (out["message_id"],),
        ).fetchone()
        self.assertEqual(row["discord_message_id"], "snowflake-C-SWARM")

    def test_single_agent_audience_wins_over_board(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="chitin", channel_id="C-SWARM")
        _call(self.server, self.conn, "bus_routes_set",
              scope="audience", key="clawta", channel_id="C-CLAWTA")
        out = _call(self.server, self.conn, "bus_post_thread",
                    author="red", title="t", body="hi",
                    board="chitin", audience="clawta")
        self.assertEqual(out["discord_channel_id"], "C-CLAWTA")

    def test_multi_agent_audience_falls_through_to_board(self):
        # Regression for the post-PR-789 hotfix: a thread with audience
        # naming multiple agents should land in the BOARD channel
        # (coordination), not in any single participant's audience channel.
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="chitin", channel_id="C-SWARM")
        _call(self.server, self.conn, "bus_routes_set",
              scope="audience", key="clawta", channel_id="C-CLAWTA")
        _call(self.server, self.conn, "bus_routes_set",
              scope="audience", key="ares", channel_id="C-ARES")
        out = _call(self.server, self.conn, "bus_post_thread",
                    author="red", title="coordination",
                    body="ping both", board="chitin", audience="ares,clawta")
        self.assertEqual(out["discord_channel_id"], "C-SWARM")

    def test_global_default_used_when_board_unmatched(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="global", key="*", channel_id="C-DEFAULT")
        out = _call(self.server, self.conn, "bus_post_thread",
                    author="red", title="t", body="hi", board="anything")
        self.assertEqual(out["discord_channel_id"], "C-DEFAULT")

    # ---- explicit mute ----

    def test_explicit_mute_row_makes_thread_discord_dark(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="global", key="*", channel_id="C-DEFAULT")
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="internal", channel_id=None)
        out = _call(self.server, self.conn, "bus_post_thread",
                    author="red", title="internal note", body="x",
                    board="internal")
        self.assertIsNone(out["discord_channel_id"])
        self.assertEqual(self._push_calls, [])

    # ---- CRUD via MCP ----

    def test_routes_list_initially_empty(self):
        out = _call(self.server, self.conn, "bus_routes_list")
        self.assertEqual(out["routes"], [])

    def test_routes_set_and_list_roundtrip(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="chitin", channel_id="C-1", priority=50)
        _call(self.server, self.conn, "bus_routes_set",
              scope="audience", key="clawta", channel_id="C-2")
        out = _call(self.server, self.conn, "bus_routes_list")
        self.assertEqual(len(out["routes"]), 2)
        names = [(r["scope"], r["key"], r["channel_id"]) for r in out["routes"]]
        self.assertIn(("board", "chitin", "C-1"), names)
        self.assertIn(("audience", "clawta", "C-2"), names)

    def test_routes_unset_removes_row(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="chitin", channel_id="C-1")
        out = _call(self.server, self.conn, "bus_routes_unset",
                    scope="board", key="chitin")
        self.assertTrue(out["removed"])
        out2 = _call(self.server, self.conn, "bus_routes_unset",
                     scope="board", key="chitin")
        self.assertFalse(out2["removed"])

    def test_routes_resolve_diagnostic(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="chitin", channel_id="C-SWARM")
        out = _call(self.server, self.conn, "bus_routes_resolve",
                    board="chitin", audience=None)
        self.assertTrue(out["routed"])
        self.assertEqual(out["channel_id"], "C-SWARM")
        self.assertFalse(out["muted"])

    def test_routes_resolve_unroutable(self):
        out = _call(self.server, self.conn, "bus_routes_resolve",
                    board="nope", audience=None)
        self.assertFalse(out["routed"])
        self.assertIn("no Discord route resolves", out["error"])

    def test_routes_resolve_mute(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="internal", channel_id=None)
        out = _call(self.server, self.conn, "bus_routes_resolve",
                    board="internal", audience=None)
        self.assertTrue(out["routed"])
        self.assertIsNone(out["channel_id"])
        self.assertTrue(out["muted"])

    # ---- determinism check: same input → same output ----

    def test_same_input_resolves_to_same_channel(self):
        _call(self.server, self.conn, "bus_routes_set",
              scope="board", key="chitin", channel_id="C-SWARM")
        results = set()
        for _ in range(10):
            r = _call(self.server, self.conn, "bus_routes_resolve",
                      board="chitin", audience="ares,clawta,red")
            results.add(r["channel_id"])
        self.assertEqual(results, {"C-SWARM"})


if __name__ == "__main__":
    unittest.main()
