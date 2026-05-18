"""Spec 024 — active-repo doc-bundle contract regression tests.

# spec: 024-active-repo-doc-bundle

Static analysis + integration tests against the check script
behavior. Uses a temp fixture workspace so the tests don't depend
on the operator's actual checkout.
"""
from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CHECK_SCRIPT = ROOT / "swarm" / "bin" / "check-active-repo-docs.sh"
SPEC = ROOT / ".specify" / "specs" / "024-active-repo-doc-bundle" / "spec.md"
CONSTITUTION = ROOT / ".specify" / "constitution.md"


def _write_fixture_roadmap(path: Path, repos: list[str]) -> None:
    """Write a minimal workspace roadmap.md with the 'truly-active' section."""
    rows = "\n".join(
        f"| `{slug}` | hot | red | x | 0 | x | - |" for slug in repos
    )
    path.write_text(
        f"# Workspace Roadmap — test fixture\n\n"
        f"## The {len(repos)} truly-active repos\n\n"
        f"| Repo | Hot? | Owner lane | Current focus | Open PRs | Next milestone | Blockers |\n"
        f"|------|------|------------|---------------|----------|----------------|----------|\n"
        f"{rows}\n\n"
        f"## Some other section\n\nignore me\n"
    )


def _write_bundle_piece(repo_dir: Path, piece: str) -> None:
    """Create a minimal bundle piece in repo_dir."""
    paths = {
        "README.md": repo_dir / "README.md",
        "AGENTS.md": repo_dir / "AGENTS.md",
        "CLAUDE.md": repo_dir / "CLAUDE.md",
        "docs/roadmap.md": repo_dir / "docs" / "roadmap.md",
        ".specify/specs/INDEX.md": repo_dir / ".specify" / "specs" / "INDEX.md",
    }
    p = paths[piece]
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(f"# {piece}\n\ntest fixture\n")


def _write_full_bundle(repo_dir: Path) -> None:
    """All 4 pieces (using AGENTS.md form)."""
    for piece in ["README.md", "AGENTS.md", "docs/roadmap.md", ".specify/specs/INDEX.md"]:
        _write_bundle_piece(repo_dir, piece)


class CheckScriptTests(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="spec024-"))
        self.workspace = self.tmp / "workspace"
        self.workspace.mkdir()
        self.roadmap = self.workspace / "roadmap.md"
        # Patch out gh dependency for unit tests — env wrapper bypasses
        # the archive cross-check (real archive check tested separately)
        self.env = {
            **os.environ,
            "WORKSPACE_ROADMAP": str(self.roadmap),
            "WORKSPACE_ROOT": str(self.workspace),
            "PATH": str(self.tmp / "bin") + os.pathsep + os.environ["PATH"],
        }
        # Mock gh: respect --jq '.isArchived' by emitting just the boolean.
        # The real check script calls gh with --jq so it expects raw values.
        bindir = self.tmp / "bin"
        bindir.mkdir()
        mock_gh = bindir / "gh"
        mock_gh.write_text('#!/usr/bin/env bash\necho false\nexit 0\n')
        mock_gh.chmod(0o755)

    def _run(self):
        return subprocess.run(
            ["bash", str(CHECK_SCRIPT)],
            env=self.env, capture_output=True, text=True,
        )

    def test_check_script_exists_and_is_executable(self):
        self.assertTrue(CHECK_SCRIPT.exists())
        self.assertTrue(os.access(CHECK_SCRIPT, os.X_OK))

    def test_all_active_repos_have_bundle_pieces(self):
        """AC1: passes when every active repo has the full bundle."""
        _write_fixture_roadmap(self.roadmap, ["chitinhq/foo", "chitinhq/bar"])
        for r in ["foo", "bar"]:
            _write_full_bundle(self.workspace / r)
        res = self._run()
        self.assertEqual(res.returncode, 0,
                         f"expected pass; stdout={res.stdout!r} stderr={res.stderr!r}")
        self.assertIn("OK: all active repos", res.stdout)

    def test_check_script_exits_nonzero_when_piece_missing(self):
        """AC2: nonzero when any bundle piece is missing."""
        _write_fixture_roadmap(self.roadmap, ["chitinhq/foo"])
        foo = self.workspace / "foo"
        foo.mkdir()
        # only 3 of 4 pieces
        for piece in ["README.md", "AGENTS.md", "docs/roadmap.md"]:
            _write_bundle_piece(foo, piece)
        res = self._run()
        self.assertEqual(res.returncode, 1, f"expected exit 1; got {res.returncode}: {res.stderr!r}")
        self.assertIn("MISS: chitinhq/foo missing .specify/specs/INDEX.md", res.stderr)

    def test_check_script_lists_new_active_repo_missing_bundle(self):
        """AC3: a newly-declared active repo without bundle fails — the fix is add the bundle."""
        _write_fixture_roadmap(self.roadmap, ["chitinhq/foo", "chitinhq/brand-new"])
        _write_full_bundle(self.workspace / "foo")
        # brand-new exists but is bare
        (self.workspace / "brand-new").mkdir()
        res = self._run()
        self.assertEqual(res.returncode, 1)
        # Should name brand-new specifically
        self.assertIn("brand-new missing", res.stderr)

    def test_check_script_flags_archived_listed_as_active(self):
        """AC4: archived repo listed as active → exit 3 (mismatch)."""
        # Re-mock gh to return true
        bindir = self.tmp / "bin"
        mock_gh = bindir / "gh"
        mock_gh.write_text('#!/usr/bin/env bash\necho true\nexit 0\n')
        mock_gh.chmod(0o755)
        _write_fixture_roadmap(self.roadmap, ["chitinhq/foo"])
        _write_full_bundle(self.workspace / "foo")
        res = self._run()
        self.assertEqual(res.returncode, 3, f"expected exit 3; got {res.returncode}: {res.stderr!r}")
        self.assertIn("GitHub-archived but listed as active", res.stderr)

    def test_check_script_accepts_claude_md_in_lieu_of_agents_md(self):
        """The bundle accepts AGENTS.md OR CLAUDE.md per the spec — bench-devs uses CLAUDE.md."""
        _write_fixture_roadmap(self.roadmap, ["chitinhq/foo"])
        foo = self.workspace / "foo"
        for piece in ["README.md", "CLAUDE.md", "docs/roadmap.md", ".specify/specs/INDEX.md"]:
            _write_bundle_piece(foo, piece)
        res = self._run()
        self.assertEqual(res.returncode, 0,
                         f"CLAUDE.md alone should satisfy the agents-doc piece; got {res.returncode}: {res.stderr!r}")


class ConstitutionAmendmentTests(unittest.TestCase):
    """The §1.3 amendment lives in chitinhq/workspace/.specify/constitution.md
    (the workspace constitution that chitin's overlay extends). This test
    skips when run from chitin (where the amendment doesn't belong);
    a parallel test in chitinhq/workspace will check the workspace
    constitution has §1.3. Tracked in the spec 024 implementation PRs."""

    def test_chitin_overlay_does_not_need_section_1_3(self):
        # Documenting the cross-spec coupling. The chitin overlay extends
        # the workspace constitution; it doesn't repeat workspace-level
        # rules. §1.3 is a workspace-level rule (active across ALL active
        # repos), so it lives there — not here.
        if not CONSTITUTION.exists():
            self.skipTest("chitin constitution overlay not present")
        text = CONSTITUTION.read_text()
        # Asserts the chitin overlay continues to NOT carry §1.3 (it would
        # be a duplicate). If a future contributor adds it here, this test
        # flags the duplication.
        if "§1.3" in text and "active-repo doc-bundle" in text.lower():
            self.fail(
                "Chitin overlay duplicates §1.3 active-repo doc-bundle; "
                "this rule belongs in the workspace constitution only."
            )


class SpecShapeTests(unittest.TestCase):
    def test_spec_has_test_coverage_section(self):
        """Spec 024 itself must carry the ## Test coverage section (spec 020 §1.2 contract)."""
        text = SPEC.read_text()
        self.assertIn("## Test coverage", text)
        # AC table with at least 3 entries
        self.assertGreaterEqual(text.count("`test_"), 3)


if __name__ == "__main__":
    unittest.main()
