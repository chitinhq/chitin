"""Spec 023 R1 — outbound env refresh.

# spec: 023-agent-bus-bidirectional-liveness

Verifies discord_push.try_push picks up env changes without daemon
restart. Brings forward the tests originally specced in 021.
"""
from __future__ import annotations

import importlib
import json
import os
import sys
import time
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))


class DiscordPushEnvRefreshTests(unittest.TestCase):
    def setUp(self):
        # Fresh import + isolate the ENV file path to a tmpdir-scoped one.
        self.tmpdir = Path(self._tmp())
        self.env = self.tmpdir / ".env"
        self._import_with_env(self.env)

    def tearDown(self):
        # Best-effort cleanup; the tmpdir is process-local.
        for p in [self.env]:
            if p.exists():
                p.unlink()

    def _tmp(self) -> str:
        import tempfile
        return tempfile.mkdtemp(prefix="agbus-push-test-")

    def _import_with_env(self, env_path: Path):
        """Import discord_push with ENV_PATH patched to the tmp env."""
        if "agent-bus.discord_push" in sys.modules:
            del sys.modules["agent-bus.discord_push"]
        # The module is loaded as a top-level discord_push since
        # services/agent-bus is on sys.path
        if "discord_push" in sys.modules:
            del sys.modules["discord_push"]
        sys.path.insert(0, str(ROOT / "services" / "agent-bus"))
        import discord_push
        discord_push.ENV_PATH = env_path
        # Reset module globals so previous test state doesn't leak.
        discord_push._ENV_MTIME = 0.0
        discord_push._BOT_TOKEN = ""
        discord_push._WEBHOOKS_BY_ID = {}
        discord_push._WEBHOOKS_BY_NAME = {}
        self.discord_push = discord_push

    def _write_env(self, lines: list[str]):
        self.env.write_text("\n".join(lines) + "\n")
        # Force mtime tick — same-second writes can collide.
        future = time.time() + 1
        os.utime(self.env, (future, future))

    def test_env_addition_picked_up_without_restart(self):
        """AC1: webhook URL added to .env after import is read on next push."""
        # Initial state: .env exists but has no webhook URL
        self._write_env(["FOO=bar"])
        with mock.patch.object(self.discord_push, "_post_via_webhook") as p:
            p.return_value = True
            self.discord_push.try_push(channel_id="999", author="red", body="hi")
            # No webhook → not called via webhook path
            p.assert_not_called()

        # Operator adds the webhook URL
        self._write_env([
            "FOO=bar",
            "DISCORD_WEBHOOK_URL_999=https://discord.com/api/webhooks/x/y",
        ])

        with mock.patch.object(self.discord_push, "_post_via_webhook") as p:
            p.return_value = True
            self.discord_push.try_push(channel_id="999", author="red", body="hi")
            # Now the webhook is in cache → webhook path fires
            p.assert_called_once()
            args, kwargs = p.call_args
            self.assertEqual(args[0], "https://discord.com/api/webhooks/x/y")

    def test_env_removal_invalidates_webhook(self):
        """AC3: removing the URL from .env stops using the cached value."""
        self._write_env([
            "DISCORD_WEBHOOK_URL_888=https://discord.com/api/webhooks/keep",
        ])
        with mock.patch.object(self.discord_push, "_post_via_webhook") as p:
            p.return_value = True
            self.discord_push.try_push(channel_id="888", author="red", body="first")
            p.assert_called_once()

        # Remove the webhook URL
        self._write_env(["FOO=bar"])

        with mock.patch.object(self.discord_push, "_post_via_webhook") as p:
            self.discord_push.try_push(channel_id="888", author="red", body="second")
            p.assert_not_called()

    def test_missing_env_file_does_not_raise(self):
        """AC4: a missing .env keeps existing in-memory state, doesn't raise."""
        # Set the env path to a non-existent path
        self.discord_push.ENV_PATH = self.tmpdir / "does-not-exist.env"
        # Should not raise
        self.discord_push.try_push(channel_id="777", author="red", body="hi")

    def test_push_failure_writes_to_jsonl(self):
        """R5: failures land in ~/.hermes/logs/discord-push-failures.jsonl."""
        log_path = self.tmpdir / "failures.jsonl"
        self.discord_push.FAILURE_LOG_PATH = log_path
        self._write_env([
            "DISCORD_WEBHOOK_URL_555=https://example.invalid/webhook",
        ])
        with mock.patch.object(self.discord_push, "_post_via_webhook") as p:
            p.side_effect = RuntimeError("HTTP 500 from mock")
            # No bot token in env → no fallback path
            self.discord_push.try_push(channel_id="555", author="red", body="hi")
        self.assertTrue(log_path.exists(), "failure log must exist after failed push")
        line = log_path.read_text().strip()
        self.assertIn('"channel_id": "555"', line)
        self.assertIn('"path": "webhook"', line)
        self.assertIn("HTTP 500", line)


if __name__ == "__main__":
    unittest.main()
