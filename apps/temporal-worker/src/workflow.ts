import { proxyActivities } from '@temporalio/workflow';
import type { ExecutionRequest } from '@chitin/contracts';
import type { ActivityResult } from './activity-types.ts';

// startToCloseTimeout is the Temporal-level kill switch on the activity.
// It must dominate every wall_timeout_s the dispatcher or review-graph
// can configure, otherwise Temporal cuts the agent short before the
// activity's own SIGKILL fires (slice 7a). Current ceilings:
//   - dispatcher.ts: implementor T2/T3/T4 = 1800s (30 min)
//   - review-graph.ts: R3 = 1800s (30 min)
// Plus ~60s buffer for the activity's startup + its own SIGKILL grace.
// 31 minutes covers both. Bump in lockstep if any tier's wall climbs.
const { runAgentTurn } = proxyActivities<{
  runAgentTurn(req: ExecutionRequest): Promise<ActivityResult>;
}>({
  startToCloseTimeout: '31 minutes',
  retry: { maximumAttempts: 1 },
});

export async function executeRequestWorkflow(req: ExecutionRequest): Promise<ActivityResult> {
  return runAgentTurn(req);
}
