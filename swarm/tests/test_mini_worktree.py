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

from swarm.mini._internal import worktree as wt_mod


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


class TestCopyGovernanceSidecars(unittest.TestCase):
    """Root cause of policy_signature_missing on every `mini open`:
    chitin.yaml.sig is gitignored, so `git worktree add` leaves it behind
    in the primary checkout. Without it, the governance hook rejects
    every tool call inside the new worktree. The fix copies the sidecar.
    """

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.primary = Path(self.tmp.name) / "primary"
        self.worktree = Path(self.tmp.name) / "worktree"
        self.primary.mkdir()
        self.worktree.mkdir()

    def test_copies_chitin_yaml_sig(self):
        (self.primary / "chitin.yaml").write_text("policy: ...")
        (self.primary / "chitin.yaml.sig").write_bytes(b"\x00\x01signature")
        copied = wt_mod.copy_governance_sidecars(
            primary=self.primary, worktree=self.worktree
        )
        self.assertEqual(copied, ["chitin.yaml.sig"])
        self.assertEqual(
            (self.worktree / "chitin.yaml.sig").read_bytes(),
            b"\x00\x01signature",
        )

    def test_missing_source_is_silent(self):
        copied = wt_mod.copy_governance_sidecars(
            primary=self.primary, worktree=self.worktree
        )
        self.assertEqual(copied, [])

    def test_does_not_overwrite_existing(self):
        (self.primary / "chitin.yaml.sig").write_bytes(b"primary")
        (self.worktree / "chitin.yaml.sig").write_bytes(b"worktree-local")
        copied = wt_mod.copy_governance_sidecars(
            primary=self.primary, worktree=self.worktree
        )
        self.assertEqual(copied, [])
        self.assertEqual(
            (self.worktree / "chitin.yaml.sig").read_bytes(), b"worktree-local"
        )

    def test_create_worktree_invokes_sidecar_copy(self):
        """End-to-end: create_worktree must trigger the sidecar copy after
        git worktree add succeeds."""
        with mock.patch.object(wt_mod, "primary_checkout", return_value=self.primary):
            with mock.patch.object(
                wt_mod, "worktree_path", lambda gid: self.worktree / gid
            ):
                # Pre-seed the primary with a signature
                (self.primary / "chitin.yaml.sig").write_bytes(b"sig-data")

                def fake(args, **kw):
                    if "worktree" in args and "add" in args:
                        # Materialize the worktree dir as `git worktree add` would.
                        for a in args:
                            if isinstance(a, str) and a.startswith(str(self.worktree)):
                                Path(a).mkdir(parents=True, exist_ok=True)
                    return _ok(args, **kw)

                wt, _ = wt_mod.create_worktree(
                    goal_id="g-sig", ticket=None, runner=fake
                )
                self.assertTrue((wt / "chitin.yaml.sig").is_file())
                self.assertEqual((wt / "chitin.yaml.sig").read_bytes(), b"sig-data")


if __name__ == "__main__":
    unittest.main()
