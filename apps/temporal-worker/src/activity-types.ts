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
   * Hook events extracted from the agent's stream-json output.
   *
   * Only present when --include-hook-events was passed AND the parser
   * found at least one well-formed event in the bounded stdout tail.
   * The set is a best-effort projection from the tail window — events
   * older than TAIL_BYTES of stdout will not appear.
   *
   * Each entry is a typed projection of the underlying agent hook
   * event (claude-code or openclaw), keeping only the fields downstream
   * consumers (apply-step, audit log) actually use.
   */
  hook_events?: HookEventSummary[];
}

/**
 * Stable, narrow projection of an agent hook event. Both claude-code
 * (PreToolUse / PostToolUse / Stop / etc.) and openclaw (before_tool_call,
 * subagent_spawning, etc.) emit events with these fields populated.
 *
 * Fields are optional because event types vary — a Stop event has no
 * tool_name, a Notification event has no decision, etc.
 */
export interface HookEventSummary {
  /** Event family — e.g. 'PreToolUse', 'before_tool_call', 'Stop'. */
  hook_name?: string;
  /** Tool the event references when applicable. */
  tool_name?: string;
  /** allow / deny / error when the event reports a gate decision. */
  decision?: 'allow' | 'deny' | 'error';
  /** Human-readable explanation when present. */
  reason?: string;
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
