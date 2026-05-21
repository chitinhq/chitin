"""Spec 018 — dispatch base-freshness invariant regression tests.

These are static-analysis style tests against kanban-dispatch.lobster
(matching the pattern in test_kanban_dispatch_zero_commit_regression.py).
They assert the lobster contains the four required pieces of the
base-freshness contract — if a future edit removes any of them, the
regression bites here, not in production.

Reference: .specify/specs/018-dispatch-base-freshness/spec.md
"""
from __future__ import annotations

import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
CANONICAL = ROOT / "swarm" / "workflows" / "kanban-dispatch.lobster"
MIRROR = ROOT / "docs" / "governance-setup-extras" / "kanban-dispatch.lobster"
POLLER = ROOT / "swarm" / "bin" / "clawta-poller"
CONTROLLER = ROOT / "swarm" / "bin" / "swarm-controller"


class DispatchBaseFreshnessRegressionTests(unittest.TestCase):
    def test_workflow_mirror_matches_canonical(self):
        self.assertEqual(CANONICAL.read_text(), MIRROR.read_text())

    def test_fetch_runs_before_worktree_setup(self):
        """R1: every spawn fetches origin/$DEFAULT_BRANCH before worktree work."""
        workflow = CANONICAL.read_text()
        self.assertIn(
            'git -C "$CHITIN_REPO" fetch --quiet origin "$DEFAULT_BRANCH"',
            workflow,
        )
        fetch_idx = workflow.find(
            'git -C "$CHITIN_REPO" fetch --quiet origin "$DEFAULT_BRANCH"'
        )
        worktree_add_idx = workflow.find(
            'git -C "$CHITIN_REPO" worktree add -b "$BRANCH" "$WORKTREE_DIR"'
        )
        self.assertGreater(fetch_idx, 0, "fetch must exist")
        self.assertGreater(worktree_add_idx, fetch_idx, "fetch must precede worktree add")

    def test_start_records_dedicated_dispatch_worktree(self):
        """Spec 070 FR-013/SC-007: direct workflow dispatch must not let
        kanban-flow infer the primary checkout as the task run worktree."""
        workflow = CANONICAL.read_text()
        self.assertIn(
            'DISPATCH_WORKTREE="$HOME/.cache/chitin/swarm-worktrees/$pick_driver.json.driver-${ticket_id}"',
            workflow,
        )
        self.assertIn(
            'kanban-flow start ${ticket_id} --author clawta --worktree "$DISPATCH_WORKTREE"',
            workflow,
        )
        start_idx = workflow.find(
            'kanban-flow start ${ticket_id} --author clawta --worktree "$DISPATCH_WORKTREE"'
        )
        worktree_add_idx = workflow.find(
            'git -C "$CHITIN_REPO" worktree add -b "$BRANCH" "$WORKTREE_DIR"'
        )
        self.assertGreater(start_idx, 0)
        self.assertGreater(worktree_add_idx, start_idx)

    def test_python_dispatchers_start_with_dedicated_worktree(self):
        """Poller/controller must record the same worktree path the Lobster
        worker spawn will use, instead of letting kanban-flow fall back to a
        task workspace or operator checkout."""
        for path in (POLLER, CONTROLLER):
            text = path.read_text()
            with self.subTest(path=path):
                self.assertIn('".cache" / "chitin" / "swarm-worktrees"', text)
                self.assertIn('"--worktree", str(worktree_path)', text)

    def test_fetch_failure_aborts_spawn(self):
        """R1 fail-loud: if fetch fails, spawn aborts (does not fall through
        to a stale worktree). The error string must be the spec'd one."""
        workflow = CANONICAL.read_text()
        self.assertIn(
            "spawn_worker: base-freshness fetch failed for origin/$DEFAULT_BRANCH",
            workflow,
        )
        fetch_idx = workflow.find(
            'git -C "$CHITIN_REPO" fetch --quiet origin "$DEFAULT_BRANCH"'
        )
        worktree_check_idx = workflow.find('if [[ ! -d "$WORKTREE_DIR" ]]')
        between = workflow[fetch_idx:worktree_check_idx]
        self.assertIn("exit 1", between, "fetch failure must exit 1 before worktree work")

    def test_redispatch_resets_worktree_to_fresh_base(self):
        """R2: when worktree exists, the pipeline resets it to origin/$DEFAULT_BRANCH
        and cleans untracked files. Without this, re-dispatch keeps stale base."""
        workflow = CANONICAL.read_text()
        self.assertIn(
            'git -C "$WORKTREE_DIR" reset --hard "origin/$DEFAULT_BRANCH"',
            workflow,
        )
        self.assertIn('git -C "$WORKTREE_DIR" clean -fd', workflow)
        # Must be in the else branch of `if [[ ! -d "$WORKTREE_DIR" ]]`.
        worktree_check_idx = workflow.find('if [[ ! -d "$WORKTREE_DIR" ]]')
        cd_idx = workflow.find('if ! cd "$WORKTREE_DIR"')
        reset_idx = workflow.find('git -C "$WORKTREE_DIR" reset --hard "origin/$DEFAULT_BRANCH"')
        self.assertGreater(reset_idx, worktree_check_idx)
        self.assertLess(reset_idx, cd_idx)

    def test_post_setup_invariant_assert(self):
        """R3: after worktree is ready, HEAD must equal origin/$DEFAULT_BRANCH,
        otherwise spawn aborts. This is the belt to the suspenders of R1+R2."""
        workflow = CANONICAL.read_text()
        self.assertIn('BASE_SHA=$(git -C "$CHITIN_REPO" rev-parse "origin/$DEFAULT_BRANCH")', workflow)
        self.assertIn('HEAD_SHA=$(git -C "$WORKTREE_DIR" rev-parse HEAD)', workflow)
        self.assertIn(
            'spawn_worker: base-freshness invariant violated',
            workflow,
        )
        # Invariant check must come before the model spawn (PROMPT build).
        invariant_idx = workflow.find('base-freshness invariant violated')
        prompt_idx = workflow.find('PROMPT="${ROLE_HEADER}${ENV_HEADER}${TICKET_BODY}"')
        self.assertGreater(invariant_idx, 0)
        self.assertGreater(prompt_idx, invariant_idx)

    def test_base_state_logged_to_dispatch_log(self):
        """R4: a one-line `[base-freshness]` summary is logged before the
        model starts, so retros can pinpoint what base each worker ran on."""
        workflow = CANONICAL.read_text()
        self.assertIn('spawn_worker: [base-freshness]', workflow)
        # Must include the sha (we use ${BASE_SHA:0:7}).
        log_idx = workflow.find('spawn_worker: [base-freshness]')
        log_line_end = workflow.find('\n', log_idx)
        log_line = workflow[log_idx:log_line_end]
        self.assertIn('${BASE_SHA:0:7}', log_line)


if __name__ == "__main__":
    unittest.main()
