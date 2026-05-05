import { describe, expect, it } from 'vitest';
import { executeRequestWorkflow } from '../src/workflow';

// Drift gate (closes #82 finding #7). Several remaining callers
// (comment-responder/dispatch.ts, peer-reviewer/dispatch.ts,
// dispatcher.ts) still dispatch the executeRequestWorkflow by string
// name pre-Group-B-cut-over:
//
//   const WORKFLOW_NAME = 'executeRequestWorkflow';
//   await client.workflow.start<typeof executeRequestWorkflow>(WORKFLOW_NAME, {...})
//
// The type-only import gives compile-time linkage, but if the export
// gets renamed the string constant goes stale silently. This test
// asserts the live function's .name still matches the dispatchers'
// string constant.
//
// reviewGraphWorkflow's drift gate was removed when review-graph-
// dispatch.ts cut over from Temporal to lobster spawn — the workflow
// no longer dispatched by string name; lobster reads the .lobster
// file path instead.

const EXPECTED_EXECUTE_REQUEST_WORKFLOW_NAME = 'executeRequestWorkflow';

describe('Workflow name drift gate', () => {
  it('executeRequestWorkflow.name matches the dispatcher string constant', () => {
    expect(executeRequestWorkflow.name).toBe(EXPECTED_EXECUTE_REQUEST_WORKFLOW_NAME);
  });
});
