import { Connection, Client } from '@temporalio/client';
import { ExecutionRequestSchema, type ExecutionRequest } from '@chitin/contracts';
// Workflow code is type-only here. A runtime import would pull
// `@temporalio/workflow` into the client process, which is meant to run
// only inside the Temporal V8 isolate. The workflow is dispatched by name.
import type { executeRequestWorkflow } from './workflow.ts';
import { mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { ActivityResult } from './activity-types.ts';

const WORKFLOW_NAME = 'executeRequestWorkflow';

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
    allowed_drivers: [(process.env.DRIVER ?? 'copilot') as 'copilot' | 'openclaw-glm-flash' | 'openclaw-glm-cloud' | 'openclaw-deepseek'],
    network_policy: 'allowlist',
    write_policy: 'none',
    bounds: {
      max_tool_calls: parseInt(process.env.MAX_TOOL_CALLS ?? '5', 10),
      max_cost_usd: 0,
      wall_timeout_s: parseInt(process.env.WALL_TIMEOUT_S ?? '120', 10),
    },
    prompt:
      process.env.PROMPT ??
      'Use the Bash tool to run exactly: echo hello-from-temporal-worker. Then stop.',
    // Slice 5: when BASE_REF is set, the activity creates a worktree and
    // the agent's edits are durable. Apply-step pushes + opens PR.
    ...(process.env.BASE_REF ? { base_ref: process.env.BASE_REF } : {}),
  });

  const conn = await Connection.connect({ address: '127.0.0.1:7233' });
  const client = new Client({ connection: conn, namespace: 'default' });

  console.log(`[submit] starting workflow_id=${workflowId}`);
  const handle = await client.workflow.start<typeof executeRequestWorkflow>(WORKFLOW_NAME, {
    args: [req],
    taskQueue: TASK_QUEUE,
    workflowId,
  });

  const result = (await handle.result()) as ActivityResult;
  console.log('[submit] workflow result:', JSON.stringify(result, null, 2));
  // Slice 5: write a result envelope file so apply-workflow-result.ts can
  // consume it without re-querying Temporal. Always written; apply step
  // is a no-op when result.worktree is absent.
  const tmpDir = resolve(process.cwd(), 'tmp');
  mkdirSync(tmpDir, { recursive: true });
  const envelopePath = resolve(tmpDir, `result-${workflowId}.json`);
  writeFileSync(
    envelopePath,
    JSON.stringify(
      {
        workflow_id: workflowId,
        result,
        pr_title: process.env.PR_TITLE,
        pr_body: process.env.PR_BODY,
      },
      null,
      2,
    ),
  );
  console.log(`[submit] envelope → ${envelopePath}`);
  await conn.close();
}

main().catch((err) => {
  console.error('[submit] fatal:', err);
  process.exit(1);
});
