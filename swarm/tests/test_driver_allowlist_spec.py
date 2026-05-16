from __future__ import annotations

import unittest
from pathlib import Path


SPEC = (
    Path(__file__).resolve().parents[2]
    / ".specify"
    / "specs"
    / "004-driver-allowlist"
    / "spec.md"
)


class DriverAllowlistSpecTests(unittest.TestCase):
    def test_ticket_t_7cb9cf49_references_spec_path(self) -> None:
        text = SPEC.read_text(encoding="utf-8")

        self.assertIn("t_7cb9cf49", text)
        self.assertTrue(SPEC.is_file())

    def test_empty_boundary_has_planned_test_case(self) -> None:
        text = SPEC.read_text(encoding="utf-8").lower()

        self.assertIn("empty boundary", text)
        self.assertIn("approval_source=fallback", text)

    def test_max_boundary_has_planned_test_case(self) -> None:
        text = SPEC.read_text(encoding="utf-8").lower()

        self.assertIn("max boundary", text)
        self.assertIn("large driver registry", text)

    def test_error_boundary_has_planned_test_case(self) -> None:
        text = SPEC.read_text(encoding="utf-8").lower()

        self.assertIn("error boundary", text)
        self.assertIn("malformed json", text)


if __name__ == "__main__":
    unittest.main()
