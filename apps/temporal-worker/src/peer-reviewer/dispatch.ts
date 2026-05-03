// Dispatch helper for the peer-reviewer agent role (#207).
//
// Mirrors review-graph-dispatch.ts in shape: takes the bits the
// pr-event-ingester (or any other caller) has, produces an
// ExecutionRequest, spawns executeRequestWorkflow with a stable
// workflow id for dedup. The agent self-bounds (read-only) and does
// its own gh CLI reads + posts a single review comment.
//
// The dispatch is base_ref-less (no worktree). The peer-reviewer is
// read-only so the activity's worktree + apply-step machinery is
// pure overhead — and skipping it avoids the wrong-bounds problem
// flagged on PR #207, where apply-workflow-result would otherwise
// try to push an empty diff as a no-op PR.

import type { Client } from '@temporalio/client';
import type { ExecutionRequest, DriverId, Tier } from '@chitin/contracts';
import type { executeRequestWorkflow } from '../workflow.ts';
import type { BacklogEntry } from '../grooming/parse-backlog.ts';
import { buildPeerReviewerPrompt } from './prompt.ts';

const EXECUTE_REQUEST_WORKFLOW_NAME = 'executeRequestWorkflow';

export interface EnqueuePeerReviewerInput {
  client: Client;
  taskQueue: string;
  /** PR URL for the agent to review. Required — the prompt's step-0
   *  guard refuses to act without a PR URL in the entry detail. */
  pr_url: string;
  /** Repo slug, e.g. 'chitinhq/chitin'. Used by the agent's gh calls. */
  repo: string;
  /** Optional driver override. Default: copilot at T2 (Sonnet) for
   *  enough judgment to do an adversarial review. */
  driver?: DriverId;
  /** Optional tier override. Default: T2. */
  tier?: Tier;
  /** Optional log sink (defaults to console.log). Tests inject. */
  log?: (line: string) => void;
}

export interface EnqueuePeerReviewerResult {
  enqueued: boolean;
  workflow_id?: string;
  error?: string;
}

/**
 * Stable workflow id for the peer-review on a given PR. Same id
 * across re-dispatches lets the ingester dedup against an already-
 * running review without hitting Temporal's id-already-in-use path.
 */
export function peerReviewerWorkflowIdForPr(prNumber: number): string {
  return `peer-review-pr-${prNumber}`;
}

export function extractPrNumber(pr_url: string): number {
  const m = pr_url.match(/\/pull\/(\d+)/);
  if (!m) {
    throw new Error(`peer-reviewer-dispatch: pr_url does not contain /pull/<n>: ${pr_url}`);
  }
  return Number(m[1]);
}

/**
 * Build the synthetic BacklogEntry the prompt builder consumes.
 * Entry description carries the PR URL — the prompt's step-0 guard
 * scans for `https://github.com/<o>/<r>/pull/<n>` and refuses to
 * act if absent.
 */
export function buildPeerReviewerEntry(pr_url: string, repo: string): BacklogEntry {
  const prNumber = extractPrNumber(pr_url);
  return {
    id: `peer-review-pr-${prNumber}`,
    status: 'ready',
    role: 'peer-reviewer',
    description:
      `Peer-review PR ${pr_url} (repo: ${repo}). ` +
      `Read the diff, evaluate against the five-axis checklist, ` +
      `post one structured review comment. See SKILL.md for full workflow.`,
    rawFrontmatter: '',
    rawSection: '',
  };
}

/**
 * Build the ExecutionRequest the peer-reviewer agent will receive.
 * Pure — exported so tests can pin the field-mapping without standing
 * up a Temporal client.
 */
export function buildPeerReviewerRequest(
  input: Pick<EnqueuePeerReviewerInput, 'pr_url' | 'repo' | 'driver' | 'tier'>,
): ExecutionRequest {
  const prNumber = extractPrNumber(input.pr_url);
  const workflowId = peerReviewerWorkflowIdForPr(prNumber);
  const entry = buildPeerReviewerEntry(input.pr_url, input.repo);
  const driver = input.driver ?? 'copilot';
  const tier = input.tier ?? 'T2';

  return {
    schema_version: '1',
    workflow_id: workflowId,
    // run_id must be UNIQUE PER EXECUTION — the kernel writes
    // canonical events to `.chitin/events-<run_id>.jsonl`, so a
    // stable run_id collapses repeat dispatches' telemetry into a
    // single file and breaks per-run auditability. workflow_id is
    // stable per PR (for Temporal dedup); run_id is per-dispatch.
    // (Copilot review #212 #2.)
    run_id: `${workflowId}-${Date.now()}`,
    repo: input.repo,
    task_class: 'exploration',          // closest fit for read-only review
    risk_level: 'low',
    allowed_drivers: [driver],
    network_policy: 'allowlist',        // gh CLI only
    write_policy: 'none',               // strictly read-only
    bounds: {
      max_tool_calls: 30,
      max_cost_usd: 0,
      wall_timeout_s: 900,              // 15 min — peer review shouldn't be slow
    },
    prompt: buildPeerReviewerPrompt(entry),
    role: 'peer-reviewer',
    tier,
    // base_ref intentionally absent — no worktree, no apply step.
  } as ExecutionRequest;
}

/**
 * Spawn the peer-reviewer workflow. Failure is logged but never
 * propagates — peer review is consultative; missing it is not worse
 * than the pre-PR baseline where Copilot R0 was the only review.
 */
export async function enqueuePeerReviewer(
  input: EnqueuePeerReviewerInput,
): Promise<EnqueuePeerReviewerResult> {
  const log = input.log ?? ((l: string) => console.log(l));

  let request: ExecutionRequest;
  try {
    request = buildPeerReviewerRequest(input);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(JSON.stringify({
      ts: new Date().toISOString(),
      level: 'warn',
      component: 'peer-reviewer-dispatch',
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
    });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(JSON.stringify({
      ts: new Date().toISOString(),
      level: 'warn',
      component: 'peer-reviewer-dispatch',
      msg: 'submit failed; PR review will rely on R0 only',
      pr_url: input.pr_url,
      error: msg,
    }));
    return { enqueued: false, error: msg };
  }

  log(JSON.stringify({
    ts: new Date().toISOString(),
    level: 'info',
    component: 'peer-reviewer-dispatch',
    msg: 'peer-reviewer enqueued',
    workflow_id: request.workflow_id,
    pr_url: input.pr_url,
  }));

  return { enqueued: true, workflow_id: request.workflow_id };
}
