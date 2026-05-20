"""Tests for swarm.mini._internal.routing — thread_id <-> goal_id resolution.

Slice 1 of spec 039 binds R1 (per-session thread binding) and B2
(first-inbound message binds the mapping). These tests pin both behaviors
plus the 1:1 invariant required by AC2.
"""

from __future__ import annotations

import os
import stat
import tempfile
import unittest
from pathlib import Path

from swarm.mini._internal.routing import (
    BoundThreadMismatch,
    RouteResult,
    ThreadAlreadyClaimed,
    bind_thread,
    route_message,
)


def _make_state_dir(state_root: Path, goal_id: str, *, thread_id: str | None = None) -> Path:
    sd = state_root / goal_id
    sd.mkdir(parents=True, exist_ok=True)
    (sd / "goal_id").write_text(goal_id + "\n")
    if thread_id is not None:
        (sd / "thread_id").write_text(thread_id + "\n")
    return sd


class TestRouteMessage(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_bound_thread_routes_to_goal(self) -> None:
        sd = _make_state_dir(self.root, "alpha", thread_id="T1")
        r = route_message(state_root=self.root, bus_thread_id="T1", body="@mini ping")
        self.assertEqual(r.decision, "bound")
        self.assertEqual(r.goal_id, "alpha")
        self.assertEqual(r.state_dir, sd)
        self.assertEqual(r.candidates, ())

    def test_empty_state_root_returns_no_match(self) -> None:
        r = route_message(state_root=self.root, bus_thread_id="T1", body="hi @mini")
        self.assertEqual(r.decision, "no_match")
        self.assertIsNone(r.goal_id)
        self.assertIsNone(r.state_dir)

    def test_unbound_thread_no_goal_in_body_returns_no_match(self) -> None:
        _make_state_dir(self.root, "alpha")  # no thread_id
        _make_state_dir(self.root, "beta")
        r = route_message(state_root=self.root, bus_thread_id="T1", body="@mini ping")
        self.assertEqual(r.decision, "no_match")

    def test_unbound_thread_body_names_one_goal_returns_first_inbound_bind(self) -> None:
        _make_state_dir(self.root, "alpha")
        _make_state_dir(self.root, "beta")
        r = route_message(
            state_root=self.root, bus_thread_id="T-new", body="@mini please nudge `alpha`",
        )
        self.assertEqual(r.decision, "first_inbound_bind")
        self.assertEqual(r.goal_id, "alpha")
        self.assertEqual(r.state_dir, self.root / "alpha")

    def test_unbound_thread_body_names_two_goals_returns_ambiguous(self) -> None:
        _make_state_dir(self.root, "alpha")
        _make_state_dir(self.root, "beta")
        r = route_message(
            state_root=self.root, bus_thread_id="T-new",
            body="route this between `alpha` and `beta` please",
        )
        self.assertEqual(r.decision, "ambiguous")
        self.assertIsNone(r.goal_id)
        self.assertEqual(set(r.candidates), {"alpha", "beta"})

    def test_bound_thread_takes_precedence_over_body_naming(self) -> None:
        """Once bound, the thread is sticky — body naming a different
        live goal does NOT re-route."""
        _make_state_dir(self.root, "alpha", thread_id="T1")
        _make_state_dir(self.root, "beta")
        r = route_message(
            state_root=self.root, bus_thread_id="T1",
            body="@mini please nudge `beta` instead",
        )
        self.assertEqual(r.decision, "bound")
        self.assertEqual(r.goal_id, "alpha")

    def test_collision_when_two_state_dirs_share_thread_id(self) -> None:
        """Defensive: bind_thread enforces 1:1, but if the filesystem
        somehow has duplicates, route_message must report it instead of
        silently picking one."""
        _make_state_dir(self.root, "alpha", thread_id="T1")
        _make_state_dir(self.root, "beta", thread_id="T1")
        r = route_message(state_root=self.root, bus_thread_id="T1", body="hi")
        self.assertEqual(r.decision, "collision")
        self.assertEqual(set(r.candidates), {"alpha", "beta"})
        self.assertIsNone(r.goal_id)

    def test_goal_id_match_is_token_based_not_substring(self) -> None:
        """Goal 'abc' must not match a body containing 'abc-extended'
        (substring trap). Token boundary required."""
        _make_state_dir(self.root, "abc")
        _make_state_dir(self.root, "abc-extended")
        r = route_message(
            state_root=self.root, bus_thread_id="T-new",
            body="nudge `abc-extended` please",
        )
        self.assertEqual(r.decision, "first_inbound_bind")
        self.assertEqual(r.goal_id, "abc-extended")

    # ----- B3: sole-session auto-bind (added 2026-05-19) ----------------
    # UX motivation: a 40-char goal_id is impossible to type in Discord.
    # When exactly one unbound Mini session exists, `@mini ping` should
    # route to it without forcing the operator to name it explicitly.

    def test_sole_unbound_session_auto_binds(self) -> None:
        """One unbound session + body with no goal_id → bind to that session."""
        _make_state_dir(self.root, "smoke-test-inbound-respond-pong-4bd0f1a4")
        r = route_message(
            state_root=self.root, bus_thread_id="T-new", body="@mini ping",
        )
        self.assertEqual(r.decision, "first_inbound_bind")
        self.assertEqual(r.goal_id, "smoke-test-inbound-respond-pong-4bd0f1a4")

    def test_sole_session_already_bound_does_not_poach_new_thread(self) -> None:
        """A session bound to T1 must NOT auto-bind to a new thread T2.
        Without this guard, the second user typing `@mini ping` in a
        different Discord channel would hijack the original Mini session."""
        _make_state_dir(self.root, "alpha", thread_id="T1")
        r = route_message(
            state_root=self.root, bus_thread_id="T2", body="@mini ping",
        )
        self.assertEqual(r.decision, "no_match")
        self.assertIsNone(r.goal_id)

    def test_two_unbound_sessions_no_body_match_stays_no_match(self) -> None:
        """Auto-bind only fires when the unbound count is exactly 1.
        Two unbound sessions and no body disambiguation → still no_match
        (regression guard for the existing behavior)."""
        _make_state_dir(self.root, "alpha")
        _make_state_dir(self.root, "beta")
        r = route_message(
            state_root=self.root, bus_thread_id="T-new", body="@mini ping",
        )
        self.assertEqual(r.decision, "no_match")

    def test_one_bound_one_unbound_routes_to_unbound_on_new_thread(self) -> None:
        """alpha bound to T1, beta unbound. Message on T2 → bind to beta."""
        _make_state_dir(self.root, "alpha", thread_id="T1")
        _make_state_dir(self.root, "beta")
        r = route_message(
            state_root=self.root, bus_thread_id="T2", body="@mini ping",
        )
        self.assertEqual(r.decision, "first_inbound_bind")
        self.assertEqual(r.goal_id, "beta")

    def test_sole_session_body_names_it_still_binds(self) -> None:
        """Naming the sole session explicitly is still valid — same outcome
        as auto-bind, but the route_message returns it via the body-named
        path (step 2), not the sole-session path (step 3)."""
        _make_state_dir(self.root, "alpha")
        r = route_message(
            state_root=self.root, bus_thread_id="T-new",
            body="@mini please nudge alpha",
        )
        self.assertEqual(r.decision, "first_inbound_bind")
        self.assertEqual(r.goal_id, "alpha")


class TestBindThread(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_writes_atomically_with_mode_600(self) -> None:
        sd = _make_state_dir(self.root, "alpha")
        bind_thread(state_root=self.root, state_dir=sd, bus_thread_id="T1")
        f = sd / "thread_id"
        self.assertTrue(f.is_file())
        self.assertEqual(f.read_text().strip(), "T1")
        mode = stat.S_IMODE(f.stat().st_mode)
        self.assertEqual(mode, 0o600, f"expected mode 600, got {oct(mode)}")

    def test_idempotent_for_same_value(self) -> None:
        sd = _make_state_dir(self.root, "alpha", thread_id="T1")
        # Should not raise; should leave the file unchanged.
        bind_thread(state_root=self.root, state_dir=sd, bus_thread_id="T1")
        self.assertEqual((sd / "thread_id").read_text().strip(), "T1")

    def test_raises_on_different_existing_value(self) -> None:
        sd = _make_state_dir(self.root, "alpha", thread_id="T1")
        with self.assertRaises(BoundThreadMismatch):
            bind_thread(state_root=self.root, state_dir=sd, bus_thread_id="T2")
        # File must be untouched.
        self.assertEqual((sd / "thread_id").read_text().strip(), "T1")

    def test_raises_when_thread_already_claimed_by_other_goal(self) -> None:
        sd_a = _make_state_dir(self.root, "alpha", thread_id="T1")
        sd_b = _make_state_dir(self.root, "beta")
        with self.assertRaises(ThreadAlreadyClaimed):
            bind_thread(state_root=self.root, state_dir=sd_b, bus_thread_id="T1")
        # beta must remain unbound; AC2 invariant holds.
        self.assertFalse((sd_b / "thread_id").exists())
        # alpha untouched.
        self.assertEqual((sd_a / "thread_id").read_text().strip(), "T1")

    def test_no_tmp_file_left_behind(self) -> None:
        sd = _make_state_dir(self.root, "alpha")
        bind_thread(state_root=self.root, state_dir=sd, bus_thread_id="T1")
        leftovers = [p for p in sd.iterdir() if p.name.startswith("thread_id.")]
        self.assertEqual(leftovers, [], f"tmp files left behind: {leftovers}")


class TestRouteResultType(unittest.TestCase):
    def test_route_result_is_frozen(self) -> None:
        r = RouteResult(decision="no_match", goal_id=None, state_dir=None, candidates=())
        with self.assertRaises((AttributeError, Exception)):
            r.decision = "bound"  # type: ignore[misc]


if __name__ == "__main__":
    unittest.main()
