// Test helpers for review-graph-workflow tests. Mock reviewer
// dispatchers (canned outputs per tier) + a tiny mock-output builder
// that synthesizes the agent's stdout_tail from a structured
// ReviewerOutput.

import type { ReviewerOutput, ReviewTier } from '../src/review-graph.ts';
import type { ReviewerDispatch } from '../src/review-graph-workflow.ts';
import type { ActivityResult } from '../src/activity-types.ts';

export const REVIEW_MARKER_FOR_TEST = '<<<REVIEW>>>';

/**
 * Synthesize the agent's stdout_tail from a structured ReviewerOutput.
 * Mirrors what the agent is instructed to emit by
 * STRUCTURED_OUTPUT_INSTRUCTIONS in reviewer-prompts.ts.
 */
export function buildMockReviewerOutput(
  out: Partial<ReviewerOutput> & { decision: ReviewerOutput['decision']; confidence: ReviewerOutput['confidence'] },
): ActivityResult {
  const full: ReviewerOutput = {
    decision: out.decision,
    confidence: out.confidence,
    findings: out.findings ?? [],
  };
  return {
    exit_code: 0,
    stdout_tail: `Agent walking through review...\n${REVIEW_MARKER_FOR_TEST}${JSON.stringify(full)}`,
    stderr_tail: '',
    duration_ms: 60_000,
  };
}

/**
 * Build a `ReviewerDispatch` callback that returns canned results
 * keyed by review tier. The dispatcher infers the tier from the
 * request's workflow_id (which the workflow loop sets to
 * `<parent>-revr<tier-lower>`).
 */
export function makeMockDispatcher(
  outputs: Partial<Record<ReviewTier, ActivityResult>>,
): ReviewerDispatch {
  return async (req) => {
    const tierMatch = req.workflow_id.match(/-revr([0-4])$/);
    if (!tierMatch) {
      throw new Error(`mock dispatcher: workflow_id ${req.workflow_id} doesn't end with -revrN`);
    }
    const tier = `R${tierMatch[1]}` as ReviewTier;
    const canned = outputs[tier];
    if (!canned) {
      throw new Error(`mock dispatcher: no canned output for ${tier} (provided: ${Object.keys(outputs).join(', ')})`);
    }
    return canned;
  };
}
