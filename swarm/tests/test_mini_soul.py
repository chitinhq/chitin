"""Tests for swarm.mini._internal.soul — soul resolution and rendering."""

from __future__ import annotations

import hashlib
import os
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
sys_path_patched = str(REPO) not in os.environ.get("PYTHONPATH", "")

if sys_path_patched:
    os.environ["PYTHONPATH"] = str(REPO) + ":" + os.environ.get("PYTHONPATH", "")

import sys

if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.mini._internal.soul import (
    EMPTY_SOUL,
    SoulConfig,
    find_soul_file,
    render_soul_section,
    resolve_soul_for_session,
    souls_dir_candidates,
)


class TestSoulConfig(unittest.TestCase):
    def test_empty_soul_is_empty(self):
        self.assertTrue(EMPTY_SOUL.is_empty)
        self.assertEqual(EMPTY_SOUL.soul_id, "")
        self.assertEqual(EMPTY_SOUL.soul_hash, "")
        self.assertEqual(EMPTY_SOUL.soul_body, "")

    def test_non_empty_soul_is_not_empty(self):
        s = SoulConfig(soul_id="knuth", soul_hash="abc123", soul_body="some text")
        self.assertFalse(s.is_empty)


class TestFindSoulFile(unittest.TestCase):
    def setUp(self):
        self._old_env = os.environ.get("CHITIN_SOULS_DIR")
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        # Create a test soul file
        soul_dir = Path(self.tmp.name) / "canonical"
        soul_dir.mkdir(parents=True)
        (soul_dir / "knuth.md").write_text("---\narchetype: knuth\n---\nKnuth body")
        (soul_dir / "davinci.md").write_text("---\narchetype: davinci\n---\nDa Vinci body")

    def tearDown(self):
        if self._old_env is not None:
            os.environ["CHITIN_SOULS_DIR"] = self._old_env
        else:
            os.environ.pop("CHITIN_SOULS_DIR", None)

    def test_finds_soul_in_env_dir(self):
        os.environ["CHITIN_SOULS_DIR"] = self.tmp.name
        result = find_soul_file("knuth")
        self.assertIsNotNone(result)
        self.assertTrue(str(result).endswith("knuth.md"))

    def test_returns_none_for_missing_soul(self):
        os.environ["CHITIN_SOULS_DIR"] = self.tmp.name
        result = find_soul_file("nonexistent")
        self.assertIsNone(result)

    def test_checks_canonical_before_experimental(self):
        exp = Path(self.tmp.name) / "experimental"
        exp.mkdir()
        (exp / "knuth.md").write_text("---\narchetype: knuth\nstatus: provisional\n---\nExperimental knuth")
        os.environ["CHITIN_SOULS_DIR"] = self.tmp.name
        result = find_soul_file("knuth")
        self.assertIsNotNone(result)
        # Should find the canonical version first
        self.assertIn("canonical", str(result))


class TestResolveSoulForSession(unittest.TestCase):
    def setUp(self):
        self._old_env = os.environ.get("CHITIN_SOULS_DIR")
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        # Create test soul files
        soul_dir = Path(self.tmp.name) / "canonical"
        soul_dir.mkdir(parents=True)
        body = "---\narchetype: knuth\nstatus: promoted\n---\n## Knuth heuristic"
        (soul_dir / "knuth.md").write_text(body)
        self._body = body

    def tearDown(self):
        if self._old_env is not None:
            os.environ["CHITIN_SOULS_DIR"] = self._old_env
        else:
            os.environ.pop("CHITIN_SOULS_DIR", None)

    def test_explicit_soul_id_resolves(self):
        os.environ["CHITIN_SOULS_DIR"] = self.tmp.name
        result = resolve_soul_for_session("knuth")
        self.assertEqual(result.soul_id, "knuth")
        self.assertEqual(result.soul_hash, hashlib.sha256(self._body.encode("utf-8")).hexdigest())
        self.assertIn("Knuth heuristic", result.soul_body)
        self.assertFalse(result.is_empty)

    def test_none_soul_returns_empty(self):
        result = resolve_soul_for_session(None)
        self.assertTrue(result.is_empty)

    def test_agent_default_resolves(self):
        os.environ["CHITIN_SOULS_DIR"] = self.tmp.name
        result = resolve_soul_for_session(None, agent="mini")
        # mini defaults to knuth
        self.assertEqual(result.soul_id, "knuth")

    def test_unknown_agent_no_default(self):
        result = resolve_soul_for_session(None, agent="unknown_agent")
        # No default for unknown agent
        self.assertTrue(result.is_empty)

    def test_soul_not_found_on_disk_returns_empty_body(self):
        os.environ["CHITIN_SOULS_DIR"] = "/nonexistent/path"
        result = resolve_soul_for_session("knuth")
        # soul_id is set but body and hash are empty
        self.assertEqual(result.soul_id, "knuth")
        self.assertEqual(result.soul_hash, "")
        self.assertEqual(result.soul_body, "")

    def test_explicit_overrides_agent_default(self):
        """If both soul and agent are given, explicit soul wins."""
        os.environ["CHITIN_SOULS_DIR"] = self.tmp.name
        # mini defaults to knuth, but we explicitly ask for knuth (same)
        result = resolve_soul_for_session("knuth", agent="mini")
        self.assertEqual(result.soul_id, "knuth")


class TestRenderSoulSection(unittest.TestCase):
    def test_empty_soul_returns_empty_string(self):
        self.assertEqual(render_soul_section(EMPTY_SOUL), "")

    def test_rendered_section_contains_soul_id(self):
        s = SoulConfig(
            soul_id="knuth",
            soul_hash="abc",
            soul_body="## Heuristics\n\n1. Prove it.",
        )
        rendered = render_soul_section(s)
        self.assertIn("knuth", rendered)
        self.assertIn("Heuristics", rendered)
        self.assertIn("cognitive lens", rendered)
        self.assertTrue(rendered.startswith("\n\n---"))


if __name__ == "__main__":
    unittest.main()