"""Tests for discord_mirror.py. All HTTP is mocked via the http_request
chokepoint — no real network calls.
"""
from __future__ import annotations

import importlib.util
import json
import os
import sqlite3
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SVC_DIR = Path(__file__).resolve().parents[1]
SCHEMA_SQL = SVC_DIR / "schema.sql"


def _load(name: str):
    if str(SVC_DIR) not in sys.path:
        sys.path.insert(0, str(SVC_DIR))
    if name in sys.modules:
        del sys.modules[name]
    return importlib.import_module(name)


def _seed_db(path: Path) -> sqlite3.Connection:
    path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(path))
    conn.executescript(SCHEMA_SQL.read_text())
    conn.commit()
    return conn


class DiscordCursorTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.db = Path(self.tmp.name) / "bus.db"
        self.conn = _seed_db(self.db)
        self.mod = _load("discord_mirror")
        self.mod.ensure_cursor_table(self.conn)

    def tearDown(self) -> None:
        self.conn.close()
        self.tmp.cleanup()

    def test_cursor_roundtrip(self) -> None:
        self.assertIsNone(self.mod.get_cursor(self.conn, "ch1"))
        self.mod.set_cursor(self.conn, "ch1", "111")
        self.assertEqual(self.mod.get_cursor(self.conn, "ch1"), "111")
        self.mod.set_cursor(self.conn, "ch1", "222")  # upsert
        self.assertEqual(self.mod.get_cursor(self.conn, "ch1"), "222")
        # Other channel unaffected
        self.mod.set_cursor(self.conn, "ch2", "999")
        self.assertEqual(self.mod.get_cursor(self.conn, "ch1"), "222")
        self.assertEqual(self.mod.get_cursor(self.conn, "ch2"), "999")


class DiscordOutboundTests(unittest.TestCase):
    def setUp(self) -> None:
        self.mod = _load("discord_mirror")

    def test_post_to_webhook_includes_username_and_payload(self) -> None:
        captured = {}
        def fake_http(url, **kw):
            captured["url"] = url
            captured["body"] = json.loads(kw["body"])
            captured["headers"] = kw["headers"]
            return 204, b""
        with mock.patch.object(self.mod, "http_request", fake_http):
            self.mod.post_to_discord_webhook(
                "https://discord.com/api/webhooks/123/abc",
                content="hello bus", username="agent-bus",
            )
        self.assertEqual(captured["body"], {"content": "hello bus", "username": "agent-bus"})
        self.assertEqual(captured["headers"]["Content-Type"], "application/json")

    def test_post_to_webhook_truncates_at_2000(self) -> None:
        captured = {}
        def fake_http(url, **kw):
            captured["body"] = json.loads(kw["body"])
            return 204, b""
        with mock.patch.object(self.mod, "http_request", fake_http):
            self.mod.post_to_discord_webhook(
                "https://x", content="x" * 5000,
            )
        # 1990 + "\n(…continued)" suffix
        self.assertTrue(captured["body"]["content"].endswith("(…continued)"))
        self.assertLessEqual(len(captured["body"]["content"]), 2010)

    def test_post_to_webhook_raises_on_4xx(self) -> None:
        with mock.patch.object(self.mod, "http_request",
                                lambda *a, **k: (400, b"bad")):
            with self.assertRaisesRegex(RuntimeError, "discord webhook POST failed: 400"):
                self.mod.post_to_discord_webhook("https://x", content="y")


class DiscordInboundTests(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.db = Path(self.tmp.name) / "bus.db"
        self.conn = _seed_db(self.db)
        self.mod = _load("discord_mirror")
        self.mod.ensure_cursor_table(self.conn)

    def tearDown(self) -> None:
        self.conn.close()
        self.tmp.cleanup()

    def _fake_msgs(self, ids: list[str], author: str = "@hermes",
                   text: str = "ping") -> list[dict]:
        # Snowflake doesn't matter for ordering tests as long as ids are
        # monotonic. The encoded timestamp is computed in the ingester;
        # using small numbers means epoch = (id>>22)+1420070400 (Discord
        # epoch ~ Jan 2015).
        return [{"id": i, "content": f"{text} {i}",
                 "author": {"username": author}} for i in ids]

    def test_poll_once_creates_mirror_thread_and_ingests(self) -> None:
        def fake_fetch(*a, **kw):
            return self._fake_msgs(["10", "11", "12"])
        with mock.patch.object(self.mod, "fetch_new_messages", fake_fetch):
            result = self.mod.poll_once(self.conn, channel_id="C1",
                                         token="t", thread_title="ChanMirror")
        self.assertEqual(result["ingested"], 3)
        self.assertEqual(result["cursor"], "12")
        # Mirror thread exists with the right discord_thread_id
        row = self.conn.execute(
            "SELECT id, title, discord_thread_id FROM threads WHERE discord_thread_id='C1'"
        ).fetchone()
        self.assertIsNotNone(row)
        self.assertEqual(row[1], "ChanMirror")
        # 3 messages ingested with discord_message_id set
        msgs = self.conn.execute(
            "SELECT discord_message_id, author, body FROM messages WHERE thread_id=? ORDER BY id",
            (row[0],),
        ).fetchall()
        self.assertEqual([m[0] for m in msgs], ["10", "11", "12"])
        self.assertEqual(msgs[0][1], "@hermes")

    def test_poll_idempotent_on_replay(self) -> None:
        with mock.patch.object(self.mod, "fetch_new_messages",
                                lambda *a, **kw: self._fake_msgs(["10", "11"])):
            self.mod.poll_once(self.conn, channel_id="C2", token="t",
                                thread_title="x")
        # Second call with the same messages — should not duplicate
        with mock.patch.object(self.mod, "fetch_new_messages",
                                lambda *a, **kw: self._fake_msgs(["10", "11"])):
            r = self.mod.poll_once(self.conn, channel_id="C2", token="t",
                                    thread_title="x")
        self.assertEqual(r["ingested"], 0)
        count = self.conn.execute(
            "SELECT COUNT(*) FROM messages "
            "WHERE thread_id=(SELECT id FROM threads WHERE discord_thread_id='C2')"
        ).fetchone()[0]
        self.assertEqual(count, 2)

    def test_poll_uses_cursor_for_subsequent_calls(self) -> None:
        captured = {}
        def fake_fetch(channel_id, *, token, after, limit=50):
            captured["after"] = after
            return self._fake_msgs(["10"])
        with mock.patch.object(self.mod, "fetch_new_messages", fake_fetch):
            self.mod.poll_once(self.conn, channel_id="C3", token="t",
                                thread_title="x")
        # First call: no cursor
        self.assertIsNone(captured["after"])
        # Second call should pass the cursor
        with mock.patch.object(self.mod, "fetch_new_messages",
                                lambda *a, **kw: (captured.update(after=kw.get("after")) or [])):
            self.mod.poll_once(self.conn, channel_id="C3", token="t",
                                thread_title="x")
        self.assertEqual(captured["after"], "10")

    def test_poll_handles_empty_message_with_attachment_stub(self) -> None:
        msgs = [{"id": "100", "content": "",
                 "author": {"username": "u"},
                 "attachments": [{}, {}], "embeds": []}]
        with mock.patch.object(self.mod, "fetch_new_messages",
                                lambda *a, **kw: msgs):
            self.mod.poll_once(self.conn, channel_id="C4", token="t",
                                thread_title="x")
        body = self.conn.execute(
            "SELECT body FROM messages WHERE discord_message_id='100'"
        ).fetchone()[0]
        self.assertIn("attachment x2", body)


if __name__ == "__main__":
    unittest.main()
