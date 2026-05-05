// Activity that enqueues a comment-responder agent run.
//
// Called from reviewGraphWorkflow / lobster review-graph after the §5
// escalation chain terminates with action='request-changes'.
//
// Pre-cut-over: opened a Temporal connection per call to dispatch a
// new workflow (because workflows can't spawn arbitrary top-level
// peers; the activity gave us a normal client). Post-cut-over:
// dispatcher is just a detached spawn (via spawnExecuteRequest) — no
// Temporal client needed, no connection lifecycle to manage.
//
// Failure-tolerant: if the enqueue fails (binary missing, dedup
// conflict, etc.), we log and return enqueued=false. The reviewer
// chain has already completed; missing the responder dispatch isn't
// worse than the pre-this-PR baseline.

import { enqueueCommentResponder } from './dispatch.ts';

export interface EnqueueCommentResponderActivityInput {
  pr_url: string;
  repo: string;
}

export interface EnqueueCommentResponderActivityResult {
  enqueued: boolean;
  workflow_id?: string;
  /** Reason when enqueued=false. */
  reason?: string;
}

export async function runCommentResponderEnqueue(
  input: EnqueueCommentResponderActivityInput,
): Promise<EnqueueCommentResponderActivityResult> {
  if (!input.pr_url) {
    return { enqueued: false, reason: 'missing pr_url' };
  }
  if (!input.repo) {
    return { enqueued: false, reason: 'missing repo' };
  }

  try {
    const result = await enqueueCommentResponder({
      pr_url: input.pr_url,
      repo: input.repo,
    });
    if (!result.enqueued) {
      return {
        enqueued: false,
        workflow_id: result.workflow_id,
        reason: result.error ?? 'enqueue helper returned not-enqueued',
      };
    }
    return { enqueued: true, workflow_id: result.workflow_id };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return { enqueued: false, reason: `activity error: ${msg}` };
  }
}
