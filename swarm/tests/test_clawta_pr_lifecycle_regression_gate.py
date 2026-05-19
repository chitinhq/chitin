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


class RegressionGateFetchTests(unittest.TestCase):
    def test_gate_fetches_github_merge_ref_before_worktree(self) -> None:
        m = load_module()
        calls = []

        def fake_run(cmd, **kwargs):
            calls.append(cmd)
            return mock.Mock(returncode=0, stdout="", stderr="")

        with mock.patch.object(m.tempfile, "mkdtemp", return_value="/tmp/regression-gate-pr-42"), \
             mock.patch.object(m, "run", side_effect=fake_run), \
             mock.patch.object(m.subprocess, "run", return_value=mock.Mock(returncode=0, stdout="ok\n", stderr="")), \
             mock.patch.object(m.shutil, "rmtree"):
            rc, diagnostic = m.run_regression_gate(42, "abc1234")

        self.assertEqual(rc, 0)
        self.assertEqual(diagnostic, "ok")
        self.assertEqual(
            calls[0],
            ["git", "fetch", "origin", "+refs/pull/42/merge:refs/remotes/origin/pr/42/merge"],
        )
        self.assertEqual(
            calls[1],
            ["git", "worktree", "add", "--detach", "/tmp/regression-gate-pr-42", "refs/remotes/origin/pr/42/merge"],
        )


class TicketInferenceTests(unittest.TestCase):
    def test_infer_ticket_branch_implies_close_intent(self) -> None:
        m = load_module()
        pr = base_pr(headRefName="swarm/hermes-f2ede4a8", body="")
        ref = m.infer_ticket(pr, [])
        self.assertIsNotNone(ref)
        self.assertEqual(ref.ticket_id, "t_f2ede4a8")
        self.assertTrue(ref.close_intent)

    def test_infer_ticket_close_keywords_imply_close_intent(self) -> None:
        m = load_module()
        cases = [
            ("Closes t_f2ede4a8", True),
            ("Fixes t_f2ede4a8", True),
            ("Resolves t_f2ede4a8", True),
            ("closes: t_f2ede4a8", True),
            ("Fixes #123, refs t_f2ede4a8", False),  # "refs" keyword = reference, not close
        ]
        for body, expected_close in cases:
            with self.subTest(body=body):
                pr = base_pr(headRefName="clawta/lifecycle-fix", body=body)
                ref = m.infer_ticket(pr, [])
                self.assertIsNotNone(ref)
                self.assertEqual(ref.ticket_id, "t_f2ede4a8")
                self.assertEqual(ref.close_intent, expected_close)

    def test_infer_ticket_ref_keywords_imply_no_close_intent(self) -> None:
        m = load_module()
        cases = [
            "Refs t_f2ede4a8",
            "Ref t_f2ede4a8",
            "References t_f2ede4a8",
            "Reference t_f2ede4a8",
            "ticket t_f2ede4a8",
            "task t_f2ede4a8",
            "kanban t_f2ede4a8",
        ]
        for body in cases:
            with self.subTest(body=body):
                pr = base_pr(
                    headRefName="clawta/lifecycle-pr-ref-mapping",
                    body=f"## Summary\n- lifecycle mapping fix\n\n{body}",
                )
                ref = m.infer_ticket(pr, [])
                self.assertIsNotNone(ref)
                self.assertEqual(ref.ticket_id, "t_f2ede4a8")
                self.assertFalse(ref.close_intent)

    def test_infer_ticket_ignores_arbitrary_comment_refs(self) -> None:
        m = load_module()
        pr = base_pr(headRefName="clawta/human-readable", body="")
        comments = [{"body": "Refs t_badbeef1"}]

        self.assertIsNone(m.infer_ticket(pr, comments))

    def test_infer_ticket_no_match_returns_none(self) -> None:
        m = load_module()
        pr = base_pr(headRefName="clawta/no-ticket-id", body="Unrelated changes")
        self.assertIsNone(m.infer_ticket(pr, []))


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


class DeployDriftTests(unittest.TestCase):
    DRIFT_DIAGNOSTIC = """── scripts/check-swarm-deployed-sync.sh ──
  DIFFERS  /home/red/.openclaw/bin/clawta-pr-reviewer

═══ regression-gate summary ═══
  PASS   scripts/check-governance-boundary.sh
  FAIL   scripts/check-swarm-deployed-sync.sh
  PASS   scripts/check-worktree-naming.sh

1/5 invariant(s) broken.
"""

    def test_deploy_drift_diagnostic_matches_only_sync_failure(self) -> None:
        m = load_module()
        self.assertTrue(m.is_deploy_drift_diagnostic(self.DRIFT_DIAGNOSTIC))
        self.assertFalse(m.is_deploy_drift_diagnostic(
            self.DRIFT_DIAGNOSTIC + "  FAIL   scripts/check-governance-boundary.sh\n"
        ))

    def test_classify_deploy_drift_as_first_class_action(self) -> None:
        m = load_module()
        pr = base_pr()
        with mock.patch.object(m, "run_regression_gate",
                               return_value=(1, self.DRIFT_DIAGNOSTIC)), \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info",
                               return_value=_TICKET_INFO_PASS):
            result = m.classify(pr, [approve_comment()])

        self.assertEqual(result["gate_status"], "deploy-drift")
        self.assertEqual(result["action"], "deploy-drift")
        self.assertFalse(result["auto_merge_ready"])


    def test_deploy_drift_existing_marker_suppresses_duplicate_comment(self) -> None:
        m = load_module()
        pr = base_pr()
        comments = [
            approve_comment(),
            {"body": "<!-- clawta-lifecycle:v1 kind=deploy-drift head=abc1234deadbeefdeadbeefdeadbeef12345678 -->"},
        ]

        with mock.patch.object(m, "list_prs", return_value=[pr]), \
             mock.patch.object(m, "comments", return_value=comments), \
             mock.patch.object(m, "run_regression_gate", return_value=(1, self.DRIFT_DIAGNOSTIC)), \
             mock.patch.object(m, "checks_state", return_value="pass"), \
             mock.patch.object(m, "ticket_info", return_value=_TICKET_INFO_PASS), \
             mock.patch.object(m, "post_deploy_drift_comment") as post_comment:
            result = m.run_once(("swarm/",), apply=True, auto_merge=True, escalate_to="red")

        post_comment.assert_not_called()
        item = result["items"][0]
        self.assertEqual(item["action"], "deploy-drift")
        self.assertEqual(item["applied"], "deploy-drift")

    def test_repair_deploy_drift_runs_install_then_verify(self) -> None:
        m = load_module()
        calls = []

        def fake_run(cmd, **kwargs):
            calls.append(cmd)
            return mock.Mock(returncode=0, stdout="", stderr="")

        with mock.patch.object(m, "run", side_effect=fake_run):
            self.assertTrue(m.repair_deploy_drift(apply=True))

        self.assertEqual(calls, [
            ["bash", "scripts/install-swarm.sh"],
            ["bash", "scripts/check-swarm-deployed-sync.sh"],
        ])


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
