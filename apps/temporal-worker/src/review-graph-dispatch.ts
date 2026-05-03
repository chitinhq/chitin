// Phase 2 step 3b: dispatcher → review-graph integration. After the
// programmer dispatcher's apply step opens a PR, this enqueues the
// reviewGraphWorkflow so the §5 escalation chain runs in parallel
// with whatever the dispatcher does next (the next tick can pick a
// new entry while the review proceeds).
//
// The review-graph runs as a SEPARATE top-level workflow — it doesn't
// block the dispatcher's tick. That preserves the "one swarm
// workflow in flight at a time" invariant on the implementor side
// while letting reviewers proceed independently. The next tick still
// won't dispatch a new programmer until the review-graph (and any
// other reviewer-side work) completes, because the running-workflow
// scan in dispatcher.ts matches `swarm-*` prefix — which the
// reviewer-graph + reviewer dispatches inherit via parent prefix.
//
// Failure mode: if review-graph submit fails, log and move on. The
// PR still exists; operator can manually start the review or merge
// based on Copilot's R0 review. We never want a review-graph failure
// to leave the dispatcher stuck (the implementor's work has shipped).

import type { Client } from '@temporalio/client';
import type { reviewGraphWorkflow } from './review-graph-workflow.ts';
import type { BacklogEntry } from './grooming/parse-backlog.ts';
import type { WorktreeResult } from './activity-types.ts';
import type { PrMeta } from './review-graph.ts';

export const REVIEW_GRAPH_WORKFLOW_NAME = 'reviewGraphWorkflow';

export interface EnqueueReviewGraphInput {
  client: Client;
  /** Task queue the worker polls for review-graph dispatches. */
  taskQueue: string;
  /** The programmer workflow_id that just finished. Becomes the
   *  parent_workflow_id on every reviewer dispatch the graph fires. */
  parent_workflow_id: string;
  /** PR URL the apply step opened. Required — without it the graph
   *  has no PR to review and we silently no-op. */
  pr_url: string | undefined;
  /** Worktree result from the programmer's activity — source of
   *  truth for diff stats. May be undefined if the activity didn't
   *  produce a worktree (rare; review-graph still runs but with
   *  diff_loc=0, files_changed=0). */
  worktree: WorktreeResult | undefined;
  /** Backlog entry the programmer worked off. Drives tier rules
   *  (e.g., T3 implementor → start review at R2). */
  entry: BacklogEntry;
  /** Repo slug, e.g. 'chitinhq/chitin'. Used by reviewer agents to
   *  call gh CLI. */
  repo: string;
  /** Optional pre-built PrMeta. When set, the dispatcher skips its
   *  worktree-derived rebuild and threads this object through to
   *  reviewGraphWorkflow as-is. Callers like `pr-event-ingester`
   *  that don't have a worktree but DO have authoritative diff
   *  stats from `gh pr view` should pass this so the §5 trigger
   *  matrix evaluates against real metadata, not zeros. */
  pr_meta?: PrMeta;
  /** Optional log sink (defaults to console.log). Tests inject. */
  log?: (line: string) => void;
}

export interface EnqueueReviewGraphResult {
  /** Whether the review-graph workflow was actually submitted. False
   *  when pr_url was absent (PR didn't open) or submit failed. */
  enqueued: boolean;
  /** The review-graph workflow_id, when enqueued. */
  workflow_id?: string;
  /** Failure message when enqueued=false because of a submit error
   *  (vs. enqueued=false because pr_url was absent). */
  error?: string;
}

/**
 * Build the review-graph input from the dispatcher's post-apply state.
 * Pure — exported so tests can pin the field-mapping invariant
 * without standing up a Temporal client.
 */
export function buildReviewGraphInput(
  parent_workflow_id: string,
  pr_url: string,
  worktree: WorktreeResult | undefined,
  entry: BacklogEntry,
  repo: string,
): { parent_workflow_id: string; pr_meta: PrMeta; entry: BacklogEntry; repo: string } {
  const pr_number = extractPrNumber(pr_url);
  const { diff_loc, files_changed } = parseDiffShortstat(worktree?.diff_shortstat ?? '');
  return {
    parent_workflow_id,
    pr_meta: {
      diff_loc,
      files_changed,
      // We don't enumerate the file list here — git would have to be
      // re-invoked from a worktree that may be cleaned up. Reviewers
      // can `gh pr diff` if they need it. Empty list = "unknown,
      // skip path-scope-based bumps" per PrMeta docstring.
      files: [],
      pr_url,
      pr_number,
      // copilot_comment_count is undefined on first dispatch — Copilot
      // is racing us. Reviewer prompt handles that branch.
    },
    entry,
    repo,
  };
}

/**
 * Parse a `git diff --shortstat`-style line into (LOC, files_changed).
 * Defensive for the empty-diff case (worktree had no change to commit
 * but a PR opened on whatever was already pushed — rare but real).
 */
export function parseDiffShortstat(s: string): { diff_loc: number; files_changed: number } {
  if (!s) return { diff_loc: 0, files_changed: 0 };
  // Format: " 4 files changed, 123 insertions(+), 45 deletions(-)"
  const filesMatch = s.match(/(\d+)\s+files?\s+changed/);
  const insMatch = s.match(/(\d+)\s+insertions?/);
  const delMatch = s.match(/(\d+)\s+deletions?/);
  const ins = insMatch ? Number(insMatch[1]) : 0;
  const del = delMatch ? Number(delMatch[1]) : 0;
  return {
    diff_loc: ins + del,
    files_changed: filesMatch ? Number(filesMatch[1]) : 0,
  };
}

/**
 * Pull the numeric PR number off a github.com/<o>/<r>/pull/N URL.
 * Returns undefined when the URL doesn't match the expected shape
 * (defensive — caller should still enqueue with pr_number=undefined
 * and let the reviewer-prompt builder throw the schema rejection).
 */
export function extractPrNumber(pr_url: string): number | undefined {
  const m = pr_url.match(/\/pull\/(\d+)/);
  return m ? Number(m[1]) : undefined;
}

/**
 * Submit the review-graph workflow. Failure is logged but never
 * propagates — the implementor's work has already shipped (PR exists);
 * a missing review is not worse than the pre-Phase-2 baseline where
 * Copilot was the only reviewer.
 */
export async function enqueueReviewGraph(
  input: EnqueueReviewGraphInput,
): Promise<EnqueueReviewGraphResult> {
  const log = input.log ?? ((l: string) => console.log(l));

  if (!input.pr_url) {
    return { enqueued: false };
  }

  // If a pre-built PrMeta was provided (pr-event-ingester path),
  // thread it through unchanged. Otherwise fall back to the
  // worktree-derived rebuild (programmer-success path).
  const reviewGraphInput = input.pr_meta
    ? {
        parent_workflow_id: input.parent_workflow_id,
        pr_meta: input.pr_meta,
        entry: input.entry,
        repo: input.repo,
      }
    : buildReviewGraphInput(
        input.parent_workflow_id,
        input.pr_url,
        input.worktree,
        input.entry,
        input.repo,
      );

  // Reviewer chain workflow_id derives from the parent. Keep it
  // greppable so the operator can correlate `swarm-<entry>-<ts>` to
  // its review-graph chain.
  const reviewGraphId = `${input.parent_workflow_id}-review-graph`;

  try {
    await input.client.workflow.start<typeof reviewGraphWorkflow>(REVIEW_GRAPH_WORKFLOW_NAME, {
      args: [reviewGraphInput],
      taskQueue: input.taskQueue,
      workflowId: reviewGraphId,
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'warn',
        component: 'review-graph-dispatch',
        msg: 'submit failed; PR will rely on Copilot R0 only',
        parent_workflow_id: input.parent_workflow_id,
        pr_url: input.pr_url,
        error: msg,
      }),
    );
    return { enqueued: false, error: msg };
  }

  log(
    JSON.stringify({
      ts: new Date().toISOString(),
      level: 'info',
      component: 'review-graph-dispatch',
      msg: 'review-graph enqueued',
      parent_workflow_id: input.parent_workflow_id,
      review_graph_id: reviewGraphId,
      pr_url: input.pr_url,
      diff_loc: reviewGraphInput.pr_meta.diff_loc,
      files_changed: reviewGraphInput.pr_meta.files_changed,
    }),
  );

  return { enqueued: true, workflow_id: reviewGraphId };
}
