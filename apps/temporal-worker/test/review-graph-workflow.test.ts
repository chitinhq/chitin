import { describe, expect, it } from 'vitest';
import {
  REVIEW_MARKER_FOR_TEST,
  buildMockReviewerOutput,
  makeMockDispatcher,
} from './review-graph-workflow.helpers.ts';
import {
  runReviewGraphLoop,
  __test__,
  type ReviewGraphInput,
  type ReviewerDispatch,
} from '../src/review-graph-workflow.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';
import type { PrMeta, ReviewerFinding } from '../src/review-graph.ts';

const { buildReviewerRequest, formatFindingsForNextTier, tierAsStepIndex } = __test__;

function makeEntry(overrides: Partial<BacklogEntry> = {}): BacklogEntry {
  return {
    id: 'sample',
    status: 'ready',
    description: 'sample',
    rawFrontmatter: '```yaml\nid: sample\n```',
    rawSection: '### sample',
    tier: 'T1',
    file: 'apps/temporal-worker/src/foo.ts',
    ...overrides,
  };
}

function makePrMeta(overrides: Partial<PrMeta> = {}): PrMeta {
  return {
    diff_loc: 50,
    files_changed: 2,
    files: ['apps/temporal-worker/src/foo.ts'],
    pr_number: 132,
    pr_url: 'https://github.com/chitinhq/chitin/pull/132',
    ...overrides,
  };
}

function makeInput(overrides: Partial<ReviewGraphInput> = {}): ReviewGraphInput {
  return {
    parent_workflow_id: 'swarm-parent-12345',
    pr_meta: makePrMeta(),
    entry: makeEntry(),
    repo: 'chitinhq/chitin',
    ...overrides,
  };
}

// ─── Loop control flow — happy path ──────────────────────────────────────

describe('runReviewGraphLoop — happy path', () => {
  it('approves at R1 when first reviewer says approve+high', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({ decision: 'approve', confidence: 'high' }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('approve');
    expect(result.final_tier).toBe('R1');
    expect(result.tier_log).toHaveLength(1);
    expect(result.tier_log[0].tier).toBe('R1');
    expect(result.tier_log[0].parsed).toBe(true);
  });

  it('starts at R2 when computeStartingTier bumps (T3 implementor)', async () => {
    const dispatcher = makeMockDispatcher({
      R2: buildMockReviewerOutput({ decision: 'approve', confidence: 'high' }),
    });
    const result = await runReviewGraphLoop(
      makeInput({ entry: makeEntry({ tier: 'T3' }) }),
      dispatcher,
    );
    expect(result.action).toBe('approve');
    expect(result.final_tier).toBe('R2');
    expect(result.tier_log[0].tier).toBe('R2');
  });

  it('starts at R3 when kernel internals are touched', async () => {
    const dispatcher = makeMockDispatcher({
      R3: buildMockReviewerOutput({ decision: 'approve', confidence: 'high' }),
    });
    const result = await runReviewGraphLoop(
      makeInput({ pr_meta: makePrMeta({ files: ['go/execution-kernel/internal/gov/normalize.go'] }) }),
      dispatcher,
    );
    expect(result.action).toBe('approve');
    expect(result.final_tier).toBe('R3');
  });
});

// ─── Loop control flow — escalation ──────────────────────────────────────

describe('runReviewGraphLoop — escalation', () => {
  it('escalates from R1 → R2 when R1 returns decision: escalate', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({ decision: 'escalate', confidence: 'medium' }),
      R2: buildMockReviewerOutput({ decision: 'approve', confidence: 'high' }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('approve');
    expect(result.final_tier).toBe('R2');
    expect(result.tier_log.map((e) => e.tier)).toEqual(['R1', 'R2']);
  });

  it('escalates R1 → R2 → R3 when both lower tiers escalate', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({ decision: 'escalate', confidence: 'medium' }),
      R2: buildMockReviewerOutput({ decision: 'escalate', confidence: 'medium' }),
      R3: buildMockReviewerOutput({ decision: 'approve', confidence: 'high' }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('approve');
    expect(result.final_tier).toBe('R3');
    expect(result.tier_log.map((e) => e.tier)).toEqual(['R1', 'R2', 'R3']);
  });

  it('escalates to operator when R3 returns confidence: low (the headline rule)', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({ decision: 'escalate', confidence: 'medium' }),
      R2: buildMockReviewerOutput({ decision: 'escalate', confidence: 'medium' }),
      R3: buildMockReviewerOutput({
        decision: 'request_changes',
        confidence: 'low',
        findings: [{ severity: '🔴', file: 'x.ts', category: 'bug', summary: 'unsure' }],
      }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('escalate-to-operator');
    expect(result.final_tier).toBe('R3');
  });

  it('bumps from R1 to R2 when R1 returns confidence: low (R1 not the operator escalation point)', async () => {
    // The operator-escalate-on-confidence-low rule is R3-only. At R1
    // / R2, low confidence means "bump to a heavier reviewer" — let
    // R3 handle the call before pinging the operator.
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({ decision: 'request_changes', confidence: 'low' }),
      R2: buildMockReviewerOutput({ decision: 'approve', confidence: 'high' }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('approve');
    expect(result.final_tier).toBe('R2');
    expect(result.tier_log.map((e) => e.tier)).toEqual(['R1', 'R2']);
  });

  it('escalates past R3 when R3 returns decision: escalate (chain saturates at R4 → operator)', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({ decision: 'escalate', confidence: 'medium' }),
      R2: buildMockReviewerOutput({ decision: 'escalate', confidence: 'medium' }),
      R3: buildMockReviewerOutput({ decision: 'escalate', confidence: 'medium' }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('escalate-to-operator');
    expect(result.final_tier).toBe('R3');
    // Chain visited R1+R2+R3
    expect(result.tier_log.map((e) => e.tier)).toEqual(['R1', 'R2', 'R3']);
  });
});

// ─── Loop control flow — request_changes ─────────────────────────────────

describe('runReviewGraphLoop — request_changes', () => {
  it('terminates at first request_changes (gatekeeper re-dispatches implementor)', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({
        decision: 'request_changes',
        confidence: 'high',
        findings: [{ severity: '🔴', file: 'x.ts', category: 'bug', summary: 'real bug' }],
      }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('request-changes');
    expect(result.final_tier).toBe('R1');
    expect(result.output?.findings).toHaveLength(1);
  });

  it('does NOT escalate to operator on request_changes + medium confidence + 🔴 (impl re-runs)', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({
        decision: 'request_changes',
        confidence: 'medium',
        findings: [{ severity: '🔴', file: 'x.ts', category: 'bug', summary: 'real bug' }],
      }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('request-changes');
  });
});

// ─── Loop control flow — parse failures ──────────────────────────────────

describe('runReviewGraphLoop — parse failures', () => {
  it('treats parse failure as escalate-tier signal', async () => {
    const dispatcher: ReviewerDispatch = async (req) => {
      // R1 returns gibberish (no marker); R2 returns clean approve
      if (req.workflow_id.endsWith('-revr1')) {
        return makeBlankResult('garbage; no marker here');
      }
      return makeBlankResult(`${REVIEW_MARKER_FOR_TEST}{"decision":"approve","confidence":"high","findings":[]}`);
    };
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('approve');
    expect(result.final_tier).toBe('R2');
    expect(result.tier_log[0].parsed).toBe(false);
    expect(result.tier_log[1].parsed).toBe(true);
  });

  it('returns parse-failure-at-r4 when every tier produces unparseable output', async () => {
    const dispatcher: ReviewerDispatch = async () => makeBlankResult('all garbage all the time');
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.action).toBe('parse-failure-at-r4');
    // final_tier reports the last DISPATCHED tier — R3 ran, then the
    // chain ran out of tiers. R4 is the action-category, not a
    // tier the chain visited.
    expect(result.final_tier).toBe('R3');
    expect(result.tier_log).toHaveLength(3);
    expect(result.tier_log.every((e) => !e.parsed)).toBe(true);
  });
});

// ─── t5_shape propagation ────────────────────────────────────────────────

describe('runReviewGraphLoop — t5_shape', () => {
  it('propagates t5_shape from computeStartingTier into the result', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({ decision: 'approve', confidence: 'high' }),
    });
    const result = await runReviewGraphLoop(
      makeInput({ pr_meta: makePrMeta({ files: ['chitin.yaml', 'apps/temporal-worker/src/foo.ts'] }) }),
      dispatcher,
    );
    expect(result.t5_shape).toBe(true);
    expect(result.action).toBe('approve');  // chain still runs; gatekeeper handles t5_shape escalation
  });

  it('reports t5_shape: false when no governance paths touched', async () => {
    const dispatcher = makeMockDispatcher({
      R1: buildMockReviewerOutput({ decision: 'approve', confidence: 'high' }),
    });
    const result = await runReviewGraphLoop(makeInput(), dispatcher);
    expect(result.t5_shape).toBe(false);
  });
});

// ─── buildReviewerRequest ────────────────────────────────────────────────

describe('buildReviewerRequest', () => {
  it('builds a valid ExecutionRequest for R1', () => {
    const req = buildReviewerRequest(makeInput(), 'R1', undefined);
    expect(req.role).toBe('reviewer');
    expect(req.allowed_drivers).toEqual(['copilot']);
    expect(req.write_policy).toBe('none');           // reviewer can't push
    expect(req.parent_workflow_id).toBe('swarm-parent-12345');
    expect(req.step_index).toBe(1);
    expect(req.workflow_id).toBe('swarm-parent-12345-revr1');
    expect(req.bounds.wall_timeout_s).toBe(600);     // R1 timeout
    expect(req.bounds.max_tool_calls).toBe(20);
    expect(req.bounds.max_cost_usd).toBe(0);         // copilot is free
  });

  it('uses claude-code-headless + cost cap at R3', () => {
    const req = buildReviewerRequest(makeInput(), 'R3', undefined);
    expect(req.allowed_drivers).toEqual(['claude-code-headless']);
    expect(req.bounds.wall_timeout_s).toBe(1800);
    expect(req.bounds.max_tool_calls).toBe(60);
    expect(req.bounds.max_cost_usd).toBeGreaterThan(0);   // R3 is paid
    expect(req.step_index).toBe(3);
  });

  it('includes prior findings in the prompt when provided', () => {
    const req = buildReviewerRequest(
      makeInput(),
      'R2',
      '- 🔴 dispatcher.ts:42 — ordering bug',
    );
    expect(req.prompt).toContain('ordering bug');
  });

  it('throws on R0 (non-dispatchable)', () => {
    expect(() => buildReviewerRequest(makeInput(), 'R0', undefined)).toThrow(/non-dispatchable/);
  });

  it('throws on R4 (non-dispatchable)', () => {
    expect(() => buildReviewerRequest(makeInput(), 'R4', undefined)).toThrow(/non-dispatchable/);
  });

  it('throws when pr_number is missing', () => {
    expect(() =>
      buildReviewerRequest(
        makeInput({ pr_meta: makePrMeta({ pr_number: undefined }) }),
        'R1',
        undefined,
      ),
    ).toThrow(/pr_number/);
  });

  it('throws when pr_url is missing', () => {
    expect(() =>
      buildReviewerRequest(
        makeInput({ pr_meta: makePrMeta({ pr_url: undefined }) }),
        'R1',
        undefined,
      ),
    ).toThrow(/pr_url/);
  });
});

// ─── formatFindingsForNextTier ───────────────────────────────────────────

describe('formatFindingsForNextTier', () => {
  it('formats findings with location + severity + summary', () => {
    const findings: ReviewerFinding[] = [
      { severity: '🔴', file: 'a.ts', line: 42, category: 'bug', summary: 'broken' },
      { severity: '🟡', file: 'b.ts', category: 'design', summary: 'rename', suggested_fix: 'use kebab' },
    ];
    const out = formatFindingsForNextTier(findings, undefined);
    expect(out).toContain('🔴 a.ts:42');
    expect(out).toContain('broken');
    expect(out).toContain('🟡 b.ts');
    expect(out).toContain('use kebab');
  });

  it('handles empty findings list (escalate without findings)', () => {
    const out = formatFindingsForNextTier([], undefined);
    expect(out).toContain('no findings');
  });
});

// ─── tierAsStepIndex ─────────────────────────────────────────────────────

describe('tierAsStepIndex', () => {
  it.each([
    ['R1', 1],
    ['R2', 2],
    ['R3', 3],
  ] as const)('%s → step_index %d', (tier, expected) => {
    expect(tierAsStepIndex(tier)).toBe(expected);
  });

  it.each(['R0', 'R4'] as const)('throws on %s (no step_index)', (tier) => {
    expect(() => tierAsStepIndex(tier)).toThrow(/no step_index/);
  });
});

// ─── helper: makeBlankResult ──────────────────────────────────────────────

function makeBlankResult(stdout_tail: string) {
  return {
    exit_code: 0 as const,
    stdout_tail,
    stderr_tail: '',
    duration_ms: 100,
  };
}
