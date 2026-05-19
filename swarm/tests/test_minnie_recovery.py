from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))


class TestRecoveryUsageError(unittest.TestCase):
    """AC11 — minnie open --recovery <nonexistent> is a usage error (exit 2)."""

    def test_recovery_missing_state_dir_exits_2(self):
        with tempfile.TemporaryDirectory() as td:
            env = {
                **os.environ,
                "MINNIE_STATE_ROOT": td,
                "PYTHONPATH": str(REPO),
            }
            proc = subprocess.run(
                ["python3", str(REPO / "swarm" / "bin" / "minnie"),
                 "open", "--recovery", "does-not-exist-12345678"],
                env=env, capture_output=True, text=True, timeout=15,
            )
            self.assertEqual(proc.returncode, 2,
                             f"expected exit 2, got {proc.returncode}\nstderr={proc.stderr}")
            self.assertIn("does-not-exist-12345678", proc.stderr)


if __name__ == "__main__":
    unittest.main()
