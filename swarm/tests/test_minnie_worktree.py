from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.minnie._internal import worktree as wt_mod


def _ok(args, **kw):
    return subprocess.CompletedProcess(args=args, returncode=0, stdout="", stderr="")


class TestResolveBranchName(unittest.TestCase):
    def test_with_ticket(self):
        self.assertEqual(
            wt_mod.resolve_branch_name(ticket="t_98aafaed", goal_id="g1"),
            "agent/octi-t_98aafaed",
        )

    def test_without_ticket(self):
        self.assertEqual(
            wt_mod.resolve_branch_name(ticket=None, goal_id="g1"),
            "octi/g1",
        )


class TestCreateWorktree(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        # Force the worktree path into the tmpdir so we don't touch ~/workspace.
        self._patcher = mock.patch.object(
            wt_mod, "worktree_path", lambda gid: Path(self.tmp.name) / gid
        )
        self._patcher.start()
        self.addCleanup(self._patcher.stop)

    def test_creates_branch_and_path(self):
        runs: list[list[str]] = []

        def fake(args, **kw):
            runs.append(args)
            return _ok(args, **kw)

        path, branch = wt_mod.create_worktree(
            goal_id="abc-12345678", ticket=None, runner=fake,
        )
        self.assertEqual(branch, "octi/abc-12345678")
        self.assertEqual(path, Path(self.tmp.name) / "abc-12345678")
        # fetch + add
        self.assertEqual(len(runs), 2)
        self.assertEqual(runs[0][:5], ["git", "-C", str(wt_mod.primary_checkout()), "fetch", "origin"])
        self.assertIn("add", runs[1])
        self.assertIn("-b", runs[1])
        self.assertIn("origin/main", runs[1])

    def test_collision_raises_file_exists(self):
        target = Path(self.tmp.name) / "abc-12345678"
        target.mkdir()
        with self.assertRaises(FileExistsError):
            wt_mod.create_worktree(goal_id="abc-12345678", ticket=None, runner=_ok)

    def test_ticket_path_uses_agent_octi_branch(self):
        runs = []

        def fake(args, **kw):
            runs.append(args)
            return _ok(args, **kw)

        _, branch = wt_mod.create_worktree(
            goal_id="x-yy", ticket="t_123", runner=fake,
        )
        self.assertEqual(branch, "agent/octi-t_123")


if __name__ == "__main__":
    unittest.main()
