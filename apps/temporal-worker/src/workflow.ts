import { proxyActivities } from '@temporalio/workflow';
import type { ExecutionRequest } from '@chitin/contracts';
import type { ActivityResult } from './activity-types.ts';

const { runAgentTurn } = proxyActivities<{
  runAgentTurn(req: ExecutionRequest): Promise<ActivityResult>;
}>({
  startToCloseTimeout: '15 minutes',
  retry: { maximumAttempts: 1 },
});

export async function executeRequestWorkflow(req: ExecutionRequest): Promise<ActivityResult> {
  return runAgentTurn(req);
}
