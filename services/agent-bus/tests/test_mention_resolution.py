"""Tests for @AgentName → <@user_id> mention resolution.

ticket t_a6df2cdc: verifies that discord_push.resolve_mentions replaces
text @AgentName patterns with Discord <@user_id> format and returns the
correct user_ids for allowed_mentions. Also tests that unknown @mentions
pass through unchanged.

Part 2: tests that server._body_mentions_agent detects both text
@AgentName and Discord native <@user_id> patterns (fallback for
listeners).
"""
from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO_ROOT / "services" / "agent-bus"))

import discord_push  # noqa: E402
import server  # noqa: E402


class ResolveMentionsTests(unittest.TestCase):
    """Pure-function tests on discord_push.resolve_mentions."""

    def test_at_clawta_resolves_to_discord_mention(self):
        body = "@Clawta please review"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, "<@1503438472801685565> please review")
        self.assertEqual(uids, ["1503438472801685565"])

    def test_lowercase_clawta_resolves(self):
        body = "@clawta ping"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, "<@1503438472801685565> ping")

    def test_at_ares_resolves(self):
        body = "@Ares heads up"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, "<@150343848646685258> heads up")
        self.assertEqual(uids, ["150343848646685258"])

    def test_at_hermes_resolves_to_same_id_as_ares(self):
        """Ares and hermes share the same bot identity."""
        body = "@hermes check this"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, "<@150343848646685258> check this")
        self.assertEqual(uids, ["150343848646685258"])

    def test_multiple_mentions_in_one_body(self):
        body = "@clawta and @ARES please"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(
            resolved,
            "<@1503438472801685565> and <@150343848646685258> please",
        )
        self.assertEqual(uids, ["1503438472801685565", "150343848646685258"])

    def test_shared_id_not_duplicated(self):
        """@ares and @hermes resolve to the same ID; it appears once."""
        body = "@ares and @hermes"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(uids, ["150343848646685258"])

    def test_unknown_agent_left_alone(self):
        body = "@randoperson check this"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, body)
        self.assertEqual(uids, [])

    def test_no_agent_without_at_prefix(self):
        """Plain 'clawta' without @ is not a mention."""
        body = "clawta is great"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, body)
        self.assertEqual(uids, [])

    def test_email_address_left_alone(self):
        body = "send to email@clawta.com"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, body)
        self.assertEqual(uids, [])

    def test_hyphenated_token_left_alone(self):
        body = "the @clawta-poller cron just fired"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, body)
        self.assertEqual(uids, [])

    def test_already_discord_mention_not_double_resolved(self):
        """If content already contains <@id>, we must not break it."""
        body = "<@1503438472801685565> ping"
        resolved, uids = discord_push.resolve_mentions(body)
        # The <@...> doesn't match the @AgentName regex, so it passes through
        self.assertEqual(resolved, body)
        self.assertEqual(uids, [])

    def test_no_mentions_passthrough(self):
        body = "PR #800 CI is green now"
        resolved, uids = discord_push.resolve_mentions(body)
        self.assertEqual(resolved, body)
        self.assertEqual(uids, [])


class BodyMentionsAgentTests(unittest.TestCase):
    """Tests for server._body_mentions_agent (fallback listener utility)."""

    def test_text_mention_clawta(self):
        self.assertTrue(server._body_mentions_agent("@Clawta please", "clawta"))

    def test_text_mention_lowercase(self):
        self.assertTrue(server._body_mentions_agent("@clawta hey", "clawta"))

    def test_discord_native_mention_clawta(self):
        self.assertTrue(
            server._body_mentions_agent("<@1503438472801685565> ping", "clawta")
        )

    def test_discord_nickname_mention_clawta(self):
        self.assertTrue(
            server._body_mentions_agent("<@!1503438472801685565> ping", "clawta")
        )

    def test_text_mention_ares(self):
        self.assertTrue(server._body_mentions_agent("@Ares check", "ares"))

    def test_discord_native_mention_ares(self):
        self.assertTrue(
            server._body_mentions_agent("<@150343848646685258> check", "ares")
        )

    def test_no_mention_returns_false(self):
        self.assertFalse(server._body_mentions_agent("just a message", "clawta"))

    def test_different_agent_not_matched(self):
        """@clawta does not match agent_id="ares"."""
        self.assertFalse(server._body_mentions_agent("@Clawta review", "ares"))

    def test_agent_without_discord_id_text_mention(self):
        """icarus has no Discord ID but text @icarus should still match."""
        self.assertTrue(server._body_mentions_agent("@icarus hello", "icarus"))

    def test_agent_without_discord_id_native_mention(self):
        """icarus has no Discord ID; <@random_id> won't match it."""
        self.assertFalse(
            server._body_mentions_agent("<@999999999> hello", "icarus")
        )

    def test_email_not_matched(self):
        self.assertFalse(
            server._body_mentions_agent("email@clawta.com", "clawta")
        )

    def test_hyphenated_not_matched(self):
        self.assertFalse(
            server._body_mentions_agent("@clawta-poller fired", "clawta")
        )


class PostViaWebhookMentionPayloadTests(unittest.TestCase):
    """Verify _post_via_webhook includes allowed_mentions when mentions exist."""

    def test_webhook_payload_includes_allowed_mentions(self):
        """When the body contains @AgentName, the webhook payload must
        include allowed_mentions with the resolved user IDs."""
        import json
        from unittest import mock

        captured = {}

        def fake_urlopen(req, timeout=5):
            captured["payload"] = json.loads(req.data.decode("utf-8"))
            # Return a minimal Discord message response
            class FakeResp:
                status = 200
                def read(self):
                    return b'{"id": "123456"}'
                def __enter__(self):
                    return self
                def __exit__(self, *a):
                    pass
            return FakeResp()

        import urllib.request
        with mock.patch.object(urllib.request, "urlopen", fake_urlopen):
            result = discord_push._post_via_webhook(
                "https://discord.com/api/webhooks/test/token",
                author="red",
                body="@Clawta please review this PR",
            )

        payload = captured["payload"]
        self.assertIn("allowed_mentions", payload)
        self.assertIn("1503438472801685565", payload["allowed_mentions"]["users"])
        # Content should have the <@id> form
        self.assertIn("<@1503438472801685565>", payload["content"])

    def test_webhook_payload_no_mentions_no_allowed_mentions(self):
        """When the body has no @AgentName, allowed_mentions should not
        be in the payload."""
        import json
        from unittest import mock

        captured = {}

        def fake_urlopen(req, timeout=5):
            captured["payload"] = json.loads(req.data.decode("utf-8"))
            class FakeResp:
                status = 200
                def read(self):
                    return b'{"id": "123456"}'
                def __enter__(self):
                    return self
                def __exit__(self, *a):
                    pass
            return FakeResp()

        import urllib.request
        with mock.patch.object(urllib.request, "urlopen", fake_urlopen):
            result = discord_push._post_via_webhook(
                "https://discord.com/api/webhooks/test/token",
                author="red",
                body="No mentions here",
            )

        payload = captured["payload"]
        self.assertNotIn("allowed_mentions", payload)


if __name__ == "__main__":
    unittest.main()