import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  buildGatekeeperDigest,
  runGatekeeperNotify,
  __test__,
  type GatekeeperInput,
} from '../src/gatekeeper.ts';
import type { ReviewGraphResult } from '../src/review-graph-workflow.ts';
import type { PrMeta, ReviewerOutput, ReviewerFinding } from '../src/review-graph.ts';

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
