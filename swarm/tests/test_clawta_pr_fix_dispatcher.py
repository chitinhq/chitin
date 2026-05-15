#!/usr/bin/env python3
"""Unit tests for clawta-pr-fix-dispatcher helper logic."""

from __future__ import annotations

import importlib.machinery
import importlib.util
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "bin" / "clawta-pr-fix-dispatcher"


def load_module():
    loader = importlib.machinery.SourceFileLoader("clawta_pr_fix_dispatcher", str(SCRIPT))
    spec = importlib.util.spec_from_loader("clawta_pr_fix_dispatcher", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


class FixDispatcherTests(unittest.TestCase):
    def test_latest_review_comment_ignores_untrusted_marker_author(self):
        module = load_module()
        untrusted = {
            "user": {"login": "random-contributor"},
            "body": "<!-- clawta-reviewer:v1 head=badc0ffee -->\n**Verdict:** REQUEST_CHANGES",
        }
        trusted = {
            "user": {"login": "jpleva91"},
            "body": "<!-- clawta-reviewer:v1 head=abc123 -->\n**Verdict:** REQUEST_CHANGES",
        }

        self.assertIsNone(module.latest_review_comment([untrusted]))
        self.assertEqual(module.latest_review_comment([untrusted, trusted]), trusted["body"])

    def test_already_dispatched_is_head_aware(self):
        module = load_module()
        comments = [
            {"body": "<!-- clawta-fix-dispatcher:v1 -->\nlegacy marker"},
            {"body": "<!-- clawta-fix-dispatcher:v1 head=abc123 -->\nold head"},
            {"body": "<!-- clawta-fix-dispatcher:v1 head=def456 -->\ncurrent head"},
        ]

        self.assertTrue(module.already_dispatched(comments, "def456"))
        self.assertFalse(module.already_dispatched(comments, "ffff00"))
        self.assertFalse(module.already_dispatched(comments, ""))

    def test_mark_dispatched_includes_head_marker(self):
        module = load_module()
        seen = []

        def fake_run(cmd, **kwargs):
            seen.append(cmd)
            return mock.Mock(returncode=0, stdout="", stderr="")

        pr = {"number": 77, "headRefOid": "abc123", "headRefName": "swarm/codex-abc", "title": "x"}
        with mock.patch.object(module, "run", side_effect=fake_run):
            module.mark_dispatched(pr, "/tmp/log", dry_run=False)

        self.assertIn("<!-- clawta-fix-dispatcher:v1 head=abc123 -->", seen[0][-1])

    def test_ensure_worktree_creates_local_branch_from_remote_branch(self):
        module = load_module()
        calls: list[list[str]] = []

        def fake_run(cmd, **kwargs):
            calls.append(cmd)
            return mock.Mock(returncode=0, stdout="", stderr="")

        with tempfile.TemporaryDirectory() as tmp:
            module.WORKTREE_ROOT = Path(tmp)
            with mock.patch.object(module, "existing_worktree_for_branch", return_value=None), mock.patch.object(module, "run", side_effect=fake_run):
                wt = module.ensure_worktree("clawta/pr-fix-dispatcher-v2", 542)

        self.assertEqual(wt.name, "pr-542")
        self.assertIn(
            [
                "git",
                "fetch",
                "origin",
                "clawta/pr-fix-dispatcher-v2:refs/remotes/origin/clawta/pr-fix-dispatcher-v2",
            ],
            calls,
        )
        self.assertIn(
            [
                "git",
                "worktree",
                "add",
                "-B",
                "clawta/pr-fix-dispatcher-v2",
                str(wt),
                "origin/clawta/pr-fix-dispatcher-v2",
            ],
            calls,
        )

    def test_dispatch_worker_pipes_prompt_over_stdin_instead_of_argv(self):
        module = load_module()
        popen_calls = []
        stdin_payloads = []

        class FakePopen:
            def __init__(self, cmd, **kwargs):
                popen_calls.append((cmd, kwargs))
                handle = kwargs["stdin"]
                handle.seek(0)
                stdin_payloads.append(handle.read())
                self.pid = 4321

        pr = {"number": 77, "title": "Fix lint", "headRefName": "swarm/codex-fix", "url": "https://example/pr/77"}
        review = "Review body with actionable changes."

        with tempfile.TemporaryDirectory() as tmp:
            worktree = Path(tmp) / "pr-77"
            worktree.mkdir()
            with mock.patch.object(module, "WORKTREE_ROOT", Path(tmp)), \
                 mock.patch.object(module, "ensure_worktree", return_value=worktree), \
                 mock.patch.object(module.subprocess, "Popen", FakePopen):
                module.dispatch_worker(pr, review, dry_run=False)

        self.assertEqual(len(popen_calls), 1)
        cmd, kwargs = popen_calls[0]
        self.assertEqual(
            cmd,
            ["codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--model", module.CODEX_MODEL],
        )
        self.assertNotIn(review, cmd)
        stdin_text = stdin_payloads[0]
        self.assertIn("Review comment:", stdin_text)
        self.assertIn(review, stdin_text)

    def test_dispatch_worker_cleans_up_prompt_file_when_popen_errors(self):
        # Boundary: error. If Popen raises, the temp prompt file — which holds
        # the review/ticket body — must still be removed, not leaked to disk.
        module = load_module()
        created_files = []
        real_named_tmp = tempfile.NamedTemporaryFile

        def recording_named_tmp(*args, **kwargs):
            handle = real_named_tmp(*args, **kwargs)
            created_files.append(handle.name)
            return handle

        pr = {"number": 88, "title": "x", "headRefName": "swarm/codex-fix", "url": "https://example/pr/88"}
        review = "Review body."

        with tempfile.TemporaryDirectory() as tmp:
            worktree = Path(tmp) / "pr-88"
            worktree.mkdir()
            with mock.patch.object(module, "WORKTREE_ROOT", Path(tmp)), \
                 mock.patch.object(module, "ensure_worktree", return_value=worktree), \
                 mock.patch.object(module.tempfile, "NamedTemporaryFile", side_effect=recording_named_tmp), \
                 mock.patch.object(module.subprocess, "Popen", side_effect=FileNotFoundError("codex")):
                with self.assertRaises(FileNotFoundError):
                    module.dispatch_worker(pr, review, dry_run=False)

        self.assertEqual(len(created_files), 1)
        self.assertFalse(
            os.path.exists(created_files[0]),
            "temp prompt file leaked to disk after Popen error",
        )


if __name__ == "__main__":
    unittest.main()
