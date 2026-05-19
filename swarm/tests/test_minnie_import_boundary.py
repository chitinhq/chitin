"""AC10 — Octi (slice 2+) may only import MiniSession from minnie."""

from __future__ import annotations

import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]


class TestMinnieImportBoundary(unittest.TestCase):
    def test_octi_only_imports_minisession(self):
        octi_dir = REPO / "swarm" / "octi"
        if not octi_dir.is_dir():
            self.skipTest("swarm/octi/ does not exist yet (pre-slice-2)")
        result = subprocess.run(
            ["bash", "-c",
             f"grep -rE 'from[[:space:]]+.*[Mm]inni?e.*[[:space:]]+import' "
             f"{octi_dir} | grep -vE 'import[[:space:]]+MiniSession\\b' || true"],
            capture_output=True, text=True, check=False, timeout=10,
        )
        self.assertEqual(result.stdout.strip(), "",
                         f"octi imports minnie internals:\n{result.stdout}")

    def test_minnie_package_exports_only_minisession(self):
        from swarm.minnie import __all__ as exported
        self.assertEqual(set(exported), {"MiniSession"})

    def test_minisession_importable_from_top(self):
        import importlib
        mod = importlib.import_module("swarm.minnie")
        self.assertTrue(hasattr(mod, "MiniSession"))


if __name__ == "__main__":
    sys.path.insert(0, str(REPO))
    unittest.main()
