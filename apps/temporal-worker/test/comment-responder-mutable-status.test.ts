import { describe, expect, it } from 'vitest';
import {
  STATUS_BODY_MARKER,
  VerdictMetaSchema,
  findByMarker,
  parseVerdict,
  read,
  renderStatusBody,
  upsert,
  type GhRunner,
  type VerdictMeta,
} from '../src/comment-responder/mutable-status.ts';

// ─── gh runner harness ───────────────────────────────────────────────────

interface FakeComment {
  id: number;
  body: string;
}

interface FakeGhCall {
  args: string[];
}

interface FakeGhState {
  comments: FakeComment[];
  calls: FakeGhCall[];
  /** Auto-incrementing id for newly-created comments. */
  nextId: number;
  /** PR number expected on `gh pr comment <n>`. */
  pr_number: number;
  /** Repo expected on `--repo <repo>` and on api paths. */
  repo: string;
}

function makeGh(state: FakeGhState): GhRunner {
  return (args: string[]): string => {
    state.calls.push({ args });
    // gh api --paginate repos/<repo>/issues/<pr>/comments
    if (
      args[0] === 'api' &&
      args[1] === '--paginate' &&
      /^repos\/.+\/issues\/\d+\/comments$/.test(args[2] ?? '')
    ) {
      return JSON.stringify(state.comments);
    }
    // gh pr comment <pr> --repo <repo> --body <body>
    if (args[0] === 'pr' && args[1] === 'comment') {
      const bodyIdx = args.indexOf('--body');
      const body = bodyIdx >= 0 ? args[bodyIdx + 1] : '';
      const id = state.nextId++;
      state.comments.push({ id, body });
      return `https://github.com/${state.repo}/pull/${state.pr_number}#issuecomment-${id}\n`;
    }
    // gh api -X PATCH repos/<repo>/issues/comments/<id> -f body=...
    if (args[0] === 'api' && args[1] === '-X' && args[2] === 'PATCH') {
      const path = args[3] ?? '';
      const m = path.match(/issues\/comments\/(\d+)$/);
      if (!m) throw new Error(`fake-gh: malformed PATCH path: ${path}`);
      const id = Number(m[1]);
      const fIdx = args.indexOf('-f');
      const fArg = fIdx >= 0 ? args[fIdx + 1] ?? '' : '';
      const body = fArg.startsWith('body=') ? fArg.slice('body='.length) : '';
      const comment = state.comments.find((c) => c.id === id);
      if (!comment) throw new Error(`fake-gh: comment id=${id} not found`);
      comment.body = body;
      return '';
    }
    throw new Error(`fake-gh: unhandled args: ${args.join(' ')}`);
  };
}

function blankState(pr_number = 207, repo = 'chitinhq/chitin'): FakeGhState {
  return { comments: [], calls: [], nextId: 1001, pr_number, repo };
}

// ─── renderStatusBody ────────────────────────────────────────────────────

describe('renderStatusBody', () => {
  it('puts the body marker on the first line and verdict marker at the end', () => {
    const verdict: VerdictMeta = {
      tier: 'R1',
      status: 'changes_requested',
      workflow_id: 'review-graph-pr-207',
      ts: '2026-05-05T12:00:00Z',
    };
    const out = renderStatusBody('Some prose body.', verdict);
    expect(out.split('\n')[0]).toBe(STATUS_BODY_MARKER);
    expect(out).toContain('Some prose body.');
    expect(out).toMatch(/<!-- chitin:verdict tier=R1 status=changes_requested workflow_id=review-graph-pr-207 ts=2026-05-05T12:00:00Z -->/);
  });

  it('trims trailing whitespace on the caller body so blank lines do not accumulate', () => {
    const verdict: VerdictMeta = {
      tier: 'R0',
      status: 'pending',
      workflow_id: 'wf',
      ts: '2026-05-05T12:00:00Z',
    };
    const out = renderStatusBody('body with trailing newlines\n\n\n', verdict);
    // Between body and verdict marker we expect exactly one blank line.
    expect(out).not.toMatch(/\n\n\n\n<!-- chitin:verdict/);
  });
});

// ─── parseVerdict ────────────────────────────────────────────────────────

describe('parseVerdict', () => {
  it('returns null when no marker present', () => {
    expect(parseVerdict('plain prose, no marker', () => {})).toBeNull();
  });

  it('parses a single well-formed marker', () => {
    const body = 'foo\n<!-- chitin:verdict tier=R2 status=approve workflow_id=abc ts=2026-05-05T12:00:00Z -->\nbar';
    const v = parseVerdict(body, () => {});
    expect(v).toEqual({
      tier: 'R2',
      status: 'approve',
      workflow_id: 'abc',
      ts: '2026-05-05T12:00:00Z',
    });
  });

  it('returns the LATEST marker by ts when multiple are present', () => {
    const body = [
      '<!-- chitin:verdict tier=R0 status=pending workflow_id=w0 ts=2026-05-05T10:00:00Z -->',
      '<!-- chitin:verdict tier=R1 status=changes_requested workflow_id=w1 ts=2026-05-05T11:00:00Z -->',
      '<!-- chitin:verdict tier=R2 status=approve workflow_id=w2 ts=2026-05-05T12:00:00Z -->',
    ].join('\n');
    const v = parseVerdict(body, () => {});
    expect(v?.tier).toBe('R2');
    expect(v?.status).toBe('approve');
  });

  it('breaks ts ties by tier (higher tier wins)', () => {
    const body = [
      '<!-- chitin:verdict tier=R1 status=changes_requested workflow_id=w1 ts=2026-05-05T12:00:00Z -->',
      '<!-- chitin:verdict tier=R3 status=approve workflow_id=w3 ts=2026-05-05T12:00:00Z -->',
    ].join('\n');
    const v = parseVerdict(body, () => {});
    expect(v?.tier).toBe('R3');
  });

  it('skips malformed markers and logs', () => {
    const logs: string[] = [];
    const body = [
      '<!-- chitin:verdict tier=BOGUS status=approve workflow_id=w ts=2026-05-05T12:00:00Z -->',
      '<!-- chitin:verdict tier=R2 status=approve workflow_id=w ts=2026-05-05T13:00:00Z -->',
    ].join('\n');
    const v = parseVerdict(body, (l) => logs.push(l));
    expect(v?.tier).toBe('R2');
    expect(logs.length).toBe(1);
    expect(logs[0]).toMatch(/malformed verdict marker/);
  });

  it('returns null and logs when EVERY marker is malformed (fail closed)', () => {
    const logs: string[] = [];
    const body = '<!-- chitin:verdict tier=R2 status=YOLO workflow_id=w ts=2026-05-05T12:00:00Z -->';
    const v = parseVerdict(body, (l) => logs.push(l));
    expect(v).toBeNull();
    expect(logs.length).toBe(1);
  });
});

// ─── findByMarker ────────────────────────────────────────────────────────

describe('findByMarker', () => {
  it('returns null when no chitin status comment is on the PR', () => {
    const state = blankState();
    state.comments.push({ id: 1, body: 'an unrelated human comment' });
    state.comments.push({ id: 2, body: 'another bot comment without the marker' });
    const result = findByMarker(207, 'chitinhq/chitin', STATUS_BODY_MARKER, makeGh(state));
    expect(result.comment_id).toBeNull();
    expect(result.body).toBe('');
  });

  it('returns the comment whose body starts with the marker', () => {
    const state = blankState();
    state.comments.push({ id: 1, body: 'unrelated' });
    state.comments.push({
      id: 42,
      body: `${STATUS_BODY_MARKER}\n\nbody text\n\n<!-- chitin:verdict tier=R0 status=pending workflow_id=w ts=2026-05-05T10:00:00Z -->`,
    });
    state.comments.push({ id: 99, body: 'noise after the chitin one' });
    const result = findByMarker(207, 'chitinhq/chitin', STATUS_BODY_MARKER, makeGh(state));
    expect(result.comment_id).toBe(42);
    expect(result.body.startsWith(STATUS_BODY_MARKER)).toBe(true);
  });

  it('uses --paginate on the gh api call', () => {
    const state = blankState();
    findByMarker(207, 'chitinhq/chitin', STATUS_BODY_MARKER, makeGh(state));
    expect(state.calls[0]?.args).toContain('--paginate');
  });
});

// ─── read ────────────────────────────────────────────────────────────────

describe('read', () => {
  it('returns the comment + parsed verdict when present', () => {
    const state = blankState();
    state.comments.push({
      id: 42,
      body: `${STATUS_BODY_MARKER}\n\nbody\n\n<!-- chitin:verdict tier=R2 status=approve workflow_id=w ts=2026-05-05T13:00:00Z -->`,
    });
    const r = read(207, 'chitinhq/chitin', () => {}, makeGh(state));
    expect(r.comment_id).toBe(42);
    expect(r.verdict?.tier).toBe('R2');
    expect(r.verdict?.status).toBe('approve');
  });

  it('returns null verdict when the comment exists but the marker is malformed', () => {
    const state = blankState();
    state.comments.push({
      id: 42,
      body: `${STATUS_BODY_MARKER}\n\nbody\n\n<!-- chitin:verdict tier=R2 status=YOLO workflow_id=w ts=2026-05-05T13:00:00Z -->`,
    });
    const r = read(207, 'chitinhq/chitin', () => {}, makeGh(state));
    expect(r.comment_id).toBe(42);
    expect(r.verdict).toBeNull();
  });

  it('returns the empty shape when no chitin comment exists', () => {
    const state = blankState();
    const r = read(207, 'chitinhq/chitin', () => {}, makeGh(state));
    expect(r).toEqual({ comment_id: null, body: '', verdict: null });
  });
});

// ─── upsert ──────────────────────────────────────────────────────────────

describe('upsert', () => {
  const verdictR1: VerdictMeta = {
    tier: 'R1',
    status: 'changes_requested',
    workflow_id: 'wf-1',
    ts: '2026-05-05T12:00:00Z',
  };
  const verdictR2: VerdictMeta = {
    tier: 'R2',
    status: 'approve',
    workflow_id: 'wf-2',
    ts: '2026-05-05T13:00:00Z',
  };

  it('first-tier upsert creates a new comment via gh pr comment', () => {
    const state = blankState();
    const r = upsert({
      pr_number: 207,
      repo: 'chitinhq/chitin',
      body: 'R1 says: please address X',
      verdict: verdictR1,
      log: () => {},
      gh: makeGh(state),
    });
    expect(r.action).toBe('created');
    expect(r.comment_id).toBe(1001);
    expect(state.comments.length).toBe(1);
    expect(state.comments[0].body.startsWith(STATUS_BODY_MARKER)).toBe(true);
    // verdict marker present
    expect(state.comments[0].body).toMatch(/tier=R1.*status=changes_requested/);
    // a `gh pr comment` call (not a PATCH) was made
    const created = state.calls.find((c) => c.args[0] === 'pr' && c.args[1] === 'comment');
    expect(created).toBeTruthy();
  });

  it('second-tier upsert edits the existing comment via PATCH (does not create a new one)', () => {
    const state = blankState();
    upsert({
      pr_number: 207,
      repo: 'chitinhq/chitin',
      body: 'R1 says: please address X',
      verdict: verdictR1,
      log: () => {},
      gh: makeGh(state),
    });
    const before = state.comments.length;
    const r = upsert({
      pr_number: 207,
      repo: 'chitinhq/chitin',
      body: 'R2 says: looks good now',
      verdict: verdictR2,
      log: () => {},
      gh: makeGh(state),
    });
    expect(r.action).toBe('edited');
    expect(state.comments.length).toBe(before);
    expect(r.comment_id).toBe(1001);
    expect(state.comments[0].body).toContain('R2 says: looks good now');
    expect(state.comments[0].body).toMatch(/tier=R2.*status=approve/);
    // a PATCH call was made
    const patch = state.calls.find(
      (c) => c.args[0] === 'api' && c.args[1] === '-X' && c.args[2] === 'PATCH',
    );
    expect(patch).toBeTruthy();
  });

  it('missing-marker on a PR with other comments → creates new one (no migration)', () => {
    const state = blankState();
    state.comments.push({ id: 1, body: 'old append-style R0 comment, no marker' });
    state.comments.push({ id: 2, body: 'old append-style R1 comment, no marker' });
    const r = upsert({
      pr_number: 207,
      repo: 'chitinhq/chitin',
      body: 'first chitin status comment on a previously-noisy PR',
      verdict: verdictR1,
      log: () => {},
      gh: makeGh(state),
    });
    expect(r.action).toBe('created');
    // pre-existing comments untouched
    expect(state.comments[0].body).toBe('old append-style R0 comment, no marker');
    expect(state.comments[1].body).toBe('old append-style R1 comment, no marker');
    // a new chitin comment was appended
    expect(state.comments.length).toBe(3);
    expect(state.comments[2].body.startsWith(STATUS_BODY_MARKER)).toBe(true);
  });

  it('malformed verdict input throws (fail loudly before writing)', () => {
    const state = blankState();
    expect(() =>
      upsert({
        pr_number: 207,
        repo: 'chitinhq/chitin',
        body: 'body',
        // @ts-expect-error — exercising the runtime validation path
        verdict: { tier: 'BOGUS', status: 'approve', workflow_id: 'w', ts: '2026-05-05T12:00:00Z' },
        log: () => {},
        gh: makeGh(state),
      }),
    ).toThrow();
    expect(state.comments.length).toBe(0);
  });
});

// ─── schema export sanity ────────────────────────────────────────────────

describe('VerdictMetaSchema', () => {
  it('accepts the canonical shape', () => {
    const r = VerdictMetaSchema.safeParse({
      tier: 'R2',
      status: 'approve',
      workflow_id: 'w',
      ts: '2026-05-05T12:00:00Z',
    });
    expect(r.success).toBe(true);
  });

  it('rejects unknown tier', () => {
    const r = VerdictMetaSchema.safeParse({
      tier: 'R5',
      status: 'approve',
      workflow_id: 'w',
      ts: '2026-05-05T12:00:00Z',
    });
    expect(r.success).toBe(false);
  });

  it('rejects unknown status', () => {
    const r = VerdictMetaSchema.safeParse({
      tier: 'R2',
      status: 'merge',
      workflow_id: 'w',
      ts: '2026-05-05T12:00:00Z',
    });
    expect(r.success).toBe(false);
  });
});
