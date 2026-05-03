import { describe, expect, it } from 'vitest';
import {
  parentWorkflowIdForPr,
  pickPrsToIngest,
  reviewGraphWorkflowIdForPr,
  synthesizeBacklogEntry,
  type OpenPrSummary,
} from '../src/pr-event-ingester.ts';

function pr(overrides: Partial<OpenPrSummary> & { number: number }): OpenPrSummary {
  return {
    title: 'test',
    headRefName: 'feature/test',
    additions: 10,
    deletions: 0,
    changedFiles: 1,
    isDraft: false,
    files: [],
    copilotCommentCount: 0,
    url: `https://github.com/chitinhq/chitin/pull/${overrides.number}`,
    ...overrides,
  };
}

describe('parentWorkflowIdForPr / reviewGraphWorkflowIdForPr', () => {
  it('parent id is stable per PR number', () => {
    expect(parentWorkflowIdForPr(199)).toBe('pr-ingest-199');
  });

  it('review-graph id is parent + suffix (matches review-graph-dispatch convention)', () => {
    expect(reviewGraphWorkflowIdForPr(199)).toBe('pr-ingest-199-review-graph');
  });
});

describe('synthesizeBacklogEntry', () => {
  it('packs PR data into a BacklogEntry-shaped object', () => {
    const entry = synthesizeBacklogEntry(pr({
      number: 199,
      title: 'Slice 1 substrate',
      additions: 800,
      deletions: 100,
      files: ['libs/governance/src/decide.ts', 'libs/governance/tests/decide.test.ts'],
    }));
    expect(entry.id).toBe('pr-ingest-199');
    expect(entry.role).toBe('reviewer');
    expect(entry.tier).toBe('T2');
    expect(entry.estimatedLoc).toBe('900');
    expect(entry.file).toContain('decide.ts');
    expect(entry.description).toContain('PR #199');
  });

  it('handles PRs with no fetched file list', () => {
    const entry = synthesizeBacklogEntry(pr({ number: 1, files: undefined }));
    expect(entry.file).toBe('');
  });
});

describe('pickPrsToIngest — invariants', () => {
  it('every input PR appears exactly once in the output', () => {
    const inputs = [pr({ number: 1 }), pr({ number: 2 }), pr({ number: 3 })];
    const decisions = pickPrsToIngest(inputs, new Set());
    expect(decisions).toHaveLength(inputs.length);
    const numbers = decisions.map((d) => d.pr.number).sort();
    expect(numbers).toEqual([1, 2, 3]);
  });
});

describe('pickPrsToIngest — skip rules', () => {
  it('skips draft PRs', () => {
    const decisions = pickPrsToIngest([pr({ number: 10, isDraft: true })], new Set());
    expect(decisions[0]).toMatchObject({ kind: 'skip', reason: 'draft PR' });
  });

  it('skips dispatcher-owned (swarm/) branches', () => {
    const decisions = pickPrsToIngest(
      [pr({ number: 11, headRefName: 'swarm/swarm-foo-1234' })],
      new Set(),
    );
    expect(decisions[0]).toMatchObject({
      kind: 'skip',
      reason: 'dispatcher-owned (swarm/ branch)',
    });
  });

  it('skips PRs with a review-graph workflow already running', () => {
    const running = new Set([reviewGraphWorkflowIdForPr(12)]);
    const decisions = pickPrsToIngest([pr({ number: 12, additions: 1000 })], running);
    expect(decisions[0]).toMatchObject({
      kind: 'skip',
      reason: 'review-graph already running',
    });
  });
});

describe('pickPrsToIngest — tier classification', () => {
  it('small clean PR with no Copilot comments → ingest_r0 (Copilot covers it)', () => {
    const decisions = pickPrsToIngest(
      [pr({ number: 20, additions: 5, deletions: 0, changedFiles: 1, copilotCommentCount: 0 })],
      new Set(),
    );
    expect(decisions[0].kind).toBe('ingest_r0');
  });

  it('PR with > 2 Copilot comments escalates to R1', () => {
    const decisions = pickPrsToIngest(
      [pr({ number: 21, additions: 50, deletions: 0, changedFiles: 1, copilotCommentCount: 5 })],
      new Set(),
    );
    expect(decisions[0].kind).toBe('ingest');
    if (decisions[0].kind === 'ingest') {
      expect(decisions[0].starting_tier).toBe('R1');
      expect(decisions[0].reasons.some((r) => r.includes('Copilot bot'))).toBe(true);
    }
  });

  it('large diff (> 200 LOC) escalates to R2', () => {
    const decisions = pickPrsToIngest(
      [pr({ number: 22, additions: 250, deletions: 0, changedFiles: 5 })],
      new Set(),
    );
    expect(decisions[0].kind).toBe('ingest');
    if (decisions[0].kind === 'ingest') {
      expect(decisions[0].starting_tier).toBe('R2');
    }
  });

  it('very large diff (> 500 LOC) escalates to R3', () => {
    const decisions = pickPrsToIngest(
      [pr({ number: 23, additions: 800, deletions: 100, changedFiles: 12 })],
      new Set(),
    );
    expect(decisions[0].kind).toBe('ingest');
    if (decisions[0].kind === 'ingest') {
      expect(decisions[0].starting_tier).toBe('R3');
    }
  });

  it('wide diff (> 20 files) escalates to R3 even with small LOC', () => {
    const decisions = pickPrsToIngest(
      [pr({ number: 24, additions: 50, deletions: 0, changedFiles: 25 })],
      new Set(),
    );
    expect(decisions[0].kind).toBe('ingest');
    if (decisions[0].kind === 'ingest') {
      expect(decisions[0].starting_tier).toBe('R3');
    }
  });
});

describe('pickPrsToIngest — mixed batch', () => {
  it('partitions a heterogeneous batch into the right buckets', () => {
    const inputs = [
      pr({ number: 1, isDraft: true }),                                // skip
      pr({ number: 2, headRefName: 'swarm/x' }),                       // skip
      pr({ number: 3, additions: 5, copilotCommentCount: 0 }),         // ingest_r0
      pr({ number: 4, additions: 50, copilotCommentCount: 5 }),        // ingest R1
      pr({ number: 5, additions: 800, deletions: 50, changedFiles: 10 }), // ingest R3
    ];
    const decisions = pickPrsToIngest(inputs, new Set());
    expect(decisions.map((d) => d.kind)).toEqual([
      'skip',
      'skip',
      'ingest_r0',
      'ingest',
      'ingest',
    ]);
  });
});
