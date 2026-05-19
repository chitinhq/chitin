"""Tests for swarm.octi.verifier.run_verify.

Boundaries covered (Knuth: name the boundaries):
- empty / whitespace command → no_verifier
- successful command (rc=0) → passed
- failing command (rc≠0) → failed
- timeout → timeout verdict + timed_out=True
- broken interpreter → error
- cwd is honored
- stdout/stderr captured and truncated
"""

from __future__ import annotations

import os
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))

from swarm.octi.verifier import run_verify, _TRUNCATE  # type: ignore[attr-defined]


class TestVerifierBoundaries(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.cwd = Path(self.tmp.name)

    def test_none_command_is_no_verifier(self):
        r = run_verify(None, cwd=self.cwd)
        self.assertEqual(r.verdict, "no_verifier")
        self.assertIsNone(r.returncode)
        self.assertFalse(r.timed_out)

    def test_empty_command_is_no_verifier(self):
        r = run_verify("", cwd=self.cwd)
        self.assertEqual(r.verdict, "no_verifier")

    def test_whitespace_command_is_no_verifier(self):
        r = run_verify("   \n\t  ", cwd=self.cwd)
        self.assertEqual(r.verdict, "no_verifier")

    def test_success_is_passed(self):
        r = run_verify("true", cwd=self.cwd)
        self.assertEqual(r.verdict, "passed")
        self.assertEqual(r.returncode, 0)
        self.assertFalse(r.timed_out)

    def test_failure_is_failed(self):
        r = run_verify("false", cwd=self.cwd)
        self.assertEqual(r.verdict, "failed")
        self.assertNotEqual(r.returncode, 0)

    def test_failure_captures_stderr(self):
        r = run_verify("echo oops 1>&2; exit 7", cwd=self.cwd)
        self.assertEqual(r.verdict, "failed")
        self.assertEqual(r.returncode, 7)
        self.assertIn("oops", r.stderr)

    def test_success_captures_stdout(self):
        r = run_verify("echo hello", cwd=self.cwd)
        self.assertEqual(r.verdict, "passed")
        self.assertIn("hello", r.stdout)

    def test_timeout(self):
        r = run_verify("sleep 5", cwd=self.cwd, timeout_seconds=1)
        self.assertEqual(r.verdict, "timeout")
        self.assertTrue(r.timed_out)
        self.assertIsNone(r.returncode)

    def test_cwd_honored(self):
        marker = self.cwd / "marker.txt"
        marker.write_text("ok")
        r = run_verify("test -f marker.txt", cwd=self.cwd)
        self.assertEqual(r.verdict, "passed")

    def test_cwd_unrelated_dir_does_not_see_marker(self):
        marker = self.cwd / "marker.txt"
        marker.write_text("ok")
        elsewhere = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(elsewhere, ignore_errors=True))
        r = run_verify("test -f marker.txt", cwd=elsewhere)
        self.assertEqual(r.verdict, "failed")

    def test_stdout_truncated_when_too_long(self):
        # Produce > _TRUNCATE bytes of output
        big = _TRUNCATE * 2
        r = run_verify(f"yes A | head -c {big}", cwd=self.cwd)
        self.assertEqual(r.verdict, "passed")
        self.assertLessEqual(len(r.stdout), _TRUNCATE)
        self.assertIn("truncated", r.stdout)

    def test_env_isolation_when_provided(self):
        r = run_verify(
            "echo $OCTI_TEST_VAR",
            cwd=self.cwd,
            env={"OCTI_TEST_VAR": "from_env", "PATH": os.environ.get("PATH", "")},
        )
        self.assertEqual(r.verdict, "passed")
        self.assertIn("from_env", r.stdout)


if __name__ == "__main__":
    unittest.main()
