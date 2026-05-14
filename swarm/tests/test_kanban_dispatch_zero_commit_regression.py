from __future__ import annotations

import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CANONICAL = ROOT / "swarm" / "workflows" / "kanban-dispatch.lobster"
MIRROR = ROOT / "docs" / "governance-setup-extras" / "kanban-dispatch.lobster"


class KanbanDispatchZeroCommitRegressionTests(unittest.TestCase):
    def test_workflow_mirror_matches_canonical(self):
        self.assertEqual(CANONICAL.read_text(), MIRROR.read_text())

    def test_zero_commit_path_is_blocked_before_pr_open(self):
        workflow = CANONICAL.read_text()

        self.assertIn('&& "$WORKER_STATUS" != "completed_no_commit"', workflow)
        self.assertIn("completed_no_commit)", workflow)
        self.assertIn('worker_failure_report.py" --ticket-id ${ticket_id}', workflow)
        self.assertIn('kanban-flow block ${ticket_id} "$BLOCK_REASON"', workflow)

        zero_commit_branch = workflow.split("completed_no_commit)", 1)[1].split("failed)", 1)[0]
        self.assertNotIn("gh pr create failed", zero_commit_branch)
        self.assertNotIn("git push -u origin", zero_commit_branch)


if __name__ == "__main__":
    unittest.main()
