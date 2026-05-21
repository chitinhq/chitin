from __future__ import annotations

import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CANONICAL = ROOT / "swarm" / "workflows" / "kanban-dispatch.lobster"
MIRROR = ROOT / "docs" / "governance-setup-extras" / "kanban-dispatch.lobster"
INSTALLER = ROOT / "swarm" / "bin" / "install-swarm-workflow.sh"


class KanbanDispatchZeroCommitRegressionTests(unittest.TestCase):
    def test_workflow_mirror_matches_canonical(self):
        self.assertEqual(CANONICAL.read_text(), MIRROR.read_text())

    def test_zero_commit_path_is_blocked_before_pr_open(self):
        workflow = CANONICAL.read_text()

        self.assertIn('&& "$WORKER_STATUS" != "completed_no_commit"', workflow)
        self.assertIn("completed_no_commit)", workflow)
        self.assertIn('worker_failure_report.py" --ticket-id ${ticket_id}', workflow)
        self.assertIn('kanban-flow crash ${ticket_id} "$BLOCK_REASON"', workflow)

        zero_commit_branch = workflow.split("completed_no_commit)", 1)[1].split("failed)", 1)[0]
        self.assertNotIn("gh pr create failed", zero_commit_branch)
        self.assertNotIn("git push -u origin", zero_commit_branch)

    def test_legacy_workflow_installer_links_failure_report_helper(self):
        installer = INSTALLER.read_text()

        self.assertIn("worker_failure_report.py", installer)
        self.assertIn("pr_failure_report.py", installer)
        self.assertIn("spawn_worker_subprocess.py", installer)

    def test_independent_git_commit_count_gate_runs_before_push(self):
        """The finalize step must verify the branch has commits ahead of
        origin/$DEFAULT_BRANCH before pushing, independent of the worker's
        self-reported WORKER_STATUS. Without this, a worker that lies about
        completing (or a buggy worker that exits clean without committing)
        leaves a zombie branch pointing at origin/main, then `gh pr create`
        fails with "no commits between…" and the ticket is silently lost.
        """
        workflow = CANONICAL.read_text()

        # The gate must use git rev-list (independent of WORKER_STATUS).
        self.assertIn('git rev-list --count "origin/$DEFAULT_BRANCH..HEAD"', workflow)
        self.assertIn('COMMITS_AHEAD', workflow)
        # The gate must precede the push (extract slice between gate and push).
        gate_idx = workflow.find('COMMITS_AHEAD=$(git rev-list')
        push_idx = workflow.find('git push -u origin "$BRANCH"')
        self.assertGreater(gate_idx, 0, 'gate must exist')
        self.assertGreater(push_idx, gate_idx, 'gate must come before push')
        # On gate failure, must crash via kanban-flow and exit before push.
        between = workflow[gate_idx:push_idx]
        self.assertIn('kanban-flow crash', between)
        self.assertIn('exit 0', between)
        self.assertIn('empty_branch', between)

    def test_pr_create_failure_path_surfaces_gh_output(self):
        workflow = CANONICAL.read_text()

        self.assertIn('GH_PR_STDOUT_FILE=$(mktemp)', workflow)
        self.assertIn('GH_PR_STDERR_FILE=$(mktemp)', workflow)
        self.assertIn('--arg stdout "${GH_PR_STDOUT:-}"', workflow)
        self.assertIn('--arg stderr "${GH_PR_STDERR:-}"', workflow)
        self.assertIn('python3 "$HOME/.openclaw/workflows/pr_failure_report.py"', workflow)
        self.assertIn("printf '%s\\n' \"$BLOCK_REASON\"", workflow)
        self.assertIn('kanban-flow crash ${ticket_id} "$BLOCK_REASON"', workflow)

    def test_structured_worker_failure_reaches_finalize_dispatch(self):
        workflow = CANONICAL.read_text()

        self.assertIn('deferring to finalize_dispatch', workflow)
        spawn_tail = workflow.split('WORKER_STATUS=$(echo "$WORKER_RESULT"', 1)[1].split('BASH_SCRIPT', 1)[0]
        self.assertIn('exit 0', spawn_tail)
        self.assertNotIn('worker failed with status=$WORKER_STATUS" >&2\n        exit 1', spawn_tail)

    def test_blocked_ticket_aborts_before_spawn(self):
        workflow = CANONICAL.read_text()

        self.assertIn('dispatch aborted: ${ticket_id} status is $STATUS before spawn', workflow)
        start_idx = workflow.find('kanban-flow start ${ticket_id}')
        spawn_idx = workflow.find('SPAWN_CONFIG=$(printf')
        guard_idx = workflow.find('status is $STATUS before spawn')
        self.assertGreater(start_idx, 0)
        self.assertGreater(guard_idx, start_idx)
        self.assertLess(guard_idx, spawn_idx)

    def test_finalize_does_not_push_when_ticket_was_blocked_mid_run(self):
        workflow = CANONICAL.read_text()

        guard_idx = workflow.find('CURRENT_STATUS=$(hermes kanban')
        push_idx = workflow.find('git push -u origin "$BRANCH"')
        self.assertGreater(guard_idx, 0)
        self.assertGreater(push_idx, guard_idx)
        between = workflow[guard_idx:push_idx]
        self.assertIn('not pushing/opening PR', between)
        self.assertIn('exit 0', between)


if __name__ == "__main__":
    unittest.main()
