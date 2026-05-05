// Manual operator one-shot dispatch script.
//
// Pre-cut-over (Temporal): connected to localhost:7233, started
// executeRequestWorkflow, awaited handle.result(). Now: directly
// invokes runAgentTurn — same code path the Temporal workflow
// previously wrapped, just no orchestration round-trip. Saves one
// process boundary + the temporal-server dependency.
//
// Usage:
//   pnpm exec tsx apps/runner/src/submit.ts
//   PROMPT='write a haiku' DRIVER=copilot pnpm exec tsx ...
//
// Behavior contract preserved: writes tmp/result-<workflow_id>.json
// envelope so apply-workflow-result.ts continues to work unchanged.

import { ExecutionRequestSchema, type ExecutionRequest } from '@chitin/contracts';
import { mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { runAgentTurn } from './activity.ts';

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
      'Use the Bash tool to run exactly: echo hello-from-runner. Then stop.',
    // Slice 5: when BASE_REF is set, the activity creates a worktree and
    // the agent's edits are durable. Apply-step pushes + opens PR.
    ...(process.env.BASE_REF ? { base_ref: process.env.BASE_REF } : {}),
  });

  console.log(`[submit] running workflow_id=${workflowId}`);
  const result = await runAgentTurn(req);
  console.log('[submit] result:', JSON.stringify(result, null, 2));

  // Slice 5 envelope contract — apply-workflow-result.ts consumes this.
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
}

main().catch((err) => {
  console.error('[submit] fatal:', err);
  process.exit(1);
});
