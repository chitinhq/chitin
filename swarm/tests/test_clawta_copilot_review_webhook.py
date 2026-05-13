#!/usr/bin/env python3
"""Unit tests for the Copilot review webhook receiver."""

from __future__ import annotations

import hashlib
import hmac
import importlib.machinery
import importlib.util
import json
import threading
import unittest
from http.client import HTTPConnection
from http.server import ThreadingHTTPServer
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "bin" / "clawta-copilot-review-webhook"


def load_module():
    loader = importlib.machinery.SourceFileLoader("clawta_copilot_review_webhook", str(SCRIPT))
    spec = importlib.util.spec_from_loader("clawta_copilot_review_webhook", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class CopilotReviewWebhookTests(unittest.TestCase):
    def test_verify_signature_accepts_sha256_hmac(self):
        module = load_module()
        body = b'{"ok":true}'
        digest = hmac.new(b"secret", body, hashlib.sha256).hexdigest()

        self.assertTrue(module.verify_signature(body, f"sha256={digest}", "secret"))
        self.assertFalse(module.verify_signature(body, "sha256=bad", "secret"))

    def test_non_copilot_review_is_ignored(self):
        module = load_module()
        payload = {
            "review": {"user": {"login": "human-reviewer"}, "body": "Severity: high - bad"},
            "pull_request": {"number": 12, "head": {"ref": "swarm/codex-084e79e0"}, "html_url": "https://example/pr/12"},
        }

        result = module.process_event("pull_request_review", payload, dry_run=True)

        self.assertEqual(result["status"], "ignored")
        self.assertEqual(result["reason"], "non-copilot-author")

    def test_copilot_review_posts_comment_and_creates_medium_followup(self):
        module = load_module()
        payload = {
            "review": {
                "user": {"login": "github-copilot[bot]", "type": "Bot"},
                "body": "**Findings**\n- Severity: medium - apps/cli/src/main.ts - validate webhook auth\n- Severity: low - docs - typo",
            },
            "pull_request": {
                "number": 88,
                "title": "feat: add webhook",
                "body": "Closes ticket t_084e79e0",
                "head": {"ref": "swarm/codex-084e79e0"},
                "html_url": "https://github.test/chitinhq/chitin/pull/88",
            },
            "repository": {"full_name": "chitinhq/chitin"},
            "delivery": "delivery-1",
        }
        calls: list[list[str]] = []

        def fake_run(cmd, **kwargs):
            calls.append(cmd)
            return mock.Mock(returncode=0, stdout='{"id":"t_followup"}', stderr="")

        with mock.patch.object(module, "run", side_effect=fake_run):
            result = module.process_event("pull_request_review", payload, dry_run=False, delivery_id="delivery-1")

        self.assertEqual(result["status"], "processed")
        self.assertEqual(result["ticket"], "t_084e79e0")
        self.assertEqual(result["findings"], 2)
        self.assertEqual(result["followups"], 1)
        self.assertIn(["hermes", "kanban", "--board", "chitin", "comment", "t_084e79e0", "--author", "copilot-review-webhook", mock.ANY], calls)
        create_calls = [cmd for cmd in calls if cmd[:5] == ["hermes", "kanban", "--board", "chitin", "create"]]
        self.assertEqual(len(create_calls), 1)
        self.assertIn("--idempotency-key", create_calls[0])
        self.assertIn("--parent", create_calls[0])
        self.assertIn("t_084e79e0", create_calls[0])

    def test_review_comment_uses_comment_author_and_location(self):
        module = load_module()
        payload = {
            "comment": {
                "user": {"login": "copilot-pull-request-reviewer[bot]", "type": "Bot"},
                "body": "Severity: high - Missing bounds check.",
                "path": "go/execution-kernel/internal/gov/gate.go",
                "line": 42,
                "html_url": "https://github.test/comment",
            },
            "pull_request": {"number": 7, "head": {"ref": "swarm/codex-084e79e0"}, "html_url": "https://github.test/pr/7"},
        }

        result = module.process_event("pull_request_review_comment", payload, dry_run=True, delivery_id="d2")

        self.assertEqual(result["status"], "processed")
        self.assertEqual(result["ticket"], "t_084e79e0")
        self.assertEqual(result["findings"], 1)
        self.assertEqual(result["followups"], 1)

    def test_http_endpoint_authenticates_and_ignores_bad_signature(self):
        module = load_module()
        secret = "webhook-secret"
        server = ThreadingHTTPServer(("127.0.0.1", 0), module.WebhookHandler)
        server.webhook_path = "/webhooks/github/copilot-reviews"
        server.webhook_secret = secret
        server.dry_run = True
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)

        body = json.dumps({
            "review": {"user": {"login": "github-copilot[bot]"}, "body": "No blocking issues found."},
            "pull_request": {"number": 9, "head": {"ref": "swarm/codex-084e79e0"}},
        }).encode()
        conn = HTTPConnection("127.0.0.1", server.server_port, timeout=5)
        conn.request(
            "POST",
            "/webhooks/github/copilot-reviews",
            body=body,
            headers={
                "X-GitHub-Event": "pull_request_review",
                "X-Hub-Signature-256": "sha256=bad",
                "Content-Type": "application/json",
            },
        )
        response = conn.getresponse()
        response.read()
        conn.close()

        self.assertEqual(response.status, 401)

    def test_http_endpoint_accepts_signed_copilot_review(self):
        module = load_module()
        secret = "webhook-secret"
        server = ThreadingHTTPServer(("127.0.0.1", 0), module.WebhookHandler)
        server.webhook_path = "/webhooks/github/copilot-reviews"
        server.webhook_secret = secret
        server.dry_run = True
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)

        body = json.dumps({
            "review": {"user": {"login": "github-copilot[bot]"}, "body": "Severity: medium - Add a regression test."},
            "pull_request": {
                "number": 10,
                "head": {"ref": "swarm/codex-084e79e0"},
                "html_url": "https://github.test/pr/10",
            },
        }).encode()
        digest = hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
        conn = HTTPConnection("127.0.0.1", server.server_port, timeout=5)
        conn.request(
            "POST",
            "/webhooks/github/copilot-reviews",
            body=body,
            headers={
                "X-GitHub-Event": "pull_request_review",
                "X-GitHub-Delivery": "delivery-good",
                "X-Hub-Signature-256": f"sha256={digest}",
                "Content-Type": "application/json",
            },
        )
        response = conn.getresponse()
        response_body = json.loads(response.read().decode())
        conn.close()

        self.assertEqual(response.status, 200)
        self.assertEqual(response_body["status"], "processed")
        self.assertEqual(response_body["ticket"], "t_084e79e0")


if __name__ == "__main__":
    unittest.main()
