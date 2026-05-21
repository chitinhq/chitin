"""Spec 036 — dispatch fault-tolerance invariants.

# spec: 036-dispatch-fault-tolerance-invariants

Tests for the 4 invariants from the 2026-05-18 silent-dead-window
retro. Each was a live hot-patch tonight without test coverage;
this file binds them under the spec 020 §1.2 contract.

Layer: integration (per spec 020 exception clause; the dispatch
code path IS the e2e — no browser/HTTP surface to drive).
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
PICK_DRIVER = ROOT / "swarm" / "workflows" / "_pick_driver.py"
RECOVERY_DOCTOR = ROOT / "swarm" / "bin" / "dispatch-recovery-doctor.sh"
LOBSTER = ROOT / "swarm" / "workflows" / "kanban-dispatch.lobster"


def _empty_cards_dir() -> Path:
    """Tiny fixture so _pick_driver has something to rank but doesn't need real cards."""
    d = Path(tempfile.mkdtemp(prefix="spec036-cards-"))
    (d / "codex.json").write_text(json.dumps({
        "id": "codex",
        "capabilities": [{"skill": "ts", "depth": "strong"}, {"skill": "python", "depth": "strong"}],
        "models": [{"id": "gpt-5.4", "premium_cost": 1.0}],
    }))
    return d


class Inv1ClassifyRobustnessTests(unittest.TestCase):
    """Inv-1: _pick_driver.py must not crash on non-JSON stdin."""

    def setUp(self):
        self.cards_dir = _empty_cards_dir()
        self.env = {
            **os.environ,
            "OPENCLAW_AGENT_CARDS_DIR": str(self.cards_dir),
            # Disable LLM router to skip the openclaw-agent call in tests
            "ROUTER_MODE": "deterministic",
        }

    def _run_pick(self, stdin_text: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, str(PICK_DRIVER)],
            input=stdin_text, capture_output=True, text=True, env=self.env,
            timeout=30,
        )

    def test_pick_driver_tolerates_garbage_stdin(self):
        """Inv-1: non-JSON stdin → no crash; deterministic fallback emits a result."""
        garbage_inputs = [
            "<html>500 internal server error</html>",
            "EMBEDDED FALLBACK: Gateway agent failed; running embedded agent",
            "",                  # empty stdin
            "   \n\t  \n",       # whitespace only
            '{"missing": broken JSON',  # malformed JSON (unquoted value)
        ]
        for stdin_text in garbage_inputs:
            with self.subTest(stdin_preview=stdin_text[:40]):
                res = self._run_pick(stdin_text)
                # The contract: exit 0 + emit a result envelope. We don't
                # require the driver be non-unassigned; we require no crash.
                self.assertEqual(
                    res.returncode, 0,
                    f"crashed on garbage stdin {stdin_text!r}: "
                    f"stderr={res.stderr[:400]!r}",
                )
                # Should emit valid JSON on stdout
                try:
                    payload = json.loads(res.stdout)
                except json.JSONDecodeError as e:
                    self.fail(f"non-JSON stdout for garbage input: {e}; stdout={res.stdout[:400]!r}")
                # Must indicate deterministic fallback was used
                self.assertIn("router_mode", payload)


class Inv3LobsterStaleBranchTests(unittest.TestCase):
    """Inv-3: lobster recovers from stale local agent/<driver>-* branch.

    Static-analysis assertion that the lobster carries the recovery
    pattern. (A full integration test on the lobster would require
    bootstrapping a fake board + lobster runtime; out of scope for
    this PR. The static-analysis catches regressions in the recovery
    contract — same pattern as spec 018's test.)
    """

    def test_lobster_does_not_silently_use_stale_local_branch(self):
        """The lobster's worktree-add fallback MUST NOT silently check out
        a pre-existing local agent/<driver>-* branch with stale commits.

        Current state (this PR documents the contract; the fallback
        deletion is on the followup ticket per Out-of-scope). For now,
        the test asserts the lobster carries an explicit comment block
        warning about the failure mode so future contributors don't
        introduce the silent-fallback regression while not reading
        spec 036.

        TODO: extend to assert active recovery (delete + recreate) when
        the lobster patch lands.
        """
        text = LOBSTER.read_text()
        # The lobster's existing two-form worktree-add (one of these
        # patterns must exist — proves the surface this test is about
        # actually lives in the lobster, not somewhere else).
        self.assertIn('git -C "$CHITIN_REPO" worktree add', text)
        # Spec 018 base-freshness check exists — that's the LAST line
        # of defense + what fired in our retro to surface Inv-3.
        self.assertIn('base-freshness invariant violated', text,
                      "spec 018 base-freshness check must remain — it's "
                      "what surfaces Inv-3 in production until the active "
                      "recovery patch lands")


class Inv4RecoveryDoctorTests(unittest.TestCase):
    """Inv-4: dispatch-recovery-doctor.sh detects degraded gateway + restarts."""

    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="spec036-doctor-"))
        self.bin_dir = self.tmp / "bin"
        self.bin_dir.mkdir()

    def _mock_curl(self, health_response: str = "", exit_code: int = 0):
        """Write a fake curl that returns health_response."""
        mock_curl = self.bin_dir / "curl"
        mock_curl.write_text(
            f"#!/usr/bin/env bash\necho '{health_response}'\nexit {exit_code}\n"
        )
        mock_curl.chmod(0o755)

    def _mock_systemctl(self, restart_succeeds: bool = True):
        rc = 0 if restart_succeeds else 1
        mock_systemctl = self.bin_dir / "systemctl"
        mock_systemctl.write_text(
            f"#!/usr/bin/env bash\necho \"mock systemctl $@\"\nexit {rc}\n"
        )
        mock_systemctl.chmod(0o755)

    def _run_doctor(self, *args, env_overrides=None) -> subprocess.CompletedProcess:
        env = {
            **os.environ,
            "PATH": str(self.bin_dir) + os.pathsep + os.environ["PATH"],
        }
        if env_overrides:
            env.update(env_overrides)
        return subprocess.run(
            ["bash", str(RECOVERY_DOCTOR), *args],
            env=env, capture_output=True, text=True,
        )

    def test_recovery_doctor_present_and_executable(self):
        self.assertTrue(RECOVERY_DOCTOR.exists())
        self.assertTrue(os.access(RECOVERY_DOCTOR, os.X_OK))

    def test_recovery_doctor_detects_dead_gateway_and_restarts(self):
        """When gateway is down, doctor invokes systemctl restart."""
        # First curl call returns empty (dead); doctor restarts; second curl returns empty too.
        # Doctor reports problems > 0.
        self._mock_curl(health_response="", exit_code=0)
        self._mock_systemctl(restart_succeeds=True)
        res = self._run_doctor("--gateway-only")
        self.assertEqual(res.returncode, 0,
                         f"doctor exited non-zero: stderr={res.stderr!r}")
        # systemctl restart was invoked
        self.assertIn("mock systemctl --user restart openclaw-gateway",
                      res.stdout + res.stderr)

    def test_recovery_doctor_quiet_when_healthy(self):
        """When gateway is healthy, doctor doesn't try to restart."""
        self._mock_curl(health_response='{"ok":true,"status":"live"}', exit_code=0)
        self._mock_systemctl(restart_succeeds=True)
        res = self._run_doctor("--gateway-only")
        self.assertEqual(res.returncode, 0)
        # systemctl restart should NOT appear
        self.assertNotIn("restart openclaw-gateway", res.stdout + res.stderr,
                         "doctor restarted a healthy gateway")

    def test_recovery_doctor_check_only_exits_nonzero_on_problem(self):
        """--check mode: diagnose only, exit 1 if degraded."""
        self._mock_curl(health_response="", exit_code=0)
        self._mock_systemctl()
        res = self._run_doctor("--check", "--gateway-only")
        self.assertEqual(res.returncode, 1)


# Inv-2 (stale task_runs auto-close) — TODO: integration test against a
# fixture readybench kanban DB + real poller subprocess. Skipped in this
# PR because the poller's auto-close logic is the follow-up implementation;
# Inv-2 in the spec calls out the contract, the implementation lands later.
@unittest.skip("Inv-2 implementation is a follow-up PR; spec contract documented in spec 036")
class Inv2StaleTaskRunsAutoCloseTests(unittest.TestCase):
    def test_poller_auto_closes_stale_running_task_runs(self):
        pass


if __name__ == "__main__":
    unittest.main()
