import { describe, expect, it } from 'vitest';
import { executeRequestWorkflow, reviewGraphWorkflow } from '../src/workflow';
import { REVIEW_GRAPH_WORKFLOW_NAME } from '../src/review-graph-dispatch';

// Drift gate (closes #82 finding #7). Several callers (submit.ts,
// dispatcher.ts, groom-pass.ts) dispatch workflows by string name:
//
//   const WORKFLOW_NAME = 'executeRequestWorkflow';
//   await client.workflow.start<typeof executeRequestWorkflow>(WORKFLOW_NAME, {...})
//
// The type-only import gives compile-time linkage, but if the export
// gets renamed the string constant goes stale silently — TypeScript
// won't catch it because the constant is just a literal. This test
// asserts that the live function's .name matches the string the
// dispatchers use.
//
// Single source of truth: the dispatch-layer constants are the
// expected names. A divergence forces an explicit decision (rename
// both, or keep the string and document why).

const EXPECTED_EXECUTE_REQUEST_WORKFLOW_NAME = 'executeRequestWorkflow';

describe('Workflow name drift gate', () => {
  it('executeRequestWorkflow.name matches the dispatcher string constant', () => {
    expect(executeRequestWorkflow.name).toBe(EXPECTED_EXECUTE_REQUEST_WORKFLOW_NAME);
  });

  it('reviewGraphWorkflow.name matches REVIEW_GRAPH_WORKFLOW_NAME', () => {
    expect(reviewGraphWorkflow.name).toBe(REVIEW_GRAPH_WORKFLOW_NAME);
  });
});
