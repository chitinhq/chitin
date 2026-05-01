import { Connection, Client } from '@temporalio/client';
import { ExecutionRequestSchema, type ExecutionRequest } from '@chitin/contracts';
import { executeRequestWorkflow } from './workflow.ts';

const TASK_QUEUE = 'chitin-worker-q';

async function main() {
  const workflowId = process.env.WORKFLOW_ID ?? `wf-${Date.now()}`;
  const runId = `${workflowId}-attempt-1`;

  const req: ExecutionRequest = ExecutionRequestSchema.parse({
    schema_version: '1',
    workflow_id: workflowId,
    run_id: runId,
    repo: 'chitinhq/chitin',
    task_class: 'exploration',
    risk_level: 'low',
    allowed_drivers: ['claude-code'],
    network_policy: 'allowlist',
    write_policy: 'none',
    bounds: { max_tool_calls: 5, max_cost_usd: 0, wall_timeout_s: 120 },
    prompt:
      process.env.PROMPT ??
      'Use the Bash tool to run exactly: echo hello-from-temporal-worker. Then stop.',
  });

  const conn = await Connection.connect({ address: '127.0.0.1:7233' });
  const client = new Client({ connection: conn, namespace: 'default' });

  console.log(`[submit] starting workflow_id=${workflowId}`);
  const handle = await client.workflow.start(executeRequestWorkflow, {
    args: [req],
    taskQueue: TASK_QUEUE,
    workflowId,
  });

  const result = await handle.result();
  console.log('[submit] workflow result:', JSON.stringify(result, null, 2));
  await conn.close();
}

main().catch((err) => {
  console.error('[submit] fatal:', err);
  process.exit(1);
});
