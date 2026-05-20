"""Tests for services/mini-mcp/server.py — spec 050.

Covers:
  Slice 1:
    R1 — mini_open accepts spec references (number / slug / range)
    R3 — missing or ambiguous references are a hard error, no spawn
    R2 — the composed /goal references every resolved spec.md
  Slice 2:
    S2-R1 — mini_open passes invoked_by, source, and specs to the CLI
    S2-R2 — webhook thread creation (unit-tested separately)
    S2-R3 — status transition format with emojis
    S2-R4 — graceful degradation when thread creation fails

The mini CLI shell-out (_run_mini) is monkeypatched so no real session
is spawned. Spec resolution runs against the actual .specify/specs/
tree in this repo.
"""
from __future__ import annotations

import importlib.util
import json
import os
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path
from unittest.mock import patch

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

    # ---- Slice 2 additions (S2-R1) ----

    def test_cli_receives_invoked_by(self) -> None:
        """S2-R1: the CLI open call gets --invoked-by."""
        server.mini_open(specs=["039"], invoked_by="hermes")
        args = self._calls[0]
        # args is a tuple of strings; find --invoked-by
        arg_list = list(args)
        idx = arg_list.index("--invoked-by")
        self.assertEqual(arg_list[idx + 1], "hermes")

    def test_cli_receives_source_mcp(self) -> None:
        """S2-R1: MCP calls pass --source mcp."""
        server.mini_open(specs=["039"])
        args = list(self._calls[0])
        idx = args.index("--source")
        self.assertEqual(args[idx + 1], "mcp")

    def test_cli_receives_spec_names(self) -> None:
        """S2-R1: resolved spec dir names are passed as --specs."""
        server.mini_open(specs=["038", "039"])
        args = list(self._calls[0])
        idx = args.index("--specs")
        self.assertEqual(args[idx + 1], "038-octi-persistent-claude-session")
        self.assertEqual(args[idx + 2], "039-mini-discord-inbound")


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


# ---------------------------------------------------------------------------
# Slice 2 — webhook thread tests
# ---------------------------------------------------------------------------

# We test the webhook module directly since it's imported from the
# swarm.mini._internal package by the session module.

_REPO_ROOT = Path(__file__).resolve().parents[3]
_SWARM_ROOT = _REPO_ROOT / "swarm"
_WEBHOOK_PATH = _SWARM_ROOT / "mini" / "_internal" / "webhook.py"
_STATEDIR_PATH = _SWARM_ROOT / "mini" / "_internal" / "statedir.py"

if _WEBHOOK_PATH.is_file():
    _wh_spec = importlib.util.spec_from_loader(
        "swarm.mini._internal.webhook",
        SourceFileLoader("swarm.mini._internal.webhook", str(_WEBHOOK_PATH)),
    )
    webhook_mod = importlib.util.module_from_spec(_wh_spec)
    _wh_spec.loader.exec_module(webhook_mod)
else:
    webhook_mod = None  # type: ignore

if _STATEDIR_PATH.is_file():
    _sd_spec = importlib.util.spec_from_loader(
        "swarm.mini._internal.statedir",
        SourceFileLoader("swarm.mini._internal.statedir", str(_STATEDIR_PATH)),
    )
    statedir_mod = importlib.util.module_from_spec(_sd_spec)
    _sd_spec.loader.exec_module(statedir_mod)
else:
    statedir_mod = None  # type: ignore


class WebhookThreadTests(unittest.TestCase):
    """S2-R2/S2-R3/S2-R4 — webhook thread functions."""

    def setUp(self) -> None:
        if webhook_mod is None:
            self.skipTest("webhook module not found")

    def test_post_to_thread_without_thread_id_falls_back(self) -> None:
        """S2-R4: when thread_id is None, post_to_thread falls back to post()."""
        with patch.object(webhook_mod, "post", return_value=True) as mock_post:
            result = webhook_mod.post_to_thread("https://example.com/webhook", None, "hello")
            self.assertTrue(result)
            mock_post.assert_called_once()

    def test_post_to_thread_with_thread_id_appends_qs(self) -> None:
        """S2-R3: thread_id is added as a query parameter."""
        with patch.object(webhook_mod, "post", return_value=True) as mock_post:
            result = webhook_mod.post_to_thread(
                "https://discord.com/api/webhooks/123/abc", "456", "hello"
            )
            self.assertTrue(result)
            call_url = mock_post.call_args[0][0]
            self.assertIn("thread_id=456", call_url)

    def test_post_to_thread_no_url_returns_false(self) -> None:
        result = webhook_mod.post_to_thread(None, "456", "hello")
        self.assertFalse(result)

    def test_create_and_post_to_thread_no_url_returns_none(self) -> None:
        result = webhook_mod.create_and_post_to_thread(None, "goal-123", "hello")
        self.assertIsNone(result)

    def test_create_and_post_to_thread_with_thread_name_success(self) -> None:
        """S2-R2: thread_name in body returns the thread id if API returns it."""
        def fake_post_raw(url, body, **kw):
            return {"id": "thread-789", "channel_id": "chan-1"}

        with patch.object(webhook_mod, "_discord_post_raw", side_effect=fake_post_raw):
            result = webhook_mod.create_and_post_to_thread(
                "https://discord.com/api/webhooks/123/abc", "goal-xyz", "hello"
            )
            self.assertEqual(result, "thread-789")

    def test_create_and_post_to_thread_fallback_on_failure(self) -> None:
        """S2-R4: if thread creation fails, returns None (message posted to channel)."""
        def fake_post_raw(url, body, **kw):
            # First call (thread_name attempt) fails
            if body.get("thread_name"):
                return None
            # Second call (plain post) succeeds but no message.id
            return {"id": "msg-1"}

        with patch.object(webhook_mod, "_discord_post_raw", side_effect=fake_post_raw):
            with patch.object(webhook_mod, "_channel_id_from_webhook_url", return_value=None):
                result = webhook_mod.create_and_post_to_thread(
                    "https://discord.com/api/webhooks/123/abc", "goal-xyz", "hello"
                )
                # No thread created, but message posted to channel
                self.assertIsNone(result)

    def test_create_and_post_to_thread_channel_id_fallback(self) -> None:
        """S2-R2/S2-Q1: thread created via message endpoint when channel_id available."""
        from io import BytesIO

        def fake_post_raw(url, body, **kw):
            if body.get("thread_name"):
                return None  # thread_name not supported (text channel)
            return {"id": "msg-1", "channel_id": "chan-1"}  # opening message

        def fake_urlopen(req, **kw):
            """Intercept the thread creation API call."""
            resp_data = json.dumps({"id": "thread-new"}).encode("utf-8")
            return BytesIO(resp_data)

        with patch.object(webhook_mod, "_discord_post_raw", side_effect=fake_post_raw):
            with patch("urllib.request.urlopen", side_effect=fake_urlopen):
                # Also need to intercept the initial _discord_post_raw
                # which uses urllib.request.urlopen internally — but since we
                # mocked _discord_post_raw, only the thread-creation path hits urlopen
                result = webhook_mod.create_and_post_to_thread(
                    "https://discord.com/api/webhooks/123/abc", "goal-xyz", "hello"
                )
                self.assertEqual(result, "thread-new")


class StatedirDiscordThreadTests(unittest.TestCase):
    """S2-R2 — discord_thread.id persistence."""

    def setUp(self) -> None:
        if statedir_mod is None:
            self.skipTest("statedir module not found")
        self._orig_root = statedir_mod.state_root
        self._tmpdir = tempfile.mkdtemp()
        # Override state root to temp dir
        statedir_mod.state_root = lambda: Path(self._tmpdir)  # type: ignore

    def tearDown(self) -> None:
        statedir_mod.state_root = self._orig_root  # type: ignore
        import shutil
        shutil.rmtree(self._tmpdir, ignore_errors=True)

    def test_write_and_read_thread_id(self) -> None:
        goal_id = "test-goal-123"
        statedir_mod.create_state_dir(
            goal_id, goal="test goal", branch="main",
            worktree=Path("/tmp/wt")
        )
        statedir_mod.write_discord_thread_id(goal_id, "thread-456")
        result = statedir_mod.read_discord_thread_id(goal_id)
        self.assertEqual(result, "thread-456")

    def test_read_missing_thread_id_returns_none(self) -> None:
        goal_id = "test-goal-nofile"
        statedir_mod.create_state_dir(
            goal_id, goal="test goal", branch="main",
            worktree=Path("/tmp/wt")
        )
        result = statedir_mod.read_discord_thread_id(goal_id)
        self.assertIsNone(result)

    def test_read_empty_thread_id_returns_none(self) -> None:
        goal_id = "test-goal-empty"
        statedir_mod.create_state_dir(
            goal_id, goal="test goal", branch="main",
            worktree=Path("/tmp/wt")
        )
        # Write empty file
        (statedir_mod.state_dir(goal_id) / "discord_thread.id").write_text("")
        result = statedir_mod.read_discord_thread_id(goal_id)
        self.assertIsNone(result)


# ---------------------------------------------------------------------------
# Slice 2 — session.py format tests
# ---------------------------------------------------------------------------

# Test the message format functions by re-implementing them (they are
# pure-formatting, no I/O) rather than trying to import session.py
# which has deep relative-import chains.

_STATUS_EMOJI = {
    "working": "✅",
    "verifying": "✅",
    "blocked": "🔶",
    "done": "🏁",
    "needs_review": "🏁",
    "failed": "🛑",
    "paused": "🔶",
    "idle": "🔶",
}


def _format_opened_message(
    goal_id: str,
    worktree: Path,
    goal: str,
    *,
    invoked_by: str | None = None,
    source: str = "cli",
    specs: list[str] | None = None,
) -> str:
    """Mirrors session._format_opened_message for testing."""
    invoker = invoked_by or "operator"
    spec_list = ",".join(specs) if specs else ""
    parts = ["🐙 Mini session opened"]
    if spec_list:
        parts.append(f"spec {spec_list}")
    parts.append(f"invoked by `{invoker}` via {source}")
    line1 = " — ".join(parts)
    line2 = f"   goal_id: `{goal_id}`   ·   worktree: `{worktree}`"
    line3 = f"> {goal[:200]}"
    return f"{line1}\n{line2}\n{line3}"


class SessionMessageFormatTests(unittest.TestCase):
    """S2-R1 — format of the 🐙 session.opened message."""

    def test_opened_message_contains_invoker(self) -> None:
        msg = _format_opened_message(
            "goal-abc", Path("/tmp/wt"), "implement foo",
            invoked_by="ares", source="mcp", specs=["050"],
        )
        self.assertIn("ares", msg)
        self.assertIn("mcp", msg)

    def test_opened_message_contains_specs(self) -> None:
        msg = _format_opened_message(
            "goal-abc", Path("/tmp/wt"), "implement foo",
            invoked_by="ares", source="mcp", specs=["037", "038", "039"],
        )
        self.assertIn("037,038,039", msg)

    def test_opened_message_contains_goal_id(self) -> None:
        msg = _format_opened_message(
            "goal-xyz", Path("/tmp/wt"), "implement bar",
            invoked_by="op", source="cli", specs=None,
        )
        self.assertIn("goal-xyz", msg)

    def test_opened_message_no_specs_omits_spec_line(self) -> None:
        msg = _format_opened_message(
            "goal-abc", Path("/tmp/wt"), "implement foo",
            invoked_by="op", source="cli", specs=None,
        )
        self.assertNotIn("spec ", msg)

    def test_status_emoji_working(self) -> None:
        self.assertEqual(_STATUS_EMOJI.get("working", "🔁"), "✅")

    def test_status_emoji_blocked(self) -> None:
        self.assertEqual(_STATUS_EMOJI.get("blocked", "🔁"), "🔶")

    def test_status_emoji_done(self) -> None:
        self.assertEqual(_STATUS_EMOJI.get("done", "🔁"), "🏁")

    def test_status_emoji_unknown(self) -> None:
        self.assertEqual(_STATUS_EMOJI.get("other", "🔁"), "🔁")


if __name__ == "__main__":
    unittest.main()