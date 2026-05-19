"""AC10 boundary — Octi imports only MiniSession from swarm.mini.

Spec 038 line 254 names the regression grep:
    grep -r 'from.*mini.*import' swarm/octi/ | grep -v 'MiniSession'
    must return zero matches.

This file pins that contract on the octi side. `swarm/tests/test_mini_import_boundary.py`
pins it from the mini side; both are required so a removal of either
swarm/octi/ or swarm/mini/ doesn't lose the regression guard.
"""

from __future__ import annotations

import importlib
import subprocess
import sys
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
if str(REPO) not in sys.path:
    sys.path.insert(0, str(REPO))


class TestOctiImportBoundary(unittest.TestCase):
    """AC10 — Octi's only allowed import from mini is `MiniSession`."""

    def test_octi_only_imports_minisession_from_mini(self):
        octi_dir = REPO / "swarm" / "octi"
        self.assertTrue(octi_dir.is_dir(), f"missing: {octi_dir}")
        # Exact spec grep, slightly hardened for portability.
        result = subprocess.run(
            ["bash", "-c",
             f"grep -rE 'from[[:space:]]+.*[Mm]inni?e.*[[:space:]]+import' "
             f"{octi_dir} | grep -vE 'import[[:space:]]+MiniSession\\b' || true"],
            capture_output=True, text=True, check=False, timeout=10,
        )
        self.assertEqual(
            result.stdout.strip(), "",
            f"AC10 violation — octi imports non-MiniSession symbol(s) from mini:\n"
            f"{result.stdout}",
        )

    def test_octi_does_not_import_mini_internals(self):
        """`from swarm.mini._internal...` MUST NOT appear in swarm/octi/."""
        octi_dir = REPO / "swarm" / "octi"
        result = subprocess.run(
            ["bash", "-c",
             f"grep -rE 'from[[:space:]]+swarm\\.mini\\._internal' "
             f"{octi_dir} || true"],
            capture_output=True, text=True, check=False, timeout=10,
        )
        self.assertEqual(
            result.stdout.strip(), "",
            f"AC10 violation — octi imports mini internals:\n{result.stdout}",
        )

    def test_minisession_is_actually_used(self):
        """Sanity: at least one octi module imports MiniSession. If this
        test fails it means the boundary has stopped being load-bearing
        — either octi is no longer using mini at all (suspicious) or
        the import has been renamed.
        """
        octi_dir = REPO / "swarm" / "octi"
        result = subprocess.run(
            ["bash", "-c",
             f"grep -rEl 'from[[:space:]]+swarm\\.mini[[:space:]]+import[[:space:]]+MiniSession' "
             f"{octi_dir}"],
            capture_output=True, text=True, check=False, timeout=10,
        )
        self.assertNotEqual(
            result.stdout.strip(), "",
            "octi does not import MiniSession anywhere — boundary test is no longer load-bearing",
        )


class TestOctiPublicSurface(unittest.TestCase):
    """`swarm.octi` exports a documented public surface. Slice 2+
    consumers (and the future octi-worker in slice 4) should only
    reach for these names.
    """

    EXPECTED = {
        "Controller",
        "ControllerConfig",
        "TickOutcome",
        "VerifyResult",
        "VerifyVerdict",
        "run_verify",
    }

    def test_all_matches_expected(self):
        import swarm.octi as octi
        self.assertEqual(set(octi.__all__), self.EXPECTED)

    def test_each_name_is_importable_from_package(self):
        mod = importlib.import_module("swarm.octi")
        missing = [n for n in self.EXPECTED if not hasattr(mod, n)]
        self.assertEqual(missing, [], f"swarm.octi missing: {missing}")


if __name__ == "__main__":
    unittest.main()
