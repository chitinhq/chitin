#!/usr/bin/env python3
"""Regression test for ticket t_4317ae81.

Invariant: ``chitin.yaml.sig`` is a tracked, non-ignored file.

Why it matters: the kernel policy gate refuses to load ``chitin.yaml``
without a verifying ``chitin.yaml.sig`` and then denies every tool call
with ``policy_signature_missing``. A dispatched worker runs in a git
worktree; ``git worktree add`` populates a worktree from tracked files
only. So the sig must be tracked AND not ignored — otherwise every
worktree comes up sig-less and the worker deadlocks on its first tool
call.

This pins the two halves of that invariant. The kernel-side signature
*verification* logic is covered separately by
go/execution-kernel/internal/gov/policy_signature_test.go.
"""

from __future__ import annotations

import subprocess
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SIG = "chitin.yaml.sig"


def git(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", "-C", str(REPO_ROOT), *args],
        capture_output=True,
        text=True,
    )


class GovernanceSigTrackedTests(unittest.TestCase):
    def test_sig_is_tracked(self) -> None:
        """The sig is committed, so `git worktree add` carries it along."""
        result = git("ls-files", "--error-unmatch", SIG)
        self.assertEqual(
            result.returncode,
            0,
            msg=(
                f"{SIG} is not tracked — dispatched worktrees come up "
                f"sig-less and deadlock the policy gate (t_4317ae81). "
                f"stderr: {result.stderr.strip()}"
            ),
        )

    def test_sig_is_not_ignored(self) -> None:
        """The sig must match no .gitignore rule.

        A tracked-but-ignored file is a trap: it works until someone
        runs `git rm --cached`, after which it silently stops being
        carried into worktrees and never returns — re-opening the
        t_4317ae81 deadlock. --no-index reports the ignore match even
        for a currently-tracked path.
        """
        result = git("check-ignore", "--no-index", SIG)
        self.assertNotEqual(
            result.returncode,
            0,
            msg=(
                f"{SIG} matches a .gitignore rule "
                f"({result.stdout.strip()}) — remove it so the tracked "
                f"sig stays inherited by every worktree (t_4317ae81)."
            ),
        )


if __name__ == "__main__":
    unittest.main()
