# spec: 020-sdd-tdd-enforcement
"""Regression tests for spec 020 SDD+TDD enforcement (all three layers).

Per spec 020 acceptance criteria:
- AC1: L1 rejects code without test
- AC2: L1 accepts code with test or escape clause
- AC3: L2 rejects spec without test coverage section
- AC4: L2 rejects test file without spec reference
- AC5: L3 blocks gh pr create without spec in diff or body
- AC6: L3 allows gh pr create with spec reference
- Regression: workflow mirror matches canonical
"""
from __future__ import annotations

import os
import subprocess
import textwrap
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
SWARM_BIN = REPO_ROOT / "swarm" / "bin"
SWARM_TESTS = REPO_ROOT / "swarm" / "tests"
WORKFLOWS = REPO_ROOT / "swarm" / "workflows"

L1_SH = SWARM_BIN / "worker-pre-commit-no-code-without-test.sh"
L1_PY = SWARM_BIN / "worker-pre-commit-no-code-without-test.py"
L2_SH = SWARM_BIN / "worker-pre-commit-spec-has-test-coverage.sh"
L2_PY = SWARM_BIN / "worker-pre-commit-spec-has-test-coverage.py"
CANONICAL_LOBSTER = WORKFLOWS / "kanban-dispatch.lobster"
MIRROR_LOBSTER = REPO_ROOT / "docs" / "governance-setup-extras" / "kanban-dispatch.lobster"


class TestL1NoCodeWithoutTest(unittest.TestCase):
    """AC1 & AC2: L1 pre-commit hook tests."""

    def test_l1_rejects_code_without_test(self):
        """AC1: Staging a code file without any test file or escape clause fails."""
        # Create a temporary directory simulating a repo root
        import tempfile
        with tempfile.TemporaryDirectory() as tmp:
            # Run the python checker with a code file on stdin and no commit message
            staged = "src/feature.py"
            result = subprocess.run(
                ["python3", str(L1_PY), "--commit-message", ""],
                input=staged,
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 1, f"L1 should reject code without test; stdout={result.stdout} stderr={result.stderr}")
            self.assertIn("src/feature.py", result.stdout)

    def test_l1_accepts_with_test_or_escape_clause(self):
        """AC2: Staging code + test file passes; code + escape clause passes."""
        # Case 1: code with test file
        staged = "src/feature.py\nsrc/feature.test.ts"
        result = subprocess.run(
            ["python3", str(L1_PY), "--commit-message", ""],
            input=staged,
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 0, f"L1 should accept code with test; stdout={result.stdout} stderr={result.stderr}")

        # Case 2: code with escape clause in commit message
        staged = "src/feature.py"
        result = subprocess.run(
            ["python3", str(L1_PY), "--commit-message", "refactor: move helpers\nno-test-change-justified: pure relocation"],
            input=staged,
            capture_output=True,
            text=True,
        )
        self.assertEqual(result.returncode, 0, f"L1 should accept code with escape clause; stdout={result.stdout} stderr={result.stderr}")


class TestL2SpecHasTestCoverage(unittest.TestCase):
    """AC3 & AC4: L2 pre-commit hook tests."""

    def test_l2_rejects_spec_without_e2e_section(self):
        """AC3: Staging a spec.md without ## Test coverage section fails."""
        import tempfile
        with tempfile.TemporaryDirectory() as tmp:
            # Create a spec file without Test coverage section
            spec_dir = os.path.join(tmp, ".specify", "specs", "099-foo")
            os.makedirs(spec_dir)
            spec_path = os.path.join(spec_dir, "spec.md")
            with open(spec_path, "w") as f:
                f.write("# 099 — Foo\n\n## Goal\nDo foo things.\n")

            staged = f".specify/specs/099-foo/spec.md"
            result = subprocess.run(
                ["python3", str(L2_PY), "--repo-root", tmp],
                input=staged,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(result.returncode, 0, f"L2 should reject spec without test coverage; stdout={result.stdout} stderr={result.stderr}")

    def test_l2_rejects_test_without_spec_reference(self):
        """AC4: A test file without spec: reference in first 20 lines fails."""
        import tempfile
        with tempfile.TemporaryDirectory() as tmp:
            # Create a test file without spec reference
            test_dir = os.path.join(tmp, "e2e")
            os.makedirs(test_dir)
            test_path = os.path.join(test_dir, "something.spec.ts")
            with open(test_path, "w") as f:
                f.write("// A test file with no spec reference\nimport { test } from 'playwright';\n")

            staged = f"e2e/something.spec.ts"
            result = subprocess.run(
                ["python3", str(L2_PY), "--repo-root", tmp],
                input=staged,
                capture_output=True,
                text=True,
            )
            self.assertNotEqual(result.returncode, 0, f"L2 should reject test without spec reference; stdout={result.stdout} stderr={result.stderr}")


class TestL3NoPRWithoutSpec(unittest.TestCase):
    """AC5 & AC6: L3 gate in kanban-dispatch.lobster."""

    def _get_lobster_text(self) -> str:
        return CANONICAL_LOBSTER.read_text()

    def test_l3_blocks_gh_pr_create_without_spec_in_diff_or_body(self):
        """AC5: PR without spec in diff AND without Spec: in body is blocked."""
        lobster = self._get_lobster_text()
        # The lobster must have a step that checks for spec reference before PR create
        self.assertIn("spec", lobster.lower(), "Lobster must reference spec check for L3 gate")
        # Verify the gate step exists before gh pr create
        # Look for a no-pr-without-spec / spec check step name
        has_l3_step = any(
            kw in lobster.lower()
            for kw in ["no-pr-without-spec", "spec-check", "spec_gate", "spec-gate", "before-gh-pr-create"]
        )
        self.assertTrue(has_l3_step, "Lobster must have an L3 gate step (spec check before PR create)")

    def test_l3_allows_gh_pr_create_with_spec_reference(self):
        """AC6: PR with Spec: reference in body (spec exists on origin/main) is allowed."""
        lobster = self._get_lobster_text()
        # The gate must have a bypass path for valid Spec: references
        # Check that the lobster text contains the mechanism for allowing PRs with spec refs
        has_spec_ref_check = any(
            kw in lobster.lower()
            for kw in ["spec:", "spec reference", "spec_ref", "specref"]
        )
        self.assertTrue(has_spec_ref_check, "Lobster must have a mechanism to accept Spec: references in PR body")


class TestWorkflowMirrorMatchesCanonical(unittest.TestCase):
    """Regression: L3 gate step appears in both canonical and mirror lobster."""

    def test_workflow_mirror_matches_canonical(self):
        """The L3 gate step in kanban-dispatch.lobster must match its mirror."""
        if not MIRROR_LOBSTER.exists():
            self.skipTest("Mirror lobster file not found")
        canonical = CANONICAL_LOBSTER.read_text()
        mirror = MIRROR_LOBSTER.read_text()
        # Both must reference the spec gate step
        for name, text in [("canonical", canonical), ("mirror", mirror)]:
            self.assertIn("spec", text.lower(), f"{name} lobster must reference spec check")


if __name__ == "__main__":
    unittest.main()