"""AC12 — swarm/bin/octi-worker exists as slice-4 placeholder."""

from __future__ import annotations

import os
import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
WORKER = REPO / "swarm" / "bin" / "octi-worker"


class TestOctiWorkerStub(unittest.TestCase):
    def test_file_exists(self):
        self.assertTrue(WORKER.is_file(), f"missing: {WORKER}")

    def test_is_executable(self):
        self.assertTrue(os.access(WORKER, os.X_OK), f"not executable: {WORKER}")

    def test_exits_nonzero_with_slice4_message(self):
        proc = subprocess.run(
            [sys.executable, str(WORKER)],
            capture_output=True, text=True, timeout=5,
        )
        self.assertEqual(proc.returncode, 1)
        self.assertIn("slice-4", proc.stderr.lower())
        self.assertIn("placeholder", proc.stderr.lower())


if __name__ == "__main__":
    unittest.main()
