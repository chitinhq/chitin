#!/usr/bin/env python3
"""Tests for the regression-gate integration in clawta-pr-lifecycle."""

from __future__ import annotations

import importlib.machinery
import importlib.util
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "bin" / "clawta-pr-lifecycle"


def load_module():
    loader = importlib.machinery.SourceFileLoader("clawta_pr_lifecycle", str(SCRIPT))
    spec = importlib.util.spec_from_loader("clawta_pr_lifecycle", loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


def base_pr(**overrides) -> dict:
    """A canonical 'ready-to-merge' PR dict from gh pr list JSON output.

    headRefName uses a swarm branch pattern so infer_ticket() resolves to
    t_deadbeef (8-hex suffix after the last '-').

    headRefOid is a full 40-char SHA that starts with the 7-char abbreviated
    head used in approve_comment() so that review_matches_head() returns True
    (it checks head.startswith(reviewed_head)).

    REVIEW_MARKER_RE requires [0-9a-f]{7,40} so the head abbreviation in the
    review comment must be at least 7 hex chars; "abc123" (6 chars) does not
    match, hence the 7-char "abc1234" prefix used here.
    """
    pr = {
        "number": 999,
        "url": "https://github.com/chitinhq/chitin/pull/999",
        "title": "test pr",
        "headRefName": "swarm/agent-deadbeef",
        "headRefOid": "abc1234deadbeefdeadbeefdeadbeef12345678",
        "baseRefName": "main",
        "state": "OPEN",
        "mergeable": "MERGEABLE",
        "mergedAt": None,
        "body": "",
        "isDraft": False,
        "updatedAt": "2026-05-13T20:00:00Z",
    }
    pr.update(overrides)
    return pr


def approve_comment(head: str = "abc1234") -> dict:
    """A canonical APPROVE review comment from gh pr comments.

    The head abbreviation must be at least 7 hex chars to satisfy
    REVIEW_MARKER_RE (which requires [0-9a-f]{7,40} in the head= field).
    """
    return {
        "user": {"login": "jpleva91"},
        "body": f"<!-- clawta-reviewer:v1 head={head} -->\n**Verdict:** APPROVE",
    }


# ticket_info returns {"status": ..., "assignee": ...} — the "id" key in the
# plan spec was a typo; the real return shape has "status" + "assignee".
_TICKET_INFO_PASS = {"status": "in_progress", "assignee": None}


class GateShortCircuitTests(unittest.TestCase):
    def test_gate_skipped_when_base_gates_fail(self) -> None:
        """If (a-e) fail, gate is NOT invoked (expensive subprocess avoided)."""
        m = load_module()
        # PR is draft → base_ready false → gate skipped.
        pr = base_pr(isDraft=True)
        with mock.patch.object(m, "run_regression_gate") as gate, \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value=_TICKET_INFO_PASS):
            result = m.classify(pr, [approve_comment()])
        self.assertEqual(result["gate_status"], "skipped")
        self.assertFalse(result["auto_merge_ready"])
        gate.assert_not_called()


class GatePassTests(unittest.TestCase):
    def test_gate_pass_sets_auto_merge_ready(self) -> None:
        m = load_module()
        pr = base_pr()
        with mock.patch.object(m, "run_regression_gate",
                               return_value=(0, "All 2 invariants preserved.")) as gate, \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value=_TICKET_INFO_PASS):
            result = m.classify(pr, [approve_comment()])
        self.assertEqual(result["gate_status"], "pass")
        self.assertTrue(result["auto_merge_ready"])
        self.assertEqual(result["action"], "ready-to-merge")
        gate.assert_called_once()


class GateFailInvariantTests(unittest.TestCase):
    def test_gate_fail_invariant_sets_action_and_diagnostic(self) -> None:
        m = load_module()
        pr = base_pr()
        diagnostic = "1/2 invariant(s) broken.\nFAIL  scripts/check-foo.sh"
        with mock.patch.object(m, "run_regression_gate",
                               return_value=(1, diagnostic)), \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value=_TICKET_INFO_PASS):
            result = m.classify(pr, [approve_comment()])
        self.assertEqual(result["gate_status"], "fail-invariant")
        self.assertEqual(result["gate_diagnostic"], diagnostic)
        self.assertFalse(result["auto_merge_ready"])
        self.assertEqual(result["action"], "regression-gate-fail")


class GateFailToolTests(unittest.TestCase):
    def test_gate_fail_tool_sets_separate_action(self) -> None:
        m = load_module()
        pr = base_pr()
        with mock.patch.object(m, "run_regression_gate",
                               return_value=(2, "git worktree add failed: ...")), \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value=_TICKET_INFO_PASS):
            result = m.classify(pr, [approve_comment()])
        self.assertEqual(result["gate_status"], "fail-tool")
        self.assertFalse(result["auto_merge_ready"])
        self.assertEqual(result["action"], "regression-gate-error")


class EscalationExclusionTests(unittest.TestCase):
    def test_regression_gate_fail_not_escalated_generically(self) -> None:
        """needs_operator_escalation must skip regression-gate-fail items
        because post_regression_gate_comment already handles them."""
        m = load_module()
        item = {
            "ticket": "t_deadbeef",
            "state": "OPEN",
            "ticket_status": "in_progress",
            "auto_merge_ready": False,
            "action": "regression-gate-fail",
        }
        self.assertFalse(m.needs_operator_escalation(item))

    def test_regression_gate_error_not_escalated_generically(self) -> None:
        m = load_module()
        item = {
            "ticket": "t_deadbeef",
            "state": "OPEN",
            "ticket_status": "in_progress",
            "auto_merge_ready": False,
            "action": "regression-gate-error",
        }
        self.assertFalse(m.needs_operator_escalation(item))


if __name__ == "__main__":
    unittest.main()
