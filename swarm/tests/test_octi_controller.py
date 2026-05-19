"""Tests for swarm.octi.controller.Controller.

The controller is exercised through a fake MiniSession that captures
nudge calls. The verifier is injected directly so we don't need /bin/sh
indirection for behaviour assertions.
"""

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

from swarm.octi.controller import (
    Controller,
    ControllerConfig,
    paused_flag_path,
    read_verdict,
)
from swarm.octi.verifier import VerifyResult


# -------- test doubles --------


class FakeSession:
    """Quacks like MiniSession for the controller's purposes."""

    def __init__(self, goal_id: str, state_dir: Path, worktree: Path) -> None:
        self.goal_id = goal_id
        self.state_dir = state_dir
        self.worktree = worktree
        self.nudges: list[dict] = []
        self.nudge_should_raise: Exception | None = None

    def nudge(self, message: str, *, holder: str | None = None, lease_seconds: int = 60) -> None:
        if self.nudge_should_raise:
            raise self.nudge_should_raise
        self.nudges.append({"message": message, "holder": holder, "lease_seconds": lease_seconds})


class FakeVerifier:
    """Records every command run and returns a queued or fixed result."""

    def __init__(self, result: VerifyResult | None = None) -> None:
        self._result = result or VerifyResult(
            verdict="passed",
            command="true",
            returncode=0,
            stdout="",
            stderr="",
            duration_seconds=0.0,
            timed_out=False,
        )
        self.calls: list[str | None] = []

    def run(self, command: str | None) -> VerifyResult:
        self.calls.append(command)
        return self._result


# -------- fixture helpers --------


def _write_status(sd: Path, **fields) -> None:
    payload = {
        "state": "working",
        "updated_at": int(time.time()),
        "summary": "",
        "next": "",
        "blockers": [],
        "verify": "true",
    }
    payload.update(fields)
    (sd / "status.json").write_text(json.dumps(payload))


def _make_controller(
    sd: Path,
    *,
    config: ControllerConfig | None = None,
    verifier: FakeVerifier | None = None,
    now: float = 1_000_000.0,
) -> tuple[Controller, FakeSession, list[float]]:
    wt = sd  # worktree == state dir for tests; verifier is faked anyway
    session = FakeSession("g-test", sd, wt)
    clock_state = {"now": now}
    def clock() -> float:
        return clock_state["now"]
    def sleep(s: float) -> None:
        clock_state["now"] += s
    # We hand a mutable list to the caller so they can poke "now" via clock fn.
    controller = Controller(
        session,
        config=config or ControllerConfig(
            poll_seconds=1.0,
            stall_seconds=60,
            nudge_cooldown_seconds=120,
            first_write_grace_seconds=30,
            verify_timeout_seconds=10,
        ),
        clock=clock,
        sleep_fn=sleep,
        verifier=verifier or FakeVerifier(),
    )
    controller._clock_state = clock_state  # type: ignore[attr-defined]
    return controller, session, clock_state  # type: ignore[return-value]


# -------- tests --------


class TestControllerInvariants(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.sd = Path(self.tmp.name)

    # ---- terminal states ----

    def test_state_failed_is_terminal(self):
        _write_status(self.sd, state="failed")
        ctl, session, _ = _make_controller(self.sd)
        outcome = ctl.tick()
        self.assertEqual(outcome, "terminal_failed")
        self.assertEqual(session.nudges, [])

    def test_state_needs_review_is_terminal(self):
        _write_status(self.sd, state="needs_review")
        ctl, session, _ = _make_controller(self.sd)
        outcome = ctl.tick()
        self.assertEqual(outcome, "terminal_needs_review")
        self.assertEqual(session.nudges, [])

    # ---- done-claim verification ----

    def test_done_with_passing_verifier_terminates_passed(self):
        _write_status(self.sd, state="done", verify="true", updated_at=999)
        verifier = FakeVerifier(VerifyResult(
            verdict="passed", command="true", returncode=0,
            stdout="ok", stderr="", duration_seconds=0.1, timed_out=False,
        ))
        ctl, _, _ = _make_controller(self.sd, verifier=verifier)
        outcome = ctl.tick()
        self.assertEqual(outcome, "terminal_done_passed")
        verdict = read_verdict(self.sd)
        self.assertIsNotNone(verdict)
        self.assertEqual(verdict["verdict"], "passed")
        self.assertEqual(verdict["status_updated_at"], 999)
        self.assertEqual(verifier.calls, ["true"])

    def test_done_with_failing_verifier_terminates_failed(self):
        _write_status(self.sd, state="done", verify="false", updated_at=1000)
        verifier = FakeVerifier(VerifyResult(
            verdict="failed", command="false", returncode=1,
            stdout="", stderr="boom", duration_seconds=0.1, timed_out=False,
        ))
        ctl, _, _ = _make_controller(self.sd, verifier=verifier)
        outcome = ctl.tick()
        self.assertEqual(outcome, "terminal_done_failed")
        verdict = read_verdict(self.sd)
        self.assertEqual(verdict["verdict"], "failed")
        self.assertEqual(verdict["returncode"], 1)
        self.assertEqual(verdict["stderr"], "boom")

    def test_done_with_empty_verify_routes_to_needs_review(self):
        _write_status(self.sd, state="done", verify="", updated_at=1001)
        verifier = FakeVerifier(VerifyResult(
            verdict="no_verifier", command="", returncode=None,
            stdout="", stderr="", duration_seconds=0.0, timed_out=False,
        ))
        ctl, _, _ = _make_controller(self.sd, verifier=verifier)
        outcome = ctl.tick()
        self.assertEqual(outcome, "terminal_needs_review")
        verdict = read_verdict(self.sd)
        self.assertEqual(verdict["verdict"], "no_verifier")

    def test_done_timeout_treated_as_done_failed(self):
        _write_status(self.sd, state="done", verify="sleep 99", updated_at=1002)
        verifier = FakeVerifier(VerifyResult(
            verdict="timeout", command="sleep 99", returncode=None,
            stdout="", stderr="", duration_seconds=10.0, timed_out=True,
        ))
        ctl, _, _ = _make_controller(self.sd, verifier=verifier)
        outcome = ctl.tick()
        self.assertEqual(outcome, "terminal_done_failed")

    def test_verify_runs_once_per_claim(self):
        _write_status(self.sd, state="done", verify="true", updated_at=2000)
        verifier = FakeVerifier(VerifyResult(
            verdict="passed", command="true", returncode=0,
            stdout="", stderr="", duration_seconds=0.0, timed_out=False,
        ))
        ctl, _, _ = _make_controller(self.sd, verifier=verifier)
        ctl.tick()
        ctl.tick()
        ctl.tick()
        self.assertEqual(len(verifier.calls), 1)

    def test_new_claim_triggers_new_verify(self):
        _write_status(self.sd, state="done", verify="true", updated_at=3000)
        verifier = FakeVerifier(VerifyResult(
            verdict="failed", command="true", returncode=1,
            stdout="", stderr="", duration_seconds=0.0, timed_out=False,
        ))
        ctl, _, _ = _make_controller(self.sd, verifier=verifier)
        ctl.tick()
        # New claim (different updated_at) should re-verify
        _write_status(self.sd, state="done", verify="true", updated_at=4000)
        ctl.tick()
        self.assertEqual(len(verifier.calls), 2)

    # ---- stall detection ----

    def test_stall_below_threshold_is_idle(self):
        # status written 30s before now; threshold = 60s
        _write_status(self.sd, state="working", updated_at=1_000_000 - 30)
        ctl, session, _ = _make_controller(self.sd, now=1_000_000)
        outcome = ctl.tick()
        self.assertEqual(outcome, "idle")
        self.assertEqual(session.nudges, [])

    def test_stall_above_threshold_nudges(self):
        # status written 120s before now; threshold = 60s
        _write_status(self.sd, state="working", updated_at=1_000_000 - 120)
        ctl, session, _ = _make_controller(self.sd, now=1_000_000)
        outcome = ctl.tick()
        self.assertEqual(outcome, "nudged_stale")
        self.assertEqual(len(session.nudges), 1)
        self.assertIn("stall-nudge", session.nudges[0]["message"])

    def test_nudge_cooldown_blocks_repeat_nudges(self):
        _write_status(self.sd, state="working", updated_at=1_000_000 - 200)
        ctl, session, clock_state = _make_controller(self.sd, now=1_000_000)
        first = ctl.tick()
        self.assertEqual(first, "nudged_stale")
        # Advance 10s — still inside cooldown of 120s
        clock_state["now"] = 1_000_010
        second = ctl.tick()
        self.assertEqual(second, "nudge_cooldown")
        self.assertEqual(len(session.nudges), 1)

    def test_nudge_cooldown_releases_after_window(self):
        _write_status(self.sd, state="working", updated_at=1_000_000 - 200)
        ctl, session, clock_state = _make_controller(self.sd, now=1_000_000)
        ctl.tick()
        # Advance past cooldown
        clock_state["now"] = 1_000_200
        outcome = ctl.tick()
        self.assertEqual(outcome, "nudged_stale")
        self.assertEqual(len(session.nudges), 2)

    def test_blocked_state_does_not_nudge_or_verify(self):
        _write_status(self.sd, state="blocked", updated_at=1_000_000 - 9999)
        ctl, session, _ = _make_controller(self.sd, now=1_000_000)
        outcome = ctl.tick()
        self.assertEqual(outcome, "blocked_observed")
        self.assertEqual(session.nudges, [])

    # ---- first-write grace ----

    def test_no_status_within_grace_is_first_write_pending(self):
        # No status.json at all; controller created at now=1_000_000
        ctl, session, _ = _make_controller(self.sd, now=1_000_000)
        outcome = ctl.tick()
        self.assertEqual(outcome, "first_write_pending")
        self.assertEqual(session.nudges, [])

    def test_no_status_past_grace_triggers_nudge(self):
        ctl, session, clock_state = _make_controller(self.sd, now=1_000_000)
        clock_state["now"] = 1_000_000 + 999  # well past 30s grace
        outcome = ctl.tick()
        self.assertEqual(outcome, "first_write_expired")
        self.assertEqual(len(session.nudges), 1)
        self.assertIn("no status.json", session.nudges[0]["message"])

    # ---- pause flag ----

    def test_pause_flag_blocks_tick(self):
        _write_status(self.sd, state="working", updated_at=1_000_000 - 9999)
        ctl, session, _ = _make_controller(self.sd, now=1_000_000)
        paused_flag_path(self.sd).write_text("paused by test\n")
        outcome = ctl.tick()
        self.assertEqual(outcome, "paused")
        self.assertEqual(session.nudges, [])

    def test_resume_after_pause_resumes_normal_logic(self):
        _write_status(self.sd, state="working", updated_at=1_000_000 - 9999)
        ctl, session, _ = _make_controller(self.sd, now=1_000_000)
        flag = paused_flag_path(self.sd)
        flag.write_text("paused\n")
        self.assertEqual(ctl.tick(), "paused")
        flag.unlink()
        self.assertEqual(ctl.tick(), "nudged_stale")

    # ---- nudge failure handling ----

    def test_nudge_failure_still_sets_cooldown(self):
        _write_status(self.sd, state="working", updated_at=1_000_000 - 9999)
        ctl, session, clock_state = _make_controller(self.sd, now=1_000_000)
        session.nudge_should_raise = RuntimeError("kitty unreachable")
        outcome = ctl.tick()
        self.assertEqual(outcome, "nudged_stale")  # outcome unchanged
        # Next tick: still inside cooldown despite failure
        clock_state["now"] = 1_000_010
        # Need to clear the raise so the second nudge wouldn't run — but it shouldn't.
        session.nudge_should_raise = None
        self.assertEqual(ctl.tick(), "nudge_cooldown")
        self.assertEqual(len(session.nudges), 0)  # neither attempt landed

    # ---- run() loop ----

    def test_run_exits_on_terminal(self):
        _write_status(self.sd, state="failed", updated_at=1_000_000)
        ctl, _, _ = _make_controller(self.sd, now=1_000_000)
        outcome = ctl.run(max_ticks=10)
        self.assertEqual(outcome, "terminal_failed")

    def test_run_stops_when_max_ticks_exceeded(self):
        _write_status(self.sd, state="working", updated_at=1_000_000)
        ctl, _, _ = _make_controller(self.sd, now=1_000_000)
        outcome = ctl.run(max_ticks=3)
        # idle on a fresh status — never reaches terminal
        self.assertEqual(outcome, "idle")


class TestControllerVerdictPersistence(unittest.TestCase):
    """Verdict file is atomic and JSON-parseable."""

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.sd = Path(self.tmp.name)

    def test_verdict_file_is_written_after_done(self):
        _write_status(self.sd, state="done", verify="true", updated_at=5000)
        verifier = FakeVerifier(VerifyResult(
            verdict="passed", command="true", returncode=0,
            stdout="hello", stderr="", duration_seconds=0.5, timed_out=False,
        ))
        ctl, _, _ = _make_controller(self.sd, verifier=verifier, now=1_000_000)
        ctl.tick()
        verdict_file = self.sd / "controller_verdict.json"
        self.assertTrue(verdict_file.is_file())
        payload = json.loads(verdict_file.read_text())
        self.assertEqual(payload["verdict"], "passed")
        self.assertEqual(payload["returncode"], 0)
        self.assertEqual(payload["stdout"], "hello")
        self.assertEqual(payload["duration_seconds"], 0.5)
        self.assertEqual(payload["verified_at"], 1_000_000)
        self.assertEqual(payload["status_updated_at"], 5000)

    def test_no_tmp_left_behind(self):
        _write_status(self.sd, state="done", verify="true", updated_at=6000)
        ctl, _, _ = _make_controller(self.sd, now=1_000_000)
        ctl.tick()
        tmps = list(self.sd.glob("*.tmp"))
        self.assertEqual(tmps, [])


if __name__ == "__main__":
    unittest.main()
