"""Regression tests for swarm/bin/clawta-mention-listener.

Required by Clawta PR #776 review (msg 5424): "this is exactly the
routing surface that just failed in prod, so regex/prompt invariants
should be locked before approval."

Invariants covered:
  - _addressed_to_clawta matches text @clawta (any case)
  - _addressed_to_clawta matches Discord native <@id>
  - _addressed_to_clawta matches leading "Clawta, ..." direct address
  - _addressed_to_clawta matches peer-review phrasing (LGTM/REQUEST_CHANGES)
  - _addressed_to_clawta rejects self-author (Clawta replying to herself)
  - _addressed_to_clawta rejects @clawta-poller (different identifier)
  - _addressed_to_clawta rejects email@clawta.com (URL/email lookbehind)
  - _addressed_to_clawta uses audience-field membership when present
  - _addressed_to_clawta suppresses phrasing patterns on mostly-quoted bodies
  - _build_prompt includes channel/thread id, author, exact body,
    and the response contract (REVIEW/REQUEST_CHANGES/COMMENT)
"""

from __future__ import annotations

import importlib.util
import sys
import unittest
from pathlib import Path
from types import SimpleNamespace

REPO = Path(__file__).resolve().parents[2]
LISTENER_PATH = REPO / "swarm" / "bin" / "clawta-mention-listener"

# Load the listener as a module despite its missing .py suffix —
# spec_from_file_location returns None for unrecognized extensions, so
# we attach a SourceFileLoader explicitly.
from importlib.machinery import SourceFileLoader
spec = importlib.util.spec_from_loader(
    "clawta_mention_listener",
    SourceFileLoader("clawta_mention_listener", str(LISTENER_PATH)),
)
listener = importlib.util.module_from_spec(spec)
spec.loader.exec_module(listener)


def _row(body: str = "", author: str = "red", audience: str = "",
         thread_id: int = 6, msg_id: int = 9999, title: str = "#clawta",
         created_at: int = 1779150000):
    """Build a fake sqlite3.Row-like object for testing.

    sqlite3.Row supports both [key] indexing and .keys(); the listener
    only uses [key], so a plain dict is sufficient.
    """
    return {
        "body": body, "author": author, "audience": audience,
        "thread_id": thread_id, "id": msg_id, "thread_title": title,
        "created_at": created_at,
    }


class AddressedToClawtaTests(unittest.TestCase):

    def test_lowercase_at_clawta_matches(self):
        self.assertTrue(listener._addressed_to_clawta(_row("@clawta please review")))

    def test_capital_at_clawta_matches(self):
        self.assertTrue(listener._addressed_to_clawta(_row("@Clawta please review")))

    def test_discord_native_mention_matches(self):
        self.assertTrue(listener._addressed_to_clawta(_row("<@1503438472801685565> what's up")))

    def test_discord_nickname_mention_matches(self):
        """`<@!id>` is the Discord 'nickname mention' variant — must match too."""
        self.assertTrue(listener._addressed_to_clawta(_row("<@!1503438472801685565> ping")))

    def test_leading_direct_address_matches(self):
        self.assertTrue(listener._addressed_to_clawta(
            _row("Clawta, can you check this for me?")))

    def test_leading_address_with_em_dash_matches(self):
        self.assertTrue(listener._addressed_to_clawta(
            _row("Clawta — your verdict on PR #774?")))

    def test_peer_review_phrasing_matches(self):
        self.assertTrue(listener._addressed_to_clawta(
            _row("REQUEST_CHANGES on the regex boundary edge case")))

    def test_lgtm_phrasing_matches(self):
        self.assertTrue(listener._addressed_to_clawta(_row("LGTM from my side")))

    def test_audience_field_match(self):
        self.assertTrue(listener._addressed_to_clawta(
            _row("a generic status update", audience="red,clawta,hermes")))

    def test_self_author_excluded(self):
        """Clawta replying to herself must not re-trigger."""
        self.assertFalse(listener._addressed_to_clawta(
            _row("@Clawta here is my reply", author="Clawta")))

    def test_at_clawta_poller_does_not_match(self):
        """`@clawta-poller` is a different identifier (hyphenated)."""
        self.assertFalse(listener._addressed_to_clawta(
            _row("the @clawta-poller cron just fired again")))

    def test_email_at_clawta_does_not_match(self):
        """`email@clawta.com` is not a Discord mention."""
        self.assertFalse(listener._addressed_to_clawta(
            _row("send a note to email@clawta.com")))

    def test_mostly_quoted_body_suppresses_phrasing(self):
        """Quoted text from old peer-review must NOT re-trigger.
        50%+ quote-prefix lines → only explicit @-mentions count."""
        body = "\n".join([
            "> old peer review request below",
            "> please post REQUEST_CHANGES if needed",
            "> LGTM means approve",
            "just forwarding for reference",
        ])
        self.assertFalse(listener._addressed_to_clawta(_row(body)))

    def test_mostly_quoted_body_still_matches_explicit_mention(self):
        """Even on a mostly-quoted body, an explicit @Clawta still counts."""
        body = "\n".join([
            "> old request",
            "> some quote",
            "> more quoted text",
            "@Clawta heads up on the above",
        ])
        self.assertTrue(listener._addressed_to_clawta(_row(body)))


class BuildPromptTests(unittest.TestCase):

    def test_includes_thread_id(self):
        p = listener._build_prompt(_row("hello", thread_id=42))
        self.assertIn("thread_id: 42", p)

    def test_includes_thread_title(self):
        p = listener._build_prompt(_row("x", title="#swarm"))
        self.assertIn("'#swarm'", p)

    def test_includes_message_id(self):
        p = listener._build_prompt(_row("x", msg_id=12345))
        self.assertIn("message_id: 12345", p)

    def test_includes_author(self):
        p = listener._build_prompt(_row("x", author="Ares"))
        self.assertIn("author: Ares", p)

    def test_includes_exact_body(self):
        body = "Please review PR #999. Specific ask: regex boundary."
        p = listener._build_prompt(_row(body))
        self.assertIn(body, p)

    def test_includes_verdict_contract(self):
        p = listener._build_prompt(_row("x"))
        self.assertIn("REVIEW (LGTM)", p)
        self.assertIn("REQUEST_CHANGES", p)
        self.assertIn("COMMENT", p)

    def test_includes_word_cap(self):
        p = listener._build_prompt(_row("x"))
        self.assertIn("500 words", p)

    def test_forbids_prompt_echo(self):
        p = listener._build_prompt(_row("x"))
        self.assertIn("Do not echo this prompt", p)


if __name__ == "__main__":
    unittest.main()
