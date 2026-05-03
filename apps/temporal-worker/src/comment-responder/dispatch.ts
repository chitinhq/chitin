// Dispatch helper for the comment-responder agent role (#207).
//
// Mirrors peer-reviewer-dispatch.ts in shape but with the bounds the
// comment-responder needs:
//   - write_policy=branch: agent commits + pushes directly to the PR's
//     branch via `gh pr checkout` + `git push`. Activity skips the
//     worktree+apply pipeline because base_ref is absent.
//   - max_tool_calls=80: 5-15 comments × (read context + decide + edit
//     + reply) + tests + commit + push fits in this budget.
//   - wall_timeout=1800s: tracks the R3 reviewer ceiling.
//
// Caller (pr-event-ingester or downstream) is responsible for the
// "should we dispatch?" decision — typically: there are unresolved
// reviewer comments AND no comment-responder workflow is currently
// running for this PR.

import { randomUUID } from 'node:crypto';
import type { Client } from '@temporalio/client';
import { ExecutionRequestSchema } from '@chitin/contracts';
import type { ExecutionRequest, DriverId, Tier } from '@chitin/contracts';
import type { executeRequestWorkflow } from '../workflow.ts';
import type { BacklogEntry } from '../grooming/parse-backlog.ts';
import { buildCommentResponderPrompt } from './prompt.ts';

const EXECUTE_REQUEST_WORKFLOW_NAME = 'executeRequestWorkflow';

export interface EnqueueCommentResponderInput {
  client: Client;
  taskQueue: string;
  /** PR URL the agent is responding to. Required. */
  pr_url: string;
  /** Repo slug for the agent's gh calls. */
  repo: string;
  /** Optional driver override. Default: copilot at T2 (Sonnet) for the
   *  judgment to evaluate comments on merit. Operator can flip via
   *  the existing tier-driver env override. */
  driver?: DriverId;
  /** Optional tier override. Default: T2. */
  tier?: Tier;
  /** Optional log sink (defaults to console.log). */
  log?: (line: string) => void;
}

export interface EnqueueCommentResponderResult {
  enqueued: boolean;
  workflow_id?: string;
  error?: string;
}

/**
 * Stable workflow id for the comment-respond cycle on a given PR.
 * Re-dispatches against this id are governed by two policies set
 * explicitly on the start call below:
 *   - workflowIdReusePolicy: default (ALLOW_DUPLICATE) — a new
 *     run can start once the previous run is in a Closed state.
 *   - workflowIdConflictPolicy: USE_EXISTING — if a run is still
 *     RUNNING, the start returns a handle to the running workflow
 *     instead of throwing WorkflowExecutionAlreadyStartedError.
 * Combined: concurrent ingester ticks against the same PR don't
 * inflate the ingester's `errors` counter; a closed run for the
 * same PR is naturally re-runnable on subsequent ticks.
 *
 * Belt-and-suspenders: the ingester also has a "no responder
 * running for this PR" check upstream of enqueue, so the
 * USE_EXISTING path is the safety net, not the primary defense.
 */
export function commentResponderWorkflowIdForPr(prNumber: number): string {
  return `comment-respond-pr-${prNumber}`;
}

export function extractPrNumber(pr_url: string): number {
  const m = pr_url.match(/\/pull\/(\d+)/);
  if (!m) {
    throw new Error(`comment-responder-dispatch: pr_url does not contain /pull/<n>: ${pr_url}`);
  }
  return Number(m[1]);
}

export function buildCommentResponderEntry(pr_url: string, repo: string): BacklogEntry {
  const prNumber = extractPrNumber(pr_url);
  return {
    id: `comment-respond-pr-${prNumber}`,
    status: 'ready',
    role: 'comment-responder',
    description:
      `Address unresolved review comments on PR ${pr_url} (repo: ${repo}). ` +
      `Read each via gh api, evaluate on merit (do NOT dismiss as noise), ` +
      `apply/dismiss/escalate per comment, push at most one fix commit, ` +
      `reply to each thread, post a summary comment.`,
    rawFrontmatter: '',
    rawSection: '',
  };
}

export function buildCommentResponderRequest(
  input: Pick<EnqueueCommentResponderInput, 'pr_url' | 'repo' | 'driver' | 'tier'>,
): ExecutionRequest {
  const prNumber = extractPrNumber(input.pr_url);
  const workflowId = commentResponderWorkflowIdForPr(prNumber);
  const entry = buildCommentResponderEntry(input.pr_url, input.repo);
  const driver = input.driver ?? 'copilot';
  const tier = input.tier ?? 'T2';

  // Validate via the schema rather than asserting. Other dispatch
  // paths in this worker use ExecutionRequestSchema.parse() so
  // contract drift fails fast at the dispatch site, not later
  // inside the workflow. (Copilot review #211 round-2 #6.)
  return ExecutionRequestSchema.parse({
    schema_version: '1',
    workflow_id: workflowId,
    // run_id must be UNIQUE PER EXECUTION — the kernel writes
    // canonical events to `.chitin/events-<run_id>.jsonl`, so a
    // stable run_id collapses repeat dispatches' telemetry into a
    // single file and breaks per-run auditability. randomUUID()
    // avoids the millisecond-collision risk that a bare Date.now()
    // suffix has when concurrent dispatches fire in the same tick.
    // (Copilot review #212 #3 + round-2 #1; same shape as
    // peer-reviewer/dispatch.ts.)
    run_id: `${workflowId}-${randomUUID()}`,
    repo: input.repo,
    task_class: 'bug_fix',              // closest fit — addressing review-flagged issues
    risk_level: 'medium',               // commits land on PR branch
    allowed_drivers: [driver],
    network_policy: 'allowlist',        // gh CLI + npm registry
    write_policy: 'branch',             // commits to the PR's branch
    bounds: {
      max_tool_calls: 80,
      max_cost_usd: 0,
      wall_timeout_s: 1800,             // 30 min — same ceiling as R3
    },
    prompt: buildCommentResponderPrompt(entry),
    role: 'comment-responder',
    tier,
    // base_ref intentionally absent — agent does its own
    // `gh pr checkout` so a worktree set up by the activity would
    // collide with the agent's branch state.
  });
}

/**
 * Spawn the comment-responder workflow. Failure is logged but never
 * propagates — missing the dispatch leaves the comments unaddressed,
 * but doesn't break the PR's existing state.
 */
export async function enqueueCommentResponder(
  input: EnqueueCommentResponderInput,
): Promise<EnqueueCommentResponderResult> {
  const log = input.log ?? ((l: string) => console.log(l));

  let request: ExecutionRequest;
  try {
    request = buildCommentResponderRequest(input);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(JSON.stringify({
      ts: new Date().toISOString(),
      level: 'warn',
      component: 'comment-responder-dispatch',
      msg: 'request build failed',
      pr_url: input.pr_url,
      error: msg,
    }));
    return { enqueued: false, error: msg };
  }

  try {
    await input.client.workflow.start<typeof executeRequestWorkflow>(EXECUTE_REQUEST_WORKFLOW_NAME, {
      args: [request],
      taskQueue: input.taskQueue,
      workflowId: request.workflow_id,
      // Stable per-PR workflow id: USE_EXISTING returns a handle to
      // the running workflow rather than throwing
      // WorkflowExecutionAlreadyStartedError on concurrent ingester
      // ticks. (Copilot review #211 round-2 #2.)
      workflowIdConflictPolicy: 'USE_EXISTING',
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(JSON.stringify({
      ts: new Date().toISOString(),
      level: 'warn',
      component: 'comment-responder-dispatch',
      msg: 'submit failed; comments will remain unaddressed until next cycle',
      pr_url: input.pr_url,
      error: msg,
    }));
    return { enqueued: false, error: msg };
  }

  log(JSON.stringify({
    ts: new Date().toISOString(),
    level: 'info',
    component: 'comment-responder-dispatch',
    msg: 'comment-responder enqueued',
    workflow_id: request.workflow_id,
    pr_url: input.pr_url,
  }));

  return { enqueued: true, workflow_id: request.workflow_id };
}
