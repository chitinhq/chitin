"""Spec 025 — dispatch atomicity invariant regression tests.

# spec: 025-dispatch-atomicity-invariant

Three layers:
- Static-analysis against lobster + kanban-flow text (the acquire
  call lives at the right place; the mirror matches canonical).
- Integration test exercising the actual race: 2 subprocesses
  contending for the same per-ticket lock; assert serialization.
- Helper script smoke (executable + correct exit codes).
"""
from __future__ import annotations

import os
import subprocess
import tempfile
import time
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CANONICAL = ROOT / "swarm" / "workflows" / "kanban-dispatch.lobster"
MIRROR = ROOT / "docs" / "governance-setup-extras" / "kanban-dispatch.lobster"
KANBAN_FLOW = ROOT / "scripts" / "kanban-flow"
LOCK_HELPER = ROOT / "swarm" / "bin" / "dispatch-finalize-lock.sh"


class LockHelperShapeTests(unittest.TestCase):
    def test_lock_helper_present_and_executable(self):
        self.assertTrue(LOCK_HELPER.exists(), f"{LOCK_HELPER} must exist")
        self.assertTrue(os.access(LOCK_HELPER, os.X_OK), "lock helper must be +x")

    def test_lock_helper_path_returns_canonical_path(self):
        res = subprocess.run(
            ["bash", str(LOCK_HELPER), "path", "t_TEST"],
            capture_output=True, text=True, env={**os.environ, "LOCK_ROOT": "/tmp/spec025"},
        )
        self.assertEqual(res.returncode, 0)
        self.assertEqual(res.stdout.strip(), "/tmp/spec025/dispatch-t_TEST.lock")

    def test_lock_helper_rejects_invalid_ticket_id(self):
        """Defense against arbitrary path injection via TID."""
        res = subprocess.run(
            ["bash", str(LOCK_HELPER), "path", "../../../etc/passwd"],
            capture_output=True, text=True,
        )
        self.assertEqual(res.returncode, 2, "must reject TIDs not matching t_<alnum>")


class LobsterShapeTests(unittest.TestCase):
    def test_finalize_dispatch_acquires_lock(self):
        """R2: finalize_dispatch acquires the lock BEFORE the status check."""
        text = CANONICAL.read_text()
        # The atomicity block must include the flock command.
        self.assertIn("Spec 025 atomicity invariant", text)
        self.assertIn('flock -nE 75 "$ATOMICITY_LOCKFD"', text)
        self.assertIn("LOCKFILE=\"${HOME}/.chitin/locks/dispatch-${ticket_id}.lock\"", text)
        # Lock acquire MUST come before the existing CURRENT_STATUS check
        # — otherwise the race surface remains.
        lock_idx = text.find("flock -nE 75")
        status_idx = text.find("CURRENT_STATUS=$(hermes kanban --board $resolve_board.json.board show")
        self.assertGreater(lock_idx, 0, "lock acquire must exist")
        self.assertGreater(status_idx, lock_idx,
                           "lock acquire MUST precede the CURRENT_STATUS check, otherwise the TOCTOU race remains")

    def test_finalize_lock_failure_exits_zero_not_retry(self):
        """R2 fail-fast: failed lock acquire exits 0 with named message
        (cron retries; not blocking-wait)."""
        text = CANONICAL.read_text()
        # The retry message
        self.assertIn("lock held by concurrent finalize/unblock", text)
        # Exit 0 (not 75) so lobster moves on; cron retries the dispatch
        lock_block = text.split("Spec 025 atomicity invariant", 1)[1].split("CURRENT_STATUS=", 1)[0]
        self.assertIn("exit 0", lock_block, "lock-fail must exit 0; lobster doesn't retry, cron does")

    def test_workflow_mirror_matches_canonical(self):
        """docs/governance-setup-extras/kanban-dispatch.lobster must equal canonical."""
        self.assertEqual(CANONICAL.read_text(), MIRROR.read_text())


class KanbanFlowShapeTests(unittest.TestCase):
    def test_kanban_flow_unblock_acquires_lock(self):
        """R3: kanban-flow unblock acquires the same per-ticket lock."""
        text = KANBAN_FLOW.read_text()
        self.assertIn("Spec 025 atomicity invariant", text)
        self.assertIn('flock -nE 75 "$ATOMICITY_LOCKFD"', text)
        self.assertIn('LOCKFILE="${HOME}/.chitin/locks/dispatch-${id}.lock"', text)
        # Lock acquire MUST precede assert_status (otherwise concurrent
        # finalize sees the unblock happen before the lock is held).
        unblock_start = text.find("  unblock)")
        lock_idx = text.find('flock -nE 75 "$ATOMICITY_LOCKFD"', unblock_start)
        status_idx = text.find('assert_status "$id" blocked', unblock_start)
        self.assertGreater(lock_idx, unblock_start, "lock must be inside unblock branch")
        self.assertGreater(status_idx, lock_idx, "lock MUST precede assert_status")

    def test_kanban_flow_unblock_exits_75_on_lock_held(self):
        """R3: failed lock acquire on unblock exits 75 (not 0) so the
        operator/cron sees the failure."""
        text = KANBAN_FLOW.read_text()
        unblock_block = text.split("  unblock)", 1)[1].split("  demote)", 1)[0]
        self.assertIn("exit 75", unblock_block)


class IntegrationRaceTests(unittest.TestCase):
    """The actual atomicity test: two contending shells.

    Uses a temp LOCK_ROOT so we don't pollute the operator's
    ~/.chitin/locks/. The test exercises the lock pattern that
    both finalize_dispatch and kanban-flow unblock use.
    """

    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="spec025-"))
        self.lock_root = self.tmp / "locks"
        self.lock_root.mkdir()
        self.tid = "t_RACETST"

    def test_concurrent_unblock_during_finalize_serializes(self):
        """R5: while one shell holds the lock, the other gets exit 75.

        First shell sleeps 0.5s inside the lock; second shell tries
        nonblock-acquire mid-sleep; second must get 75, first must
        exit 0.
        """
        lockfile = self.lock_root / f"dispatch-{self.tid}.lock"

        first_script = f"""
            set -uo pipefail
            mkdir -p {self.lock_root}
            exec {{FD}}>{lockfile}
            if flock -nE 75 $FD; then
                sleep 0.5
                exit 0
            else
                exit 75
            fi
        """

        second_script = first_script.replace("sleep 0.5", "true")

        proc1 = subprocess.Popen(["bash", "-c", first_script],
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        time.sleep(0.15)  # let proc1 acquire + start sleeping
        proc2 = subprocess.Popen(["bash", "-c", second_script],
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        rc1 = proc1.wait(timeout=3)
        rc2 = proc2.wait(timeout=3)

        # Exactly one should win (0); other gets 75
        results = sorted([rc1, rc2])
        self.assertEqual(results, [0, 75],
                         f"expected one win (0) + one fail (75); got {results}")

    def test_lock_released_after_fd_close(self):
        """R4: lock release on fd close — second shell can acquire after first exits."""
        lockfile = self.lock_root / f"dispatch-{self.tid}.lock"
        script = f"""
            set -uo pipefail
            mkdir -p {self.lock_root}
            exec {{FD}}>{lockfile}
            flock -nE 75 $FD || exit 75
            exit 0
        """
        # Run sequentially; both should succeed (no overlap)
        for _ in range(3):
            res = subprocess.run(["bash", "-c", script], capture_output=True, text=True)
            self.assertEqual(res.returncode, 0,
                             f"sequential acquires must all succeed; got {res.returncode}: {res.stderr!r}")


if __name__ == "__main__":
    unittest.main()
