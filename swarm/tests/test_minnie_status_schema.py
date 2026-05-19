from __future__ import annotations

import json
import os
import sys
import tempfile
import time
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.minnie._internal.statedir import (
    STATE_ROOT_ENV,
    create_state_dir,
    state_dir,
)
from swarm.minnie.session import MiniSession, StatusMissingError


VALID_STATES = {"starting", "working", "blocked", "verifying", "done", "failed", "needs_review"}


class TestStatusSchema(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self._old = os.environ.get(STATE_ROOT_ENV)
        os.environ[STATE_ROOT_ENV] = self.tmp.name
        create_state_dir(
            "gtest",
            goal="g",
            branch="octi/gtest",
            worktree=Path("/tmp/wt"),
        )
        self.sd = state_dir("gtest")

    def tearDown(self) -> None:
        if self._old is None:
            os.environ.pop(STATE_ROOT_ENV, None)
        else:
            os.environ[STATE_ROOT_ENV] = self._old

    def _write_status(self, payload: dict) -> None:
        (self.sd / "status.json").write_text(json.dumps(payload))

    def test_status_missing_raises(self):
        sess = MiniSession.attach("gtest")
        with self.assertRaises(StatusMissingError):
            sess.status()

    def test_status_parses_well_formed(self):
        good = {
            "state": "working",
            "updated_at": int(time.time()),
            "summary": "implementing minnie",
            "next": "write tests",
            "blockers": [],
            "verify": "pytest swarm/tests/test_minnie_state_dir.py",
        }
        self._write_status(good)
        sess = MiniSession.attach("gtest")
        out = sess.status()
        self.assertEqual(out["state"], "working")

    def test_all_seven_enum_values_accepted(self):
        for state in VALID_STATES:
            self._write_status({
                "state": state, "updated_at": 1, "summary": "x", "next": "y",
                "blockers": [], "verify": "true",
            })
            sess = MiniSession.attach("gtest")
            self.assertEqual(sess.status()["state"], state)

    def test_status_malformed_raises_json_decode_error(self):
        (self.sd / "status.json").write_text("{this is not json")
        sess = MiniSession.attach("gtest")
        with self.assertRaises(json.JSONDecodeError):
            sess.status()


if __name__ == "__main__":
    unittest.main()
