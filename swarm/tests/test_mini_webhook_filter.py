from __future__ import annotations

import io
import json
import os
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.mini._internal import webhook as wh


class _FakeResp:
    def __init__(self, status: int = 204):
        self.status = status

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False


class TestResolveWebhookUrl(unittest.TestCase):
    def setUp(self) -> None:
        self._old_primary = os.environ.pop(wh.PRIMARY_ENV, None)
        self._old_swarm = os.environ.pop(wh.SWARM_ENV, None)
        self._old_cid = os.environ.pop(wh.CHANNEL_ID_ENV, None)
        # Strip any pre-existing DISCORD_WEBHOOK_URL_* so the test sees a clean env.
        self._old_chan_urls = {k: os.environ.pop(k) for k in list(os.environ)
                               if k.startswith("DISCORD_WEBHOOK_URL_")}
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.sd = Path(self.tmp.name)
        # Point the hermes-dotenv loader at an absent file so resolve_webhook_url
        # doesn't pick up the operator's real ~/.hermes/.env during tests.
        self._old_hermes_env = os.environ.get(wh.HERMES_ENV_PATH_ENV)
        os.environ[wh.HERMES_ENV_PATH_ENV] = str(self.tmp.name + "/no-such.env")
        wh._hermes_env_cache = None

    def tearDown(self) -> None:
        if self._old_primary is not None:
            os.environ[wh.PRIMARY_ENV] = self._old_primary
        if self._old_swarm is not None:
            os.environ[wh.SWARM_ENV] = self._old_swarm
        if self._old_cid is not None:
            os.environ[wh.CHANNEL_ID_ENV] = self._old_cid
        os.environ.update(self._old_chan_urls)
        if self._old_hermes_env is None:
            os.environ.pop(wh.HERMES_ENV_PATH_ENV, None)
        else:
            os.environ[wh.HERMES_ENV_PATH_ENV] = self._old_hermes_env
        wh._hermes_env_cache = None

    def test_env_var_primary_source(self):
        os.environ[wh.PRIMARY_ENV] = "https://discord.example/webhook"
        self.assertEqual(wh.resolve_webhook_url(self.sd), "https://discord.example/webhook")

    def test_channel_id_fallback_from_hermes_env(self):
        """MINI_DISCORD_CHANNEL_ID=1234 + DISCORD_WEBHOOK_URL_1234=<url>
        should resolve to <url>. This is the hermes/.env convention."""
        os.environ[wh.CHANNEL_ID_ENV] = "1234"
        os.environ["DISCORD_WEBHOOK_URL_1234"] = "https://discord.example/chan1234"
        self.assertEqual(wh.resolve_webhook_url(self.sd), "https://discord.example/chan1234")

    def test_primary_env_beats_channel_id(self):
        os.environ[wh.PRIMARY_ENV] = "https://discord.example/explicit"
        os.environ[wh.CHANNEL_ID_ENV] = "1234"
        os.environ["DISCORD_WEBHOOK_URL_1234"] = "https://discord.example/chan1234"
        self.assertEqual(wh.resolve_webhook_url(self.sd), "https://discord.example/explicit")

    def test_state_dir_fallback(self):
        (self.sd / "webhook.url").write_text("https://discord.example/sessionhook\n")
        self.assertEqual(wh.resolve_webhook_url(self.sd), "https://discord.example/sessionhook")

    def test_no_url_returns_none(self):
        self.assertIsNone(wh.resolve_webhook_url(self.sd))

    def test_swarm_url_env_only(self):
        self.assertIsNone(wh.resolve_swarm_webhook_url())
        os.environ[wh.SWARM_ENV] = "https://discord.example/swarmhook"
        self.assertEqual(wh.resolve_swarm_webhook_url(), "https://discord.example/swarmhook")


class TestPost(unittest.TestCase):
    def test_post_no_url_returns_false(self):
        self.assertFalse(wh.post(None, "hello"))
        self.assertFalse(wh.post("", "hello"))

    def test_post_success_returns_true(self):
        calls = []

        def fake_opener(req, timeout=5):
            calls.append((req.full_url, req.data, dict(req.headers)))
            return _FakeResp(204)

        self.assertTrue(wh.post("https://example/wh", "hello", opener=fake_opener))
        self.assertEqual(len(calls), 1)
        url, data, headers = calls[0]
        self.assertEqual(url, "https://example/wh")
        self.assertEqual(json.loads(data)["content"], "hello")

    def test_post_sends_discord_compatible_user_agent(self):
        """Discord webhooks reject bare Python-urllib UA with HTTP 403.
        Slice 1 shipped without a UA header; this regression test guards
        against that recurring."""
        captured = {}

        def fake_opener(req, timeout=5):
            captured["headers"] = dict(req.headers)
            return _FakeResp(204)

        wh.post("https://example/wh", "hi", opener=fake_opener)
        ua = captured["headers"].get("User-agent") or captured["headers"].get("User-Agent")
        self.assertIsNotNone(ua, "no User-Agent header sent")
        self.assertIn("DiscordBot", ua)

    def test_post_truncates_long_content(self):
        captured = {}

        def fake_opener(req, timeout=5):
            captured["data"] = req.data
            return _FakeResp(204)

        big = "x" * 5000
        wh.post("https://example/wh", big, opener=fake_opener)
        self.assertLessEqual(len(json.loads(captured["data"])["content"]), 1900)

    def test_post_swallows_network_error(self):
        import urllib.error

        def fake_opener(req, timeout=5):
            raise urllib.error.URLError("boom")

        self.assertFalse(wh.post("https://example/wh", "hi", opener=fake_opener))


if __name__ == "__main__":
    unittest.main()
