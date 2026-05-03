// Activity that enqueues a comment-responder workflow.
//
// Called from reviewGraphWorkflow after the §5 escalation chain
// terminates with action='request-changes'. Workflows can't spawn
// arbitrary top-level workflows directly (executeChild creates a
// parent-child relationship; we want a peer); the indirection
// through an activity gives us a normal Temporal client to call
// `workflow.start()` on.
//
// Failure-tolerant: if the enqueue fails (Temporal flaky, dispatch
// error), we log and return enqueued=false. The reviewer chain has
// already completed; missing the responder dispatch isn't worse
// than the pre-this-PR baseline.
//
// Why a new activity instead of folding into runGatekeeperNotify:
// the gatekeeper makes auto-merge decisions; this activity dispatches
// follow-on work. Conceptually distinct, even though both fire after
// reviewGraphWorkflow's loop. A future "post-loop chain" abstraction
// could fold them, but that's premature.

import { Connection, Client } from '@temporalio/client';
import { enqueueCommentResponder } from './dispatch.ts';

const TEMPORAL_ADDRESS = process.env.TEMPORAL_ADDRESS ?? '127.0.0.1:7233';
const TASK_QUEUE = 'chitin-worker-q';

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

/**
 * Open a temporal connection, dispatch the comment-responder
 * workflow, close the connection. Each call is independent — we
 * don't pool the connection across activity invocations because
 * Temporal activities are short-lived and the call frequency here
 * is low (at most once per review-graph completion).
 */
export async function runCommentResponderEnqueue(
  input: EnqueueCommentResponderActivityInput,
): Promise<EnqueueCommentResponderActivityResult> {
  if (!input.pr_url) {
    return { enqueued: false, reason: 'missing pr_url' };
  }
  if (!input.repo) {
    return { enqueued: false, reason: 'missing repo' };
  }

  let connection: Connection | undefined;
  try {
    connection = await Connection.connect({ address: TEMPORAL_ADDRESS });
    const client = new Client({ connection, namespace: 'default' });
    const result = await enqueueCommentResponder({
      client,
      taskQueue: TASK_QUEUE,
      pr_url: input.pr_url,
      repo: input.repo,
    });
    if (!result.enqueued) {
      return {
        enqueued: false,
        reason: result.error ?? 'enqueue helper returned not-enqueued',
      };
    }
    return { enqueued: true, workflow_id: result.workflow_id };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return { enqueued: false, reason: `activity error: ${msg}` };
  } finally {
    if (connection) {
      await connection.close().catch(() => {
        // Connection close error doesn't change the outcome — the
        // workflow is already started (or the start failed before
        // close); swallow.
      });
    }
  }
}
