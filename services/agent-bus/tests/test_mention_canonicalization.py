"""Invariant tests for @-mention canonicalization in bus_post_thread + bus_reply.

Operator pain quote (2026-05-18) motivating these tests:
  "This should be baked into our mcp sever or something because I am
  constantly reminding you of this" — re: agents typing `@clawta`
  (lowercase) when Discord requires `@Clawta` (capital C).

Invariant: for every agent in server._CANONICAL_MENTIONS, any case
variant of `@<name>` in a bus body is rewritten to canonical case
before write + Discord push. Non-mention text (URLs, identifiers,
substring matches) is left untouched.
"""

from __future__ import annotations

import os
import sys
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT / "services" / "agent-bus"))

import server  # noqa: E402
from db import connect  # noqa: E402


class CanonicalizeMentionsTests(unittest.TestCase):
    """Pure-function tests on _canonicalize_mentions — no DB involved."""

    def test_lowercase_clawta_becomes_capital_C(self):
        out = server._canonicalize_mentions("@clawta please review")
        self.assertEqual(out, "@Clawta please review")

    def test_uppercase_clawta_normalizes_too(self):
        out = server._canonicalize_mentions("@CLAWTA review please")
        self.assertEqual(out, "@Clawta review please")

    def test_mixed_case_clawta_normalizes(self):
        out = server._canonicalize_mentions("@ClAwTa look at this")
        self.assertEqual(out, "@Clawta look at this")

    def test_ares_stays_lowercase(self):
        out = server._canonicalize_mentions("@Ares heads up")
        self.assertEqual(out, "@ares heads up")

    def test_multiple_mentions_in_one_body(self):
        out = server._canonicalize_mentions("@clawta and @ARES please")
        self.assertEqual(out, "@Clawta and @ares please")

    def test_mention_in_middle_of_sentence(self):
        body = "ping @clawta on this then check with @hermes"
        out = server._canonicalize_mentions(body)
        self.assertEqual(out, "ping @Clawta on this then check with @hermes")

    def test_email_address_left_alone(self):
        """`email@clawta.com` is NOT a Discord mention — must not rewrite."""
        body = "send to email@clawta.com"
        out = server._canonicalize_mentions(body)
        self.assertEqual(out, body)

    def test_hyphenated_token_left_alone(self):
        """`@clawta-poller` is a different identifier, not the @Clawta mention."""
        body = "the @clawta-poller cron just fired"
        out = server._canonicalize_mentions(body)
        self.assertEqual(out, body)

    def test_unknown_agent_left_alone(self):
        """`@randoperson` is not in _CANONICAL_MENTIONS — leave it."""
        body = "@randoperson check this"
        out = server._canonicalize_mentions(body)
        self.assertEqual(out, body)

    def test_url_with_at_sign_left_alone(self):
        body = "see https://example.com/users/clawta@v2"
        out = server._canonicalize_mentions(body)
        self.assertEqual(out, body)

    def test_no_mentions_passthrough(self):
        body = "PR #774 CI is green now"
        self.assertEqual(server._canonicalize_mentions(body), body)


class BusReplyCanonicalizationTests(unittest.TestCase):
    """End-to-end: bus_reply writes the canonicalized body to the DB."""

    def setUp(self):
        self.tmp = tempfile.NamedTemporaryFile(suffix=".db", delete=False)
        self.tmp.close()
        os.environ["AGENT_BUS_DB"] = self.tmp.name
        self.conn = connect()
        cur = self.conn.cursor()
        cur.execute(
            "INSERT INTO threads(title, author, created_at, updated_at) "
            "VALUES('test', 'red', 0, 0)"
        )
        self.thread_id = cur.lastrowid
        self.conn.commit()

    def tearDown(self):
        self.conn.close()
        os.unlink(self.tmp.name)
        os.environ.pop("AGENT_BUS_DB", None)

    def test_bus_reply_canonicalizes_lowercase_clawta(self):
        server.bus_reply(
            self.conn, author="red", thread_id=self.thread_id,
            body="@clawta please review PR #773",
        )
        row = self.conn.execute(
            "SELECT body FROM messages WHERE thread_id=?", (self.thread_id,)
        ).fetchone()
        self.assertEqual(row["body"], "@Clawta please review PR #773")

    def test_bus_post_thread_canonicalizes(self):
        result = server.bus_post_thread(
            self.conn, author="red", title="t",
            body="@CLAWTA + @ARES — review",
        )
        row = self.conn.execute(
            "SELECT body FROM messages WHERE thread_id=?",
            (result["thread_id"],),
        ).fetchone()
        self.assertEqual(row["body"], "@Clawta + @ares — review")


if __name__ == "__main__":
    unittest.main()
