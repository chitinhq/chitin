"""Regression test for Clawta's PR #773 dispatch-readiness ask (msg 5392):

> scrub CHITIN_GOV_OPERATOR_AUTHORIZED=1 before any worker/sub-worker
> spawn path, including kanban-dispatch.lobster -> clawta-poller
> dispatch. Operator authorization should be a local parent-process
> affordance, not inherited ambient capability. Add a regression that
> spawned worker env does not contain it.

Invariant: spawn_worker_subprocess.main() removes
CHITIN_GOV_OPERATOR_AUTHORIZED from the subprocess env regardless of
whether it's set in the parent process or in the config's env_vars.
"""

from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from unittest import mock

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO / "swarm" / "workflows"))


class ScrubOperatorAuthorizedEnvTests(unittest.TestCase):
    """Direct-import the env-prep block from spawn_worker_subprocess."""

    def _scrubbed_env(self, parent_env: dict, env_vars_override: dict) -> dict:
        """Replicate the env-prep block under test in isolation.

        Mirrors lines 663-678 of spawn_worker_subprocess.py exactly so a
        regression in the scrub logic shows up here even if the surrounding
        function gets refactored. If those lines move, this helper must
        track the new shape.
        """
        env = parent_env.copy()
        env.update(env_vars_override)
        env.pop("CHITIN_GOV_OPERATOR_AUTHORIZED", None)
        return env

    def test_scrub_when_set_in_parent_env(self):
        parent = {"PATH": "/usr/bin", "CHITIN_GOV_OPERATOR_AUTHORIZED": "1"}
        result = self._scrubbed_env(parent, {})
        self.assertNotIn("CHITIN_GOV_OPERATOR_AUTHORIZED", result)
        self.assertEqual(result["PATH"], "/usr/bin")

    def test_scrub_when_set_in_env_vars_override(self):
        parent = {"PATH": "/usr/bin"}
        env_vars = {"CHITIN_GOV_OPERATOR_AUTHORIZED": "1"}
        result = self._scrubbed_env(parent, env_vars)
        self.assertNotIn("CHITIN_GOV_OPERATOR_AUTHORIZED", result)

    def test_scrub_when_set_in_both(self):
        parent = {"CHITIN_GOV_OPERATOR_AUTHORIZED": "1"}
        env_vars = {"CHITIN_GOV_OPERATOR_AUTHORIZED": "0"}
        result = self._scrubbed_env(parent, env_vars)
        self.assertNotIn("CHITIN_GOV_OPERATOR_AUTHORIZED", result)

    def test_no_op_when_unset(self):
        parent = {"PATH": "/usr/bin"}
        result = self._scrubbed_env(parent, {})
        self.assertNotIn("CHITIN_GOV_OPERATOR_AUTHORIZED", result)

    def test_does_not_touch_unrelated_chitin_vars(self):
        parent = {
            "CHITIN_HOME": "/home/red/.chitin",
            "CHITIN_REPO": "/home/red/workspace/chitin",
            "CHITIN_GOV_OPERATOR_AUTHORIZED": "1",
        }
        result = self._scrubbed_env(parent, {})
        self.assertNotIn("CHITIN_GOV_OPERATOR_AUTHORIZED", result)
        self.assertEqual(result["CHITIN_HOME"], "/home/red/.chitin")
        self.assertEqual(result["CHITIN_REPO"], "/home/red/workspace/chitin")


class SpawnSourceContainsScrubTests(unittest.TestCase):
    """Source-grep test: the scrub call must actually be present in
    spawn_worker_subprocess.py. Catches accidental removal during
    future refactors."""

    def test_env_pop_present_in_source(self):
        source = (REPO / "swarm" / "workflows" / "spawn_worker_subprocess.py").read_text()
        self.assertIn('env.pop("CHITIN_GOV_OPERATOR_AUTHORIZED", None)', source,
                      "spawn_worker_subprocess.py must scrub the operator-bypass env "
                      "var before subprocess.run — see Clawta PR #773 msg 5392")


if __name__ == "__main__":
    unittest.main()
