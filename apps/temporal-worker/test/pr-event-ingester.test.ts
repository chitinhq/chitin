import { describe, expect, it } from 'vitest';
import {
  decideAgentDispatches,
  parentWorkflowIdForPr,
  pickPrsToIngest,
  reviewGraphWorkflowIdForPr,
  synthesizeBacklogEntry,
  type CommentResponderMarker,
  type OpenPrSummary,
  type PeerReviewerMarker,
} from '../src/pr-event-ingester.ts';

// peerReviewer + commentResponder workflow id helpers — same
// modules pr-event-ingester imports them from. Keeps test
// indirection one level shallow.
import { peerReviewerWorkflowIdForPr } from '../src/peer-reviewer/dispatch.ts';
import { commentResponderWorkflowIdForPr } from '../src/comment-responder/dispatch.ts';

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


describe('decideAgentDispatches — peer-reviewer + comment-responder gating', () => {
  // Per Copilot review on PR #264: matrix coverage for the new
  // refactored helper. Tests the three branches called out:
  // runningAgents dedup, threshold boundary, skip reasons.

  const basePr = (overrides: Partial<OpenPrSummary>): OpenPrSummary => pr({ number: 100, ...overrides });

  it('dispatches both when nothing is running and comments exceed threshold', () => {
    const d = decideAgentDispatches(
      basePr({ copilotCommentCount: 5 }),
      new Set<string>(),
      { commentResponderThreshold: 2 },
    );
    expect(d.dispatchPeerReviewer).toBe(true);
    expect(d.dispatchCommentResponder).toBe(true);
    expect(d.reasons).toEqual({});
  });

  it('skips peer-reviewer when its workflow is already running', () => {
    const running = new Set<string>([peerReviewerWorkflowIdForPr(100)]);
    const d = decideAgentDispatches(
      basePr({ copilotCommentCount: 5 }),
      running,
      { commentResponderThreshold: 2 },
    );
    expect(d.dispatchPeerReviewer).toBe(false);
    expect(d.reasons.skip_peer_reviewer).toContain('peer-reviewer already running');
    expect(d.dispatchCommentResponder).toBe(true);
  });

  it('skips comment-responder when its workflow is already running, even with comments above threshold', () => {
    const running = new Set<string>([commentResponderWorkflowIdForPr(100)]);
    const d = decideAgentDispatches(
      basePr({ copilotCommentCount: 5 }),
      running,
      { commentResponderThreshold: 2 },
    );
    expect(d.dispatchCommentResponder).toBe(false);
    expect(d.reasons.skip_comment_responder).toContain('comment-responder already running');
    // peer-reviewer still gets dispatched
    expect(d.dispatchPeerReviewer).toBe(true);
  });

  it('skips comment-responder at the threshold boundary (count <= threshold)', () => {
    // count == threshold → skip (the gate is `>` not `>=`)
    const d = decideAgentDispatches(
      basePr({ copilotCommentCount: 2 }),
      new Set<string>(),
      { commentResponderThreshold: 2 },
    );
    expect(d.dispatchCommentResponder).toBe(false);
    expect(d.reasons.skip_comment_responder).toMatch(/<= threshold/);
  });

  it('dispatches comment-responder one above the threshold boundary', () => {
    const d = decideAgentDispatches(
      basePr({ copilotCommentCount: 3 }),
      new Set<string>(),
      { commentResponderThreshold: 2 },
    );
    expect(d.dispatchCommentResponder).toBe(true);
    expect(d.reasons.skip_comment_responder).toBeUndefined();
  });

  it('uses default threshold when opts is omitted', () => {
    // The default threshold is whatever the module exports —
    // we assert behavior, not the number: a low comment count
    // (0) below any reasonable default must skip.
    const d = decideAgentDispatches(basePr({ copilotCommentCount: 0 }), new Set<string>());
    expect(d.dispatchCommentResponder).toBe(false);
  });

  it('dedups both agents simultaneously when both are already running', () => {
    const running = new Set<string>([
      peerReviewerWorkflowIdForPr(100),
      commentResponderWorkflowIdForPr(100),
    ]);
    const d = decideAgentDispatches(
      basePr({ copilotCommentCount: 10 }),
      running,
      { commentResponderThreshold: 2 },
    );
    expect(d.dispatchPeerReviewer).toBe(false);
    expect(d.dispatchCommentResponder).toBe(false);
    expect(d.reasons.skip_peer_reviewer).toBeDefined();
    expect(d.reasons.skip_comment_responder).toBeDefined();
  });
});


describe('decideAgentDispatches — marker-based dedup against completed runs', () => {
  // Closes the gap left by listRunningAgentWorkflows: once a peer-
  // reviewer run completes for (PR, head_sha), the marker keeps the
  // ingester from re-dispatching against that exact commit on the
  // next tick.
  const SHA_A = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
  const SHA_B = 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
  const peerMarker = (sha: string): PeerReviewerMarker => ({
    head_sha: sha,
    dispatched_at: '2026-05-04T12:00:00Z',
    workflow_id: peerReviewerWorkflowIdForPr(207),
  });
  const responderMarker = (sha: string, count: number): CommentResponderMarker => ({
    head_sha: sha,
    comment_count: count,
    dispatched_at: '2026-05-04T12:00:00Z',
    workflow_id: commentResponderWorkflowIdForPr(207),
  });
  const basePr = (overrides: Partial<OpenPrSummary>): OpenPrSummary =>
    pr({ number: 207, ...overrides });

  it('skips peer-reviewer when marker.head_sha matches PR head_sha (completed run dedup)', () => {
    const d = decideAgentDispatches(
      basePr({ headRefOid: SHA_A, copilotCommentCount: 0 }),
      new Set<string>(), // nothing currently running
      { peerReviewerMarker: peerMarker(SHA_A) },
    );
    expect(d.dispatchPeerReviewer).toBe(false);
    expect(d.reasons.skip_peer_reviewer).toMatch(/already ran for head_sha/);
  });

  it('dispatches peer-reviewer when PR head_sha advances past marker (new commit)', () => {
    const d = decideAgentDispatches(
      basePr({ headRefOid: SHA_B, copilotCommentCount: 0 }),
      new Set<string>(),
      { peerReviewerMarker: peerMarker(SHA_A) },
    );
    expect(d.dispatchPeerReviewer).toBe(true);
    expect(d.reasons.skip_peer_reviewer).toBeUndefined();
  });

  it('dispatches peer-reviewer when no marker has been written yet', () => {
    const d = decideAgentDispatches(
      basePr({ headRefOid: SHA_A, copilotCommentCount: 0 }),
      new Set<string>(),
      { peerReviewerMarker: undefined },
    );
    expect(d.dispatchPeerReviewer).toBe(true);
  });

  it('running-workflow check wins over marker — already-running message takes precedence', () => {
    const running = new Set<string>([peerReviewerWorkflowIdForPr(207)]);
    const d = decideAgentDispatches(
      basePr({ headRefOid: SHA_A }),
      running,
      { peerReviewerMarker: peerMarker(SHA_A) },
    );
    expect(d.dispatchPeerReviewer).toBe(false);
    expect(d.reasons.skip_peer_reviewer).toMatch(/already running/);
  });

  it('skips comment-responder when marker matches both head_sha and comment_count is unchanged', () => {
    const d = decideAgentDispatches(
      basePr({ headRefOid: SHA_A, copilotCommentCount: 5 }),
      new Set<string>(),
      {
        commentResponderThreshold: 2,
        commentResponderMarker: responderMarker(SHA_A, 5),
      },
    );
    expect(d.dispatchCommentResponder).toBe(false);
    expect(d.reasons.skip_comment_responder).toMatch(/already ran/);
  });

  it('dispatches comment-responder when PR head_sha advances past marker', () => {
    const d = decideAgentDispatches(
      basePr({ headRefOid: SHA_B, copilotCommentCount: 5 }),
      new Set<string>(),
      {
        commentResponderThreshold: 2,
        commentResponderMarker: responderMarker(SHA_A, 5),
      },
    );
    expect(d.dispatchCommentResponder).toBe(true);
  });

  it('dispatches comment-responder when comment_count grows beyond marker (new comments arrived)', () => {
    const d = decideAgentDispatches(
      basePr({ headRefOid: SHA_A, copilotCommentCount: 10 }),
      new Set<string>(),
      {
        commentResponderThreshold: 2,
        commentResponderMarker: responderMarker(SHA_A, 5),
      },
    );
    expect(d.dispatchCommentResponder).toBe(true);
  });

  it('marker dedup is bypassed when PR has no headRefOid (production safety: prefer dispatching over silent skip)', () => {
    const d = decideAgentDispatches(
      basePr({ headRefOid: undefined, copilotCommentCount: 0 }),
      new Set<string>(),
      { peerReviewerMarker: peerMarker(SHA_A) },
    );
    expect(d.dispatchPeerReviewer).toBe(true);
  });
});
