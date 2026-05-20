"""Tests for services/mini-mcp/server.py — spec 050 slice 1.

Covers:
  R1 — mini_open accepts spec references (number / slug / range)
  R3 — missing or ambiguous references are a hard error, no spawn
  R2 — the composed /goal references every resolved spec.md

The mini CLI shell-out (_run_mini) is monkeypatched so no real session
is spawned. Spec resolution runs against the actual .specify/specs/
tree in this repo.
"""
from __future__ import annotations

import importlib.util
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path

_SERVER_PATH = Path(__file__).resolve().parents[1] / "server.py"
_spec = importlib.util.spec_from_loader(
    "mini_mcp_server", SourceFileLoader("mini_mcp_server", str(_SERVER_PATH))
)
server = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(server)


class SpecResolutionTests(unittest.TestCase):
    """R1/R3 — reference forms and error paths."""

    def test_bare_number_resolves(self) -> None:
        dirs = server._resolve_spec_ref("039")
        self.assertEqual([d.name for d in dirs], ["039-mini-discord-inbound"])

    def test_exact_slug_resolves(self) -> None:
        dirs = server._resolve_spec_ref("050-mini-mcp-spec-dispatch")
        self.assertEqual([d.name for d in dirs], ["050-mini-mcp-spec-dispatch"])

    def test_ascending_range_expands(self) -> None:
        dirs = server._resolve_spec_ref("037-039")
        self.assertEqual(
            [d.name for d in dirs],
            [
                "037-sw-011-heartbeat-proof-tests",
                "038-octi-persistent-claude-session",
                "039-mini-discord-inbound",
            ],
        )

    def test_missing_number_is_hard_error(self) -> None:
        with self.assertRaises(ValueError) as ctx:
            server._resolve_spec_ref("999")
        self.assertIn("999", str(ctx.exception))

    def test_descending_range_is_hard_error(self) -> None:
        with self.assertRaises(ValueError) as ctx:
            server._resolve_spec_ref("042-039")
        self.assertIn("descending", str(ctx.exception))

    def test_ambiguous_number_is_hard_error(self) -> None:
        """036 has two spec dirs — a bare '036' must not silently pick one."""
        with self.assertRaises(ValueError) as ctx:
            server._resolve_spec_ref("036")
        self.assertIn("ambiguous", str(ctx.exception))

    def test_garbage_reference_is_hard_error(self) -> None:
        with self.assertRaises(ValueError):
            server._resolve_spec_ref("not-a-spec")

    def test_range_with_missing_endpoint_is_hard_error(self) -> None:
        """A range that includes a non-existent spec fails — no partial set."""
        with self.assertRaises(ValueError):
            server._resolve_spec_ref("998-999")


class MiniOpenTests(unittest.TestCase):
    """R1/R2/R3 — mini_open behavior with the CLI shell-out mocked."""

    def setUp(self) -> None:
        self._calls: list[tuple] = []
        self._orig = server._run_mini

        def fake_run_mini(*args: str, timeout: int = 30) -> dict:
            self._calls.append(args)
            return {"goal_id": "fake-goal-123", "state_dir": "/tmp/fake"}

        server._run_mini = fake_run_mini  # type: ignore[assignment]

    def tearDown(self) -> None:
        server._run_mini = self._orig  # type: ignore[assignment]

    def test_empty_specs_rejected_no_spawn(self) -> None:
        with self.assertRaises(ValueError):
            server.mini_open(specs=[])
        self.assertEqual(self._calls, [])  # nothing spawned

    def test_missing_spec_rejected_before_spawn(self) -> None:
        """R3 — resolution fails before _run_mini is ever called."""
        with self.assertRaises(ValueError):
            server.mini_open(specs=["039", "999"])
        self.assertEqual(self._calls, [])

    def test_composed_goal_references_every_spec(self) -> None:
        """R2 — the goal text names each resolved spec.md path + title."""
        result = server.mini_open(specs=["038", "039"])
        self.assertEqual(len(self._calls), 1)
        args = self._calls[0]
        self.assertEqual(args[0], "open")
        self.assertEqual(args[1], "--goal")
        goal = args[2]
        self.assertIn(".specify/specs/038-octi-persistent-claude-session/spec.md", goal)
        self.assertIn(".specify/specs/039-mini-discord-inbound/spec.md", goal)
        self.assertIn("in order", goal)
        # result echoes the resolved spec dir names
        self.assertEqual(
            result["specs"],
            ["038-octi-persistent-claude-session", "039-mini-discord-inbound"],
        )

    def test_duplicate_specs_deduped(self) -> None:
        """Boundary case 3 — duplicate refs collapse, one entry in the goal."""
        result = server.mini_open(specs=["039", "039"])
        self.assertEqual(result["specs"], ["039-mini-discord-inbound"])
        goal = self._calls[0][2]
        self.assertEqual(goal.count("039-mini-discord-inbound/spec.md"), 1)

    def test_invoked_by_defaults_to_mcp(self) -> None:
        result = server.mini_open(specs=["039"])
        self.assertEqual(result["invoked_by"], "mcp")

    def test_invoked_by_explicit_is_kept(self) -> None:
        result = server.mini_open(specs=["039"], invoked_by="ares")
        self.assertEqual(result["invoked_by"], "ares")

    def test_range_in_specs_list(self) -> None:
        result = server.mini_open(specs=["037-039"])
        self.assertEqual(
            result["specs"],
            [
                "037-sw-011-heartbeat-proof-tests",
                "038-octi-persistent-claude-session",
                "039-mini-discord-inbound",
            ],
        )


class ToolCatalogTests(unittest.TestCase):
    def test_mini_open_schema_requires_specs_not_goal(self) -> None:
        tool = next(t for t in server.TOOLS if t["name"] == "mini_open")
        props = tool["inputSchema"]["properties"]
        self.assertIn("specs", props)
        self.assertNotIn("goal", props)
        self.assertEqual(tool["inputSchema"]["required"], ["specs"])

    def test_all_five_tools_present(self) -> None:
        names = {t["name"] for t in server.TOOLS}
        self.assertEqual(
            names,
            {"mini_open", "mini_nudge", "mini_status", "mini_stop", "mini_list"},
        )


if __name__ == "__main__":
    unittest.main()
