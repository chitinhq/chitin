from __future__ import annotations

import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
CANONICAL_WORKFLOW = REPO_ROOT / "swarm" / "workflows" / "kanban-dispatch.lobster"
MIRROR_WORKFLOW = REPO_ROOT / "docs" / "governance-setup-extras" / "kanban-dispatch.lobster"


class KanbanDispatchWorkflowTests(unittest.TestCase):
    def test_missing_worktree_uses_retryable_dispatch_failure_path(self) -> None:
        workflow = CANONICAL_WORKFLOW.read_text()

        self.assertIn(
            'DETAILS=$(dispatch_details "$DRIVER")',
            workflow,
        )
        self.assertIn('apply_retryable_failure "missing_worktree" "$MSG" "$DETAILS"', workflow)
        self.assertNotIn(
            'kanban-flow block ${ticket_id} "worker completed but produced no worktree"',
            workflow,
        )

    def test_docs_mirror_stays_in_sync_for_missing_worktree_handling(self) -> None:
        canonical = CANONICAL_WORKFLOW.read_text()
        mirror = MIRROR_WORKFLOW.read_text()

        needle = 'apply_retryable_failure "missing_worktree" "$MSG" "$DETAILS"'
        self.assertIn(needle, canonical)
        self.assertIn(needle, mirror)

    def test_boundary_empty_branch_details_are_built_with_jq(self) -> None:
        workflow = CANONICAL_WORKFLOW.read_text()

        self.assertIn('dispatch_details() {', workflow)
        self.assertIn('DETAILS=$(dispatch_details "$DRIVER" "$BRANCH")', workflow)
        self.assertIn('apply_retryable_failure "empty_branch" "$MSG" "$DETAILS"', workflow)
        self.assertNotIn(
            'apply_retryable_failure "empty_branch" "$MSG" "{\\"driver\\":\\"$DRIVER\\",\\"branch\\":\\"$BRANCH\\"}"',
            workflow,
        )

    def test_boundary_error_pr_create_details_are_built_with_jq(self) -> None:
        workflow = CANONICAL_WORKFLOW.read_text()

        self.assertIn('apply_retryable_failure "gh_pr_create_failed" "$MSG" "$DETAILS"', workflow)
        self.assertNotIn(
            'apply_retryable_failure "gh_pr_create_failed" "$MSG" "{\\"driver\\":\\"$DRIVER\\",\\"branch\\":\\"$BRANCH\\"}"',
            workflow,
        )


if __name__ == "__main__":
    unittest.main()
