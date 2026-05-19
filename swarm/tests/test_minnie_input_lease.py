from __future__ import annotations

import json
import sys
import tempfile
import time
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.minnie._internal.lease import (
    DEFAULT_LEASE_SECONDS,
    Lease,
    LockHeldError,
    acquire,
    release,
)


class TestLease(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.sd = Path(self.tmp.name)

    def test_acquire_writes_lock(self):
        payload = acquire(self.sd, holder="alice", lease_seconds=30)
        lock_file = self.sd / "input.lock"
        self.assertTrue(lock_file.is_file())
        data = json.loads(lock_file.read_text())
        self.assertEqual(data["holder"], "alice")
        self.assertEqual(payload["holder"], "alice")
        self.assertAlmostEqual(data["expires_at"] - data["acquired_at"], 30, places=1)

    def test_double_acquire_raises_lock_held(self):
        acquire(self.sd, holder="alice", lease_seconds=60)
        with self.assertRaises(LockHeldError) as cm:
            acquire(self.sd, holder="bob", lease_seconds=60)
        self.assertEqual(cm.exception.holder, "alice")

    def test_expired_lock_auto_clears(self):
        acquire(self.sd, holder="alice", lease_seconds=1)
        # Stale the lock by rewriting expires_at to the past.
        lock_file = self.sd / "input.lock"
        d = json.loads(lock_file.read_text())
        d["expires_at"] = time.time() - 100
        lock_file.write_text(json.dumps(d))

        new = acquire(self.sd, holder="bob", lease_seconds=60)
        self.assertEqual(new["holder"], "bob")

    def test_release_removes_lock(self):
        acquire(self.sd, holder="alice", lease_seconds=60)
        release(self.sd)
        self.assertFalse((self.sd / "input.lock").exists())

    def test_release_swallows_missing(self):
        # idempotent — already-gone should not raise
        release(self.sd)
        release(self.sd)

    def test_context_manager(self):
        with Lease(self.sd, holder="cm", lease_seconds=60):
            self.assertTrue((self.sd / "input.lock").is_file())
        self.assertFalse((self.sd / "input.lock").exists())

    def test_default_lease_is_60s(self):
        self.assertEqual(DEFAULT_LEASE_SECONDS, 60)


if __name__ == "__main__":
    unittest.main()
