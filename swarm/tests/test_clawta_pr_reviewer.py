from __future__ import annotations

import importlib.util
import sys
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path


SCRIPT = Path(__file__).resolve().parents[1] / "bin" / "clawta-pr-reviewer"


def load_module():
    spec = importlib.util.spec_from_loader(
        "clawta_pr_reviewer_test",
        SourceFileLoader("clawta_pr_reviewer_test", str(SCRIPT)),
    )
    assert spec is not None
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    sys.modules["clawta_pr_reviewer_test"] = module
    spec.loader.exec_module(module)
    return module


class ClawtaPrReviewerTests(unittest.TestCase):
    def test_uncovered_boundaries_detects_missing_named_cases_in_test_diff(self) -> None:
        module = load_module()
        ticket_body = """
invariants_and_boundaries:
  Invariant: Normalize never returns a non-error action with an empty target.
  Boundaries: empty string, whitespace-only, duplicate
"""
        diff = """diff --git a/swarm/tests/test_normalize.py b/swarm/tests/test_normalize.py
+++ b/swarm/tests/test_normalize.py
@@ -1,2 +1,4 @@
+def test_empty_string_is_rejected():
+    assert normalize(\"\") == \"error\"
"""

        missing = module.uncovered_boundaries(ticket_body, diff)

        self.assertEqual(missing, ["whitespace-only", "duplicate"])

    def test_enforce_boundary_coverage_review_forces_request_changes(self) -> None:
        module = load_module()
        review = """**Clawta automated review**

**Verdict:** APPROVE

**Summary**
- Looks good.

**Findings**
- No blocking issues found.

**Validation notes**
- Tests look reasonable.
"""

        updated = module.enforce_boundary_coverage_review(
            review,
            ["whitespace-only", "duplicate"],
            "t_deadbeef",
        )

        self.assertIn("**Verdict:** REQUEST_CHANGES", updated)
        self.assertIn("whitespace-only, duplicate", updated)
        self.assertIn("ticket t_deadbeef boundary coverage", updated)

    def test_linked_ticket_id_prefers_ticket_reference_from_pr_body(self) -> None:
        module = load_module()
        meta = {
            "title": "fix: cover parser boundaries",
            "body": "Closes ticket t_6dbe137e (dispatched via clawta).",
        }

        self.assertEqual(module.linked_ticket_id(meta), "t_6dbe137e")


if __name__ == "__main__":
    unittest.main()
