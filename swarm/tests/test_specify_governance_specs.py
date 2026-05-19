from __future__ import annotations

import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def read_spec(relative_path: str) -> str:
    return (ROOT / relative_path).read_text(encoding="utf-8")


class SpecifyGovernanceSpecTests(unittest.TestCase):
    def test_scripts_manifest_spec_names_empty_max_error_test_obligations(self) -> None:
        spec = read_spec(".specify/specs/002-scripts-manifest/spec.md")

        self.assertIn("Boundary: empty", spec)
        self.assertIn("Boundary: max", spec)
        self.assertIn("Boundary: error", spec)


if __name__ == "__main__":
    unittest.main()
