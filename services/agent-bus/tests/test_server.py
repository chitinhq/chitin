"""agent-bus integration tests via the JSON-RPC dispatcher.

Drives `handle_request` directly (no subprocess) so a failure shows the
actual line that broke. Each test gets a fresh sqlite DB at AGENT_BUS_DB
so they're independent.
"""
from __future__ import annotations

import importlib.util
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path


SVC_DIR = Path(__file__).resolve().parents[1]


def _load(name: str):
    """Load services/agent-bus/<name>.py as a module without polluting
    the import path permanently. Tests run via `python3 -m unittest` from
    the repo root, so SVC_DIR isn't on sys.path by default.
    """
    if str(SVC_DIR) not in sys.path:
        sys.path.insert(0, str(SVC_DIR))
    if name in sys.modules:
        del sys.modules[name]
    return importlib.import_module(name)


def _call(server, conn, tool: str, **args) -> dict:
    """Wrap a tools/call through the dispatcher; return the parsed tool payload."""
    req = {
        "jsonrpc": "2.0", "id": 1, "method": "tools/call",
        "params": {"name": tool, "arguments": args},
    }
    resp = server.handle_request(conn, req)
    if "error" in resp:
        raise AssertionError(f"{tool} returned error: {resp['error']}")
    return json.loads(resp["result"]["content"][0]["text"])


class AgentBusTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.db_path = Path(self.tmp.name) / "bus.db"
        os.environ["AGENT_BUS_DB"] = str(self.db_path)
        self.db = _load("db")
        self.server = _load("server")
        self.conn = self.db.connect(self.db_path)

    def tearDown(self) -> None:
        self.conn.close()
        self.tmp.cleanup()
        os.environ.pop("AGENT_BUS_DB", None)

    # ------- protocol surface -------

    def test_initialize_returns_protocol_handshake(self) -> None:
        req = {"jsonrpc": "2.0", "id": 1, "method": "initialize"}
        resp = self.server.handle_request(self.conn, req)
        self.assertIn("result", resp)
        self.assertIn("protocolVersion", resp["result"])
        self.assertEqual(resp["result"]["serverInfo"]["name"], "agent-bus")

    def test_tools_list_returns_seven_tools(self) -> None:
        req = {"jsonrpc": "2.0", "id": 2, "method": "tools/list"}
        resp = self.server.handle_request(self.conn, req)
        names = {t["name"] for t in resp["result"]["tools"]}
        self.assertEqual(names, {
            "bus_post_thread", "bus_reply", "bus_list_threads",
            "bus_read_thread", "bus_inbox", "bus_mark_read", "bus_attach",
        })

    def test_unknown_method_returns_method_not_found(self) -> None:
        req = {"jsonrpc": "2.0", "id": 3, "method": "no_such_method"}
        resp = self.server.handle_request(self.conn, req)
        self.assertEqual(resp["error"]["code"], -32601)

    def test_notification_returns_no_response(self) -> None:
        req = {"jsonrpc": "2.0", "method": "notifications/initialized"}
        self.assertIsNone(self.server.handle_request(self.conn, req))

    # ------- US1: post + reply + read -------

    def test_post_thread_persists_first_message(self) -> None:
        out = _call(self.server, self.conn, "bus_post_thread",
                    author="red", title="watchdog ask", body="@hermes please clear loop")
        self.assertGreater(out["thread_id"], 0)
        self.assertGreater(out["message_id"], 0)
        thread = _call(self.server, self.conn, "bus_read_thread",
                       thread_id=out["thread_id"])
        self.assertEqual(thread["thread"]["title"], "watchdog ask")
        self.assertEqual(len(thread["messages"]), 1)
        self.assertEqual(thread["messages"][0]["body"], "@hermes please clear loop")
        self.assertEqual(thread["messages"][0]["author"], "red")

    def test_reply_appends_and_bumps_thread_updated_at(self) -> None:
        post = _call(self.server, self.conn, "bus_post_thread",
                     author="red", title="t", body="hi")
        before = _call(self.server, self.conn, "bus_read_thread",
                       thread_id=post["thread_id"])["thread"]["updated_at"]
        # Sleep just enough to see updated_at advance (epoch seconds).
        import time; time.sleep(1.05)
        _call(self.server, self.conn, "bus_reply",
              author="hermes", thread_id=post["thread_id"], body="ack")
        after = _call(self.server, self.conn, "bus_read_thread",
                      thread_id=post["thread_id"])
        self.assertEqual(len(after["messages"]), 2)
        self.assertEqual(after["messages"][1]["author"], "hermes")
        self.assertGreater(after["thread"]["updated_at"], before)

    def test_reply_rejects_parent_from_different_thread(self) -> None:
        a = _call(self.server, self.conn, "bus_post_thread",
                  author="red", title="A", body="a1")
        b = _call(self.server, self.conn, "bus_post_thread",
                  author="red", title="B", body="b1")
        req = {"jsonrpc": "2.0", "id": 99, "method": "tools/call", "params": {
            "name": "bus_reply",
            "arguments": {"author": "red", "thread_id": b["thread_id"],
                          "body": "x", "parent_id": a["message_id"]},
        }}
        resp = self.server.handle_request(self.conn, req)
        self.assertIn("error", resp)
        self.assertIn("belongs to thread", resp["error"]["message"])

    # ------- US1 cont: list filters -------

    def test_list_threads_filters_by_audience(self) -> None:
        _call(self.server, self.conn, "bus_post_thread",
              author="red", title="public", body="x")
        _call(self.server, self.conn, "bus_post_thread",
              author="red", title="hermes-only", body="x", audience="hermes")
        _call(self.server, self.conn, "bus_post_thread",
              author="red", title="red-and-hermes", body="x", audience="red,hermes")
        # clawta sees only the public one (NULL audience).
        clawta_view = _call(self.server, self.conn, "bus_list_threads",
                            audience="clawta")
        titles = {t["title"] for t in clawta_view["threads"]}
        self.assertEqual(titles, {"public"})
        # hermes sees public + the two it's named in.
        hermes_view = _call(self.server, self.conn, "bus_list_threads",
                            audience="hermes")
        titles = {t["title"] for t in hermes_view["threads"]}
        self.assertEqual(titles, {"public", "hermes-only", "red-and-hermes"})

    def test_list_threads_filters_by_board(self) -> None:
        _call(self.server, self.conn, "bus_post_thread",
              author="red", title="c", body="x", board="chitin")
        _call(self.server, self.conn, "bus_post_thread",
              author="red", title="r", body="x", board="readybench")
        out = _call(self.server, self.conn, "bus_list_threads", board="chitin")
        self.assertEqual([t["title"] for t in out["threads"]], ["c"])

    # ------- US2: inbox + mark_read -------

    def test_inbox_excludes_own_posts_and_already_read(self) -> None:
        # red posts; hermes replies; red's inbox should show hermes's reply only,
        # not red's own thread-opener.
        post = _call(self.server, self.conn, "bus_post_thread",
                     author="red", title="t", body="from red", audience="red,hermes")
        _call(self.server, self.conn, "bus_reply",
              author="hermes", thread_id=post["thread_id"], body="from hermes")
        inbox = _call(self.server, self.conn, "bus_inbox", agent_id="red")
        bodies = {m["body"] for m in inbox["messages"]}
        self.assertEqual(bodies, {"from hermes"})
        # Mark the hermes message read; inbox should now be empty.
        msg_id = inbox["messages"][0]["id"]
        _call(self.server, self.conn, "bus_mark_read", agent_id="red", message_id=msg_id)
        again = _call(self.server, self.conn, "bus_inbox", agent_id="red")
        self.assertEqual(again["count"], 0)

    def test_inbox_respects_audience_membership(self) -> None:
        _call(self.server, self.conn, "bus_post_thread",
              author="red", title="t", body="for hermes", audience="hermes")
        clawta_inbox = _call(self.server, self.conn, "bus_inbox", agent_id="clawta")
        self.assertEqual(clawta_inbox["count"], 0)
        hermes_inbox = _call(self.server, self.conn, "bus_inbox", agent_id="hermes")
        self.assertEqual(hermes_inbox["count"], 1)

    def test_inbox_star_audience_includes_everyone(self) -> None:
        _call(self.server, self.conn, "bus_post_thread",
              author="red", title="t", body="for all", audience="*")
        for agent in ("hermes", "clawta", "anyone"):
            inbox = _call(self.server, self.conn, "bus_inbox", agent_id=agent)
            self.assertEqual(inbox["count"], 1, f"{agent} should see *-audience message")

    def test_mark_read_is_idempotent(self) -> None:
        post = _call(self.server, self.conn, "bus_post_thread",
                     author="hermes", title="t", body="x")
        for _ in range(3):
            r = _call(self.server, self.conn, "bus_mark_read",
                      agent_id="red", message_id=post["message_id"])
            self.assertTrue(r["ok"])

    # ------- US5: attachments -------

    def test_attach_persists_and_appears_in_read_thread(self) -> None:
        post = _call(self.server, self.conn, "bus_post_thread",
                     author="red", title="agent-bus design", body="see attached")
        att = _call(self.server, self.conn, "bus_attach",
                    thread_id=post["thread_id"], kind="spec",
                    ref=".specify/specs/001-agent-bus/spec.md",
                    display="agent-bus spec")
        self.assertGreater(att["attachment_id"], 0)
        thread = _call(self.server, self.conn, "bus_read_thread",
                       thread_id=post["thread_id"])
        self.assertEqual(len(thread["attachments"]), 1)
        self.assertEqual(thread["attachments"][0]["kind"], "spec")
        self.assertEqual(thread["attachments"][0]["display"], "agent-bus spec")

    def test_attach_rejects_unknown_kind(self) -> None:
        post = _call(self.server, self.conn, "bus_post_thread",
                     author="red", title="t", body="x")
        req = {"jsonrpc": "2.0", "id": 9, "method": "tools/call", "params": {
            "name": "bus_attach",
            "arguments": {"thread_id": post["thread_id"], "kind": "wat", "ref": "x"},
        }}
        resp = self.server.handle_request(self.conn, req)
        self.assertEqual(resp["error"]["code"], -32602)
        self.assertIn("invalid attachment kind", resp["error"]["message"])

    def test_attach_rejects_unknown_thread(self) -> None:
        req = {"jsonrpc": "2.0", "id": 9, "method": "tools/call", "params": {
            "name": "bus_attach",
            "arguments": {"thread_id": 99999, "kind": "url", "ref": "https://x"},
        }}
        resp = self.server.handle_request(self.conn, req)
        self.assertEqual(resp["error"]["code"], -32602)


if __name__ == "__main__":
    unittest.main()
