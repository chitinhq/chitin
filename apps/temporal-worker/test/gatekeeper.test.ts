import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  buildGatekeeperDigest,
  buildGatekeeperDigestWithMerge,
  evaluateGates,
  runGatekeeperNotify,
  __test__,
  type GateInputs,
  type GatekeeperInput,
  type GatekeeperOutcome,
} from '../src/gatekeeper.ts';
import type { ReviewGraphResult, PrMeta, ReviewerOutput, ReviewerFinding } from '../src/review-graph.ts';

const { ACTION_EMOJI, ACTION_HEADLINE } = __test__;

function makeOutput(overrides: Partial<ReviewerOutput> = {}): ReviewerOutput {
  return {
    decision: 'approve',
    confidence: 'high',
    findings: [],
    ...overrides,
  };
}

function makeResult(overrides: Partial<ReviewGraphResult> = {}): ReviewGraphResult {
  return {
    final_tier: 'R1',
    action: 'approve',
    output: makeOutput(),
    t5_shape: false,
    tier_log: [{ tier: 'R1', parsed: true, output: makeOutput() }],
    ...overrides,
  };
}

function makePrMeta(overrides: Partial<PrMeta> = {}): PrMeta {
  return {
    diff_loc: 47,
    files_changed: 3,
    files: [],
    pr_url: 'https://github.com/chitinhq/chitin/pull/200',
    pr_number: 200,
    ...overrides,
  };
}

function makeInput(overrides: Partial<GatekeeperInput> = {}): GatekeeperInput {
  return {
    result: makeResult(),
    pr_meta: makePrMeta(),
    repo: 'chitinhq/chitin',
    entry_id: 'sample-entry',
    ...overrides,
  };
}

// ─── buildGatekeeperDigest ──────────────────────────────────────────────

describe('buildGatekeeperDigest', () => {
  it('renders the approve emoji + headline + repo + entry + PR link', () => {
    const out = buildGatekeeperDigest(makeInput());
    expect(out).toContain(ACTION_EMOJI.approve);
    expect(out).toContain(ACTION_HEADLINE.approve);
    expect(out).toContain('chitinhq/chitin');
    expect(out).toContain('sample-entry');
    expect(out).toContain('#200');
    expect(out).toContain('https://github.com/chitinhq/chitin/pull/200');
  });

  it('renders different emoji for each action', () => {
    expect(buildGatekeeperDigest(makeInput({ result: makeResult({ action: 'approve' }) }))).toContain('✅');
    expect(buildGatekeeperDigest(makeInput({ result: makeResult({ action: 'request-changes' }) }))).toContain('🟡');
    expect(buildGatekeeperDigest(makeInput({ result: makeResult({ action: 'escalate-to-operator' }) }))).toContain('🚨');
    expect(buildGatekeeperDigest(makeInput({ result: makeResult({ action: 'parse-failure-at-r4' }) }))).toContain('⚠️');
  });

  it('flags t5_shape with the explicit override notice', () => {
    const out = buildGatekeeperDigest(makeInput({ result: makeResult({ t5_shape: true }) }));
    expect(out).toContain('t5_shape detected');
  });

  it('reports diff_loc and files_changed in the header', () => {
    const out = buildGatekeeperDigest(
      makeInput({ pr_meta: makePrMeta({ diff_loc: 829, files_changed: 4 }) }),
    );
    expect(out).toContain('diff=829 LOC');
    expect(out).toContain('4 file(s)');
  });

  it('renders findings as bullets with severity + location + summary', () => {
    const findings: ReviewerFinding[] = [
      { severity: '🔴', file: 'a.ts', line: 42, category: 'bug', summary: 'broken logic' },
      { severity: '🟡', file: 'b.ts', category: 'design', summary: 'rename' },
    ];
    const out = buildGatekeeperDigest(
      makeInput({
        result: makeResult({
          action: 'request-changes',
          output: makeOutput({ decision: 'request_changes', findings }),
        }),
      }),
    );
    expect(out).toContain('🔴 a.ts:42');
    expect(out).toContain('broken logic');
    expect(out).toContain('🟡 b.ts');
  });

  it('says "no findings" on a clean approve', () => {
    const out = buildGatekeeperDigest(makeInput()); // approve + empty
    expect(out).toContain('no findings');
  });

  it('caps findings at 8 + adds "more" line for excess', () => {
    const findings: ReviewerFinding[] = Array.from({ length: 12 }, (_, i) => ({
      severity: '🟡' as const,
      file: `f${i}.ts`,
      category: 'design' as const,
      summary: `nit ${i}`,
    }));
    const out = buildGatekeeperDigest(
      makeInput({
        result: makeResult({
          action: 'request-changes',
          output: makeOutput({ decision: 'request_changes', findings }),
        }),
      }),
    );
    expect(out).toContain('f0.ts');
    expect(out).toContain('f7.ts');
    expect(out).not.toContain('f9.ts');
    expect(out).toContain('+4 more');
  });

  it('includes a tier log section listing each visited tier', () => {
    const tier_log: ReviewGraphResult['tier_log'] = [
      { tier: 'R1', parsed: true, output: makeOutput({ decision: 'escalate' }) },
      { tier: 'R2', parsed: true, output: makeOutput({ decision: 'escalate' }) },
      { tier: 'R3', parsed: false, error: 'no <<<REVIEW>>> marker' },
    ];
    const out = buildGatekeeperDigest(
      makeInput({
        result: makeResult({ final_tier: 'R3', action: 'parse-failure-at-r4', tier_log }),
      }),
    );
    expect(out).toContain('R1: parsed');
    expect(out).toContain('R2: parsed');
    expect(out).toContain('R3: parse-fail');
  });

  it('handles the missing-PR-url edge case (rare; fallback message)', () => {
    const out = buildGatekeeperDigest(
      makeInput({ pr_meta: { ...makePrMeta(), pr_url: undefined, pr_number: undefined } }),
    );
    expect(out).toContain('(no PR url)');
  });
});

// ─── runGatekeeperNotify ────────────────────────────────────────────────

describe('runGatekeeperNotify', () => {
  let realFetch: typeof globalThis.fetch;
  let realWebhook: string | undefined;

  beforeEach(() => {
    realFetch = globalThis.fetch;
    realWebhook = process.env.CHITIN_SLACK_WEBHOOK_URL;
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
    if (realWebhook === undefined) delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    else process.env.CHITIN_SLACK_WEBHOOK_URL = realWebhook;
  });

  it('returns notified:false when the webhook is unset (no fetch call)', async () => {
    delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    let fetchCalls = 0;
    globalThis.fetch = (async () => {
      fetchCalls++;
      return new Response('ok', { status: 200 });
    }) as typeof globalThis.fetch;
    const r = await runGatekeeperNotify(makeInput());
    expect(r.notified).toBe(false);
    expect(r.reason).toContain('unset');
    expect(r.digest).toContain('sample-entry');
    expect(fetchCalls).toBe(0);
  });

  it('posts to slack and returns notified:true on 200', async () => {
    process.env.CHITIN_SLACK_WEBHOOK_URL = 'https://hooks.slack.com/services/test';
    const captured: { url: string; body: string }[] = [];
    globalThis.fetch = (async (url: string | URL | Request, init?: RequestInit) => {
      captured.push({ url: String(url), body: String(init?.body ?? '') });
      return new Response('ok', { status: 200 });
    }) as typeof globalThis.fetch;
    const r = await runGatekeeperNotify(makeInput());
    expect(r.notified).toBe(true);
    expect(r.reason).toBe('posted');
    expect(captured).toHaveLength(1);
    expect(captured[0].url).toContain('hooks.slack.com');
    expect(captured[0].body).toContain('sample-entry');
  });

  it('returns notified:false on non-2xx without throwing', async () => {
    process.env.CHITIN_SLACK_WEBHOOK_URL = 'https://hooks.slack.com/services/test';
    globalThis.fetch = (async () => new Response('rate limited', { status: 429 })) as typeof globalThis.fetch;
    const r = await runGatekeeperNotify(makeInput());
    expect(r.notified).toBe(false);
    expect(r.reason).toContain('429');
  });

  it('returns notified:false when fetch rejects (network failure)', async () => {
    process.env.CHITIN_SLACK_WEBHOOK_URL = 'https://hooks.slack.com/services/test';
    globalThis.fetch = (async () => {
      throw new Error('connection refused');
    }) as typeof globalThis.fetch;
    const r = await runGatekeeperNotify(makeInput());
    expect(r.notified).toBe(false);
    expect(r.reason).toContain('connection refused');
  });

  it('returns the digest on every code path (so journal gets it even when slack misses)', async () => {
    delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    const r = await runGatekeeperNotify(makeInput());
    expect(r.digest.length).toBeGreaterThan(0);
    expect(r.digest).toContain('sample-entry');
  });
});

// ─── §6 auto-merge gates ─────────────────────────────────────────────────

function makeGateInputs(overrides: Partial<GateInputs> = {}): GateInputs {
  return {
    result: makeResult({ action: 'approve' }),
    pr_files: ['apps/temporal-worker/src/foo.ts'],
    entry_file_scope: 'apps/temporal-worker/src/foo.ts',
    ci_green: true,
    ...overrides,
  };
}

describe('evaluateGates', () => {
  it('passes when every signal is green (approve + CI green + scope matches + no t5)', () => {
    const r = evaluateGates(makeGateInputs());
    expect(r.passed).toBe(true);
    expect(r.failures).toEqual([]);
  });

  it('fails when action is not approve', () => {
    const r = evaluateGates(makeGateInputs({ result: makeResult({ action: 'request-changes' }) }));
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain("action=request-changes");
  });

  it('fails when t5_shape is true (even if action=approve)', () => {
    const r = evaluateGates(
      makeGateInputs({ result: makeResult({ action: 'approve', t5_shape: true }) }),
    );
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain('t5_shape');
  });

  it('fails when CI is not green', () => {
    const r = evaluateGates(makeGateInputs({ ci_green: false }));
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain('CI not green');
  });

  it('fails when reviewer flagged a 🔴 finding', () => {
    const result = makeResult({
      action: 'approve',
      output: makeOutput({
        decision: 'approve',
        findings: [{ severity: '🔴', file: 'a.ts', category: 'bug', summary: 'real bug' }],
      }),
    });
    const r = evaluateGates(makeGateInputs({ result }));
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain('🔴');
  });

  it('fails when diff touches a T5-shape path (chitin.yaml)', () => {
    const r = evaluateGates(
      makeGateInputs({
        pr_files: ['chitin.yaml', 'apps/temporal-worker/src/foo.ts'],
      }),
    );
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain('chitin.yaml');
  });

  it('fails when diff touches go/execution-kernel/internal/gov/', () => {
    const r = evaluateGates(
      makeGateInputs({
        pr_files: ['go/execution-kernel/internal/gov/normalize.go'],
        entry_file_scope: 'go/execution-kernel/internal/gov/normalize.go',
      }),
    );
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain('T5-shape');
  });

  it('fails when diff does not intersect entry file scope (bucket-B detection)', () => {
    const r = evaluateGates(
      makeGateInputs({
        pr_files: ['unrelated/path.ts'],
        entry_file_scope: 'apps/temporal-worker/src/foo.ts',
      }),
    );
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain("doesn't intersect");
  });

  it('passes when diff intersects ANY of the comma-separated scope files', () => {
    const r = evaluateGates(
      makeGateInputs({
        pr_files: ['libs/contracts/src/bar.ts'],
        entry_file_scope: 'apps/temporal-worker/src/foo.ts, libs/contracts/src/bar.ts',
      }),
    );
    expect(r.passed).toBe(true);
  });

  it('passes when diff is in a subdirectory of the scope file (directory-style)', () => {
    const r = evaluateGates(
      makeGateInputs({
        pr_files: ['apps/temporal-worker/src/lib/util.ts'],
        entry_file_scope: 'apps/temporal-worker/src/',
      }),
    );
    expect(r.passed).toBe(true);
  });

  it('skips the scope-intersection gate when entry has no declared file scope', () => {
    const r = evaluateGates(
      makeGateInputs({
        entry_file_scope: undefined,
        pr_files: ['random/path.ts'],
      }),
    );
    expect(r.passed).toBe(true);
  });

  it('accumulates multiple gate failures (does not short-circuit)', () => {
    const r = evaluateGates(
      makeGateInputs({
        result: makeResult({ action: 'request-changes', t5_shape: true }),
        ci_green: false,
      }),
    );
    expect(r.passed).toBe(false);
    expect(r.failures.length).toBeGreaterThanOrEqual(3);
  });

  it("fails when bucket-B rate > 0% (last 24h)", () => {
    const r = evaluateGates(makeGateInputs({ bucket_b_rate: 0.05 }));
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain('bucket-B rate');
  });

  it('passes when bucket-B rate is 0', () => {
    const r = evaluateGates(makeGateInputs({ bucket_b_rate: 0 }));
    expect(r.passed).toBe(true);
  });

  it('skips the bucket-B gate when rate is undefined (rollup absent / cold start)', () => {
    const r = evaluateGates(makeGateInputs({ bucket_b_rate: undefined }));
    expect(r.passed).toBe(true);
  });

  it("fails when implementor success rate < 70%", () => {
    const r = evaluateGates(
      makeGateInputs({ implementor_success_rate: 0.5, implementor_path: 'tier=T2' }),
    );
    expect(r.passed).toBe(false);
    expect(r.failures.join(' ')).toContain('success rate');
    expect(r.failures.join(' ')).toContain('tier=T2');
  });

  it('passes when implementor success rate >= 70%', () => {
    const r = evaluateGates(
      makeGateInputs({ implementor_success_rate: 0.85, implementor_path: 'tier=T1' }),
    );
    expect(r.passed).toBe(true);
  });

  it('skips the success-rate gate when undefined (insufficient telemetry)', () => {
    const r = evaluateGates(
      makeGateInputs({ implementor_success_rate: undefined, implementor_path: 'tier=T2' }),
    );
    expect(r.passed).toBe(true);
  });
});

// ─── Rollup readers ──────────────────────────────────────────────────────

import {
  bucketBRateFromRollup,
  implementorSuccessRateFromRollup,
  readLatestRollup,
} from '../src/gatekeeper.ts';

describe('readLatestRollup', () => {
  let scratch: string;

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'gatekeeper-rollup-test-'));
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('returns undefined when the rollup dir is missing', () => {
    expect(readLatestRollup(join(scratch, 'nope'))).toBeUndefined();
  });

  it('returns undefined when the rollup dir is empty', () => {
    expect(readLatestRollup(scratch)).toBeUndefined();
  });

  it('reads the latest rollup by lexicographic name (YYYY-MM-DD.json)', () => {
    writeFileSync(join(scratch, '2026-04-30.json'), JSON.stringify({ bucket_b_rate: 0.1 }), 'utf8');
    writeFileSync(join(scratch, '2026-05-02.json'), JSON.stringify({ bucket_b_rate: 0 }), 'utf8');
    const r = readLatestRollup(scratch);
    expect(r?.bucket_b_rate).toBe(0); // newest wins
  });

  it('returns undefined on a malformed JSON file (and does not throw)', () => {
    writeFileSync(join(scratch, '2026-05-02.json'), '{ not valid json', 'utf8');
    expect(readLatestRollup(scratch)).toBeUndefined();
  });
});

describe('bucketBRateFromRollup', () => {
  it('returns the rate from the rollup', () => {
    expect(bucketBRateFromRollup({ bucket_b_rate: 0.21 })).toBe(0.21);
    expect(bucketBRateFromRollup({ bucket_b_rate: 0 })).toBe(0);
  });

  it('returns undefined when rollup is undefined', () => {
    expect(bucketBRateFromRollup(undefined)).toBeUndefined();
  });

  it('returns undefined when bucket_b_rate field is missing', () => {
    expect(bucketBRateFromRollup({})).toBeUndefined();
  });
});

describe('implementorSuccessRateFromRollup', () => {
  it('returns rate when tier has enough data', () => {
    const rollup = { success_by_tier: { T1: { success: 8, total: 10 } } };
    const r = implementorSuccessRateFromRollup(rollup, 'T1');
    expect(r.rate).toBe(0.8);
    expect(r.path).toBe('tier=T1');
  });

  it('returns undefined when tier has < 5 runs (cold-start protection)', () => {
    const rollup = { success_by_tier: { T1: { success: 1, total: 2 } } };
    const r = implementorSuccessRateFromRollup(rollup, 'T1');
    expect(r.rate).toBeUndefined();
  });

  it('returns undefined when rollup is undefined', () => {
    const r = implementorSuccessRateFromRollup(undefined, 'T1');
    expect(r.rate).toBeUndefined();
  });

  it('returns undefined when tier is undefined', () => {
    const rollup = { success_by_tier: { T1: { success: 8, total: 10 } } };
    const r = implementorSuccessRateFromRollup(rollup, undefined);
    expect(r.rate).toBeUndefined();
  });
});

// ─── runGatekeeperNotify with auto-merge flag ────────────────────────────

describe('runGatekeeperNotify auto-merge gating', () => {
  let realFetch: typeof globalThis.fetch;
  let realWebhook: string | undefined;
  let realFlag: string | undefined;

  beforeEach(() => {
    realFetch = globalThis.fetch;
    realWebhook = process.env.CHITIN_SLACK_WEBHOOK_URL;
    realFlag = process.env.CHITIN_GATEKEEPER_AUTO_MERGE;
    delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    delete process.env.CHITIN_GATEKEEPER_AUTO_MERGE;
  });

  afterEach(() => {
    globalThis.fetch = realFetch;
    if (realWebhook === undefined) delete process.env.CHITIN_SLACK_WEBHOOK_URL;
    else process.env.CHITIN_SLACK_WEBHOOK_URL = realWebhook;
    if (realFlag === undefined) delete process.env.CHITIN_GATEKEEPER_AUTO_MERGE;
    else process.env.CHITIN_GATEKEEPER_AUTO_MERGE = realFlag;
  });

  it("notifies-only by default (CHITIN_GATEKEEPER_AUTO_MERGE off)", async () => {
    const r = await runGatekeeperNotify(makeInput());
    expect(r.merge.attempted).toBe(false);
    expect(r.merge.merged).toBe(false);
    expect(r.merge.reason).toContain('off');
  });

  it("does not attempt auto-merge when action is not 'approve' (even with flag on)", async () => {
    process.env.CHITIN_GATEKEEPER_AUTO_MERGE = '1';
    const r = await runGatekeeperNotify(
      makeInput({ result: makeResult({ action: 'request-changes' }) }),
    );
    expect(r.merge.attempted).toBe(false);
  });

  it("does not attempt auto-merge when pr_number is missing", async () => {
    process.env.CHITIN_GATEKEEPER_AUTO_MERGE = '1';
    const r = await runGatekeeperNotify(
      makeInput({
        pr_meta: { ...makePrMeta(), pr_number: undefined },
        result: makeResult({ action: 'approve' }),
      }),
    );
    expect(r.merge.attempted).toBe(false);
    expect(r.merge.reason).toContain('pr_number missing');
  });
});

// ─── buildGatekeeperDigestWithMerge ──────────────────────────────────────

describe('buildGatekeeperDigestWithMerge', () => {
  function makeMerge(overrides: Partial<GatekeeperOutcome['merge']> = {}): GatekeeperOutcome['merge'] {
    return {
      attempted: false,
      merged: false,
      reason: '',
      gate_failures: [],
      ...overrides,
    };
  }

  it("renders a success line when merge succeeded", () => {
    const out = buildGatekeeperDigestWithMerge(
      makeInput(),
      makeMerge({ attempted: true, merged: true, reason: 'gates passed; gh pr merge succeeded (#142)' }),
    );
    expect(out).toContain('🤖 Auto-merged');
    expect(out).toContain('#142');
  });

  it("renders a failure line when merge attempted but failed", () => {
    const out = buildGatekeeperDigestWithMerge(
      makeInput(),
      makeMerge({ attempted: true, merged: false, reason: 'gh exit 1: ref protected' }),
    );
    expect(out).toContain('Auto-merge attempted but failed');
  });

  it("renders gate-failure breakdown when gates blocked", () => {
    const out = buildGatekeeperDigestWithMerge(
      makeInput(),
      makeMerge({
        gate_failures: ['CI not green', 'diff touches T5-shape path(s): chitin.yaml'],
      }),
    );
    expect(out).toContain('Auto-merge gates failed');
    expect(out).toContain('CI not green');
    expect(out).toContain('chitin.yaml');
  });

  it("adds NO merge section when no merge was attempted + no gate failures (auto-merge off)", () => {
    const base = buildGatekeeperDigest(makeInput());
    const out = buildGatekeeperDigestWithMerge(makeInput(), makeMerge());
    expect(out).toBe(base);
  });
});
