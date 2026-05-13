#!/usr/bin/env python3
"""Behavior tests for scripts/regression-gate.sh."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
AGGREGATOR_SRC = REPO_ROOT / "scripts" / "regression-gate.sh"


def make_sandbox() -> Path:
    """Build a throwaway tree with a `scripts/` dir + a copy of the
    aggregator, then return the tree root. Callers add stub invariants
    into <tree>/scripts/ before running the aggregator."""
    tmp = Path(tempfile.mkdtemp(prefix="regression-gate-test-"))
    (tmp / "scripts").mkdir()
    shutil.copy(AGGREGATOR_SRC, tmp / "scripts" / "regression-gate.sh")
    (tmp / "scripts" / "regression-gate.sh").chmod(0o755)
    return tmp


def run_aggregator(sandbox: Path, timeout: int = 60) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["bash", "scripts/regression-gate.sh"],
        cwd=sandbox,
        capture_output=True,
        text=True,
        timeout=timeout,
    )


class EmptyRegistryTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def test_empty_registry_exits_zero(self) -> None:
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn("All 0 invariants preserved", result.stdout)


class PassingInvariantTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write_check(self, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"check-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_single_passing_invariant(self) -> None:
        self._write_check("ok", "#!/usr/bin/env bash\nexit 0\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn("PASS", result.stdout)
        self.assertIn("check-ok.sh", result.stdout)
        self.assertIn("All 1 invariants preserved", result.stdout)


class FailingInvariantTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write_check(self, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"check-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_single_failing_invariant(self) -> None:
        self._write_check("broken",
            "#!/usr/bin/env bash\necho 'violation: thing X broke'\nexit 1\n")
        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 1, msg=result.stdout + result.stderr)
        self.assertIn("FAIL", result.stdout)
        self.assertIn("check-broken.sh", result.stdout)
        self.assertIn("1/1 invariant(s) broken", result.stdout)
        self.assertIn("violation: thing X broke", result.stdout)


class NoShortCircuitTests(unittest.TestCase):
    def setUp(self) -> None:
        self.sandbox = make_sandbox()

    def tearDown(self) -> None:
        shutil.rmtree(self.sandbox, ignore_errors=True)

    def _write_check(self, name: str, body: str) -> None:
        p = self.sandbox / "scripts" / f"check-{name}.sh"
        p.write_text(body)
        p.chmod(0o755)

    def test_mixed_pass_fail_pass_all_run(self) -> None:
        self._write_check("a-pass", "#!/usr/bin/env bash\necho 'a ran'\nexit 0\n")
        self._write_check("b-fail", "#!/usr/bin/env bash\necho 'b ran'\nexit 1\n")
        self._write_check("c-pass", "#!/usr/bin/env bash\necho 'c ran'\nexit 0\n")

        result = run_aggregator(self.sandbox)
        self.assertEqual(result.returncode, 1)
        self.assertIn("a ran", result.stdout)
        self.assertIn("b ran", result.stdout)
        self.assertIn("c ran", result.stdout)
        self.assertIn("PASS", result.stdout)
        self.assertIn("FAIL", result.stdout)
        self.assertIn("1/3 invariant(s) broken", result.stdout)


if __name__ == "__main__":
    unittest.main()
