export interface ActivityResult {
  exit_code: number;
  stdout_tail: string;
  stderr_tail: string;
  duration_ms: number;
  // Slice 5: present only when base_ref was set on the request and the
  // activity created a worktree. Apply-step uses worktree_path to push
  // the branch and open a PR.
  worktree?: WorktreeResult;
  /**
   * Parsed summary of hook events emitted by the agent (if available).
   * Only present when --include-hook-events was passed and the agent supports it.
   */
  hookEvents?: any[];
}

export interface WorktreeResult {
  // Absolute path of the worktree on disk. May still exist after the
  // activity returns — the apply step is responsible for cleanup so it
  // can read the worktree's git state. Activity does NOT auto-rmrf.
  path: string;
  // Branch the worktree was checked out on (created by the activity).
  branch: string;
  // Resolved sha of the worktree HEAD when the activity exited. May
  // differ from the base_ref's sha if the agent committed.
  head_sha: string;
  // Number of commits the agent added on top of base_ref.
  commits_added: number;
  // Whether there are uncommitted changes in the worktree (working tree
  // or index dirty). Apply step decides whether to commit or discard.
  has_uncommitted_changes: boolean;
  // Output of `git diff --shortstat base_ref...HEAD` — total file/line
  // counts. Empty string if no diff. Used for apply-step gating
  // (e.g., refuse to push a PR with > N changed files).
  diff_shortstat: string;
}
