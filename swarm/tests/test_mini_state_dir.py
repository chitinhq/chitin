from __future__ import annotations

import os
import sys
import tempfile
import time
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.mini._internal.statedir import (
    STATE_ROOT_ENV,
    cleanup_stale_temp_files,
    create_state_dir,
    read_state_file,
    state_dir,
    state_root,
)


class TestStateDir(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self._old_env = os.environ.get(STATE_ROOT_ENV)
        os.environ[STATE_ROOT_ENV] = self.tmp.name

    def tearDown(self) -> None:
        if self._old_env is None:
            os.environ.pop(STATE_ROOT_ENV, None)
        else:
            os.environ[STATE_ROOT_ENV] = self._old_env

    def test_state_root_honors_env(self):
        self.assertEqual(state_root(), Path(self.tmp.name).resolve())

    def test_create_state_dir_writes_all_files(self):
        sd = create_state_dir(
            "abc-12345678",
            goal="Add mini launcher",
            branch="octi/abc-12345678",
            worktree=Path("/tmp/wt"),
        )
        self.assertTrue(sd.is_dir())
        self.assertEqual((sd / "goal.txt").read_text().strip(), "Add mini launcher")
        self.assertEqual((sd / "goal_id").read_text().strip(), "abc-12345678")
        self.assertEqual((sd / "branch").read_text().strip(), "octi/abc-12345678")
        self.assertEqual((sd / "worktree").read_text().strip(), "/tmp/wt")

    def test_read_state_file(self):
        create_state_dir(
            "g1",
            goal="g",
            branch="octi/g1",
            worktree=Path("/tmp/wt"),
        )
        self.assertEqual(read_state_file("g1", "branch"), "octi/g1")

    def test_cleanup_stale_temp_files(self):
        create_state_dir("g2", goal="g", branch="octi/g2", worktree=Path("/tmp"))
        sd = state_dir("g2")
        # one fresh, one stale
        fresh = sd / ".inject-fresh-1.txt"
        stale = sd / ".inject-stale-2.txt"
        fresh.write_text("hi")
        stale.write_text("old")
        old_ts = time.time() - 3600  # 1 hour ago
        os.utime(stale, (old_ts, old_ts))

        unlinked = cleanup_stale_temp_files("g2", max_age_seconds=60)
        self.assertEqual(unlinked, 1)
        self.assertTrue(fresh.exists())
        self.assertFalse(stale.exists())

    def test_cleanup_swallows_missing_dir(self):
        unlinked = cleanup_stale_temp_files("nonexistent-id")
        self.assertEqual(unlinked, 0)


if __name__ == "__main__":
    unittest.main()
