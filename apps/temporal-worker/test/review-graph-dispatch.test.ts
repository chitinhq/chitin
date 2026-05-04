import { describe, expect, it } from 'vitest';
import {
  buildReviewGraphInput,
  parseDiffShortstat,
  extractPrNumber,
  enqueueReviewGraph,
  REVIEW_GRAPH_WORKFLOW_NAME,
} from '../src/review-graph-dispatch.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';
import type { WorktreeResult } from '../src/activity-types.ts';

function makeEntry(overrides: Partial<BacklogEntry> = {}): BacklogEntry {
  return {
    id: 'sample-entry',
    status: 'ready',
    description: 'sample',
    rawFrontmatter: '',
    rawSection: '',
    tier: 'T1',
    file: 'apps/temporal-worker/src/foo.ts',
    ...overrides,
  };
}

function makeWorktree(overrides: Partial<WorktreeResult> = {}): WorktreeResult {
  return {
    path: '/tmp/wt',
    branch: 'swarm/swarm-sample-1',
    head_sha: 'abc1234',
    commits_added: 1,
    has_uncommitted_changes: false,
    diff_shortstat: ' 3 files changed, 47 insertions(+), 12 deletions(-)',
    ...overrides,
  };
}

// ─── parseDiffShortstat ──────────────────────────────────────────────────

describe('parseDiffShortstat', () => {
  it('parses a typical line with insertions + deletions + files changed', () => {
    const r = parseDiffShortstat(' 3 files changed, 47 insertions(+), 12 deletions(-)');
    expect(r).toEqual({ diff_loc: 59, files_changed: 3 });
  });

  it('parses a line with only insertions (no deletions)', () => {
    const r = parseDiffShortstat(' 1 file changed, 100 insertions(+)');
    expect(r).toEqual({ diff_loc: 100, files_changed: 1 });
  });

  it('parses a line with only deletions (no insertions)', () => {
    const r = parseDiffShortstat(' 2 files changed, 30 deletions(-)');
    expect(r).toEqual({ diff_loc: 30, files_changed: 2 });
  });

  it('handles single-file singular phrasing ("1 file changed", "1 insertion")', () => {
    const r = parseDiffShortstat(' 1 file changed, 1 insertion(+)');
    expect(r).toEqual({ diff_loc: 1, files_changed: 1 });
  });

  it('returns zeros for an empty string (no diff case)', () => {
    expect(parseDiffShortstat('')).toEqual({ diff_loc: 0, files_changed: 0 });
  });

  it('returns zeros for a malformed line that has no numbers', () => {
    expect(parseDiffShortstat('weird placeholder')).toEqual({
      diff_loc: 0,
      files_changed: 0,
    });
  });
});

// ─── extractPrNumber ─────────────────────────────────────────────────────

describe('extractPrNumber', () => {
  it('extracts the number from a github.com PR URL', () => {
    expect(extractPrNumber('https://github.com/chitinhq/chitin/pull/142')).toBe(142);
    expect(extractPrNumber('https://github.com/foo/bar/pull/1')).toBe(1);
  });

  it('returns undefined for a URL without /pull/N', () => {
    expect(extractPrNumber('https://github.com/chitinhq/chitin/issues/123')).toBeUndefined();
  });

  it('returns undefined for a non-URL string', () => {
    expect(extractPrNumber('some text')).toBeUndefined();
  });
});

// ─── buildReviewGraphInput ───────────────────────────────────────────────

describe('buildReviewGraphInput', () => {
  it('maps the dispatcher state into a complete ReviewGraphInput', () => {
    const out = buildReviewGraphInput(
      'swarm-sample-12345',
      'https://github.com/chitinhq/chitin/pull/200',
      makeWorktree(),
      makeEntry(),
      'chitinhq/chitin',
    );
    expect(out.parent_workflow_id).toBe('swarm-sample-12345');
    expect(out.pr_meta.pr_number).toBe(200);
    expect(out.pr_meta.pr_url).toBe('https://github.com/chitinhq/chitin/pull/200');
    expect(out.pr_meta.diff_loc).toBe(59);
    expect(out.pr_meta.files_changed).toBe(3);
    expect(out.pr_meta.files).toEqual([]);
    expect(out.entry.id).toBe('sample-entry');
    expect(out.repo).toBe('chitinhq/chitin');
    // copilot_comment_count is left undefined per the docstring (Copilot
    // races us; the prompt's R0-pending branch handles it).
    expect(out.pr_meta.copilot_comment_count).toBeUndefined();
  });

  it('handles missing worktree (rare; activity didn\'t produce one)', () => {
    const out = buildReviewGraphInput(
      'parent-1',
      'https://github.com/o/r/pull/5',
      undefined,
      makeEntry(),
      'o/r',
    );
    expect(out.pr_meta.diff_loc).toBe(0);
    expect(out.pr_meta.files_changed).toBe(0);
  });

  it('leaves pr_number undefined when the URL is malformed', () => {
    const out = buildReviewGraphInput(
      'parent-2',
      'https://example.com/not-a-pr',
      makeWorktree(),
      makeEntry(),
      'o/r',
    );
    expect(out.pr_meta.pr_number).toBeUndefined();
    expect(out.pr_meta.pr_url).toBe('https://example.com/not-a-pr');
  });
});

// ─── enqueueReviewGraph ──────────────────────────────────────────────────

interface MockClient {
  workflow: {
    start: ReturnType<typeof makeStartFn>;
  };
  __startCalls: StartCall[];
}

interface StartCall {
  workflowName: string;
  args: unknown[];
  workflowId: string;
  taskQueue: string;
}

function makeStartFn(calls: StartCall[], opts?: { rejectOn?: string }) {
  return async (
    workflowName: string,
    options: { args: unknown[]; workflowId: string; taskQueue: string },
  ) => {
    if (opts?.rejectOn === workflowName) {
      throw new Error('temporal submit failed (mock)');
    }
    calls.push({
      workflowName,
      args: options.args,
      workflowId: options.workflowId,
      taskQueue: options.taskQueue,
    });
    return { workflowId: options.workflowId, firstExecutionRunId: 'mock-run' };
  };
}

function makeMockClient(opts?: { rejectOn?: string }): MockClient {
  const calls: StartCall[] = [];
  return {
    workflow: { start: makeStartFn(calls, opts) },
    __startCalls: calls,
  };
}

describe('enqueueReviewGraph', () => {
  it('submits the review-graph workflow when pr_url is set', async () => {
    const client = makeMockClient();
    const logs: string[] = [];
    const r = await enqueueReviewGraph({
      // The Client interface we accept matches Temporal's only on the
      // workflow.start surface; cast through unknown for the test.
      client: client as unknown as Parameters<typeof enqueueReviewGraph>[0]['client'],
      taskQueue: 'chitin-worker-q',
      parent_workflow_id: 'swarm-sample-12345',
      pr_url: 'https://github.com/chitinhq/chitin/pull/300',
      worktree: makeWorktree(),
      entry: makeEntry(),
      repo: 'chitinhq/chitin',
      log: (l) => logs.push(l),
    });
    expect(r.enqueued).toBe(true);
    expect(r.workflow_id).toBe('swarm-sample-12345-review-graph');
    expect(client.__startCalls).toHaveLength(1);
    expect(client.__startCalls[0].workflowName).toBe(REVIEW_GRAPH_WORKFLOW_NAME);
    expect(client.__startCalls[0].workflowId).toBe('swarm-sample-12345-review-graph');
    expect(client.__startCalls[0].taskQueue).toBe('chitin-worker-q');
    // Telemetry log line on success.
    expect(logs).toHaveLength(1);
    const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
    expect(parsed.component).toBe('review-graph-dispatch');
    expect(parsed.msg).toBe('review-graph enqueued');
    expect(parsed.diff_loc).toBe(59);
  });

  it('no-ops when pr_url is undefined (PR did not open)', async () => {
    const client = makeMockClient();
    const logs: string[] = [];
    const r = await enqueueReviewGraph({
      client: client as unknown as Parameters<typeof enqueueReviewGraph>[0]['client'],
      taskQueue: 'chitin-worker-q',
      parent_workflow_id: 'swarm-x-1',
      pr_url: undefined,
      worktree: makeWorktree(),
      entry: makeEntry(),
      repo: 'chitinhq/chitin',
      log: (l) => logs.push(l),
    });
    expect(r.enqueued).toBe(false);
    expect(r.workflow_id).toBeUndefined();
    expect(client.__startCalls).toHaveLength(0);
    // No log line — the no-op case is silent (next dispatcher tick
    // gets the next pickup; no point spamming the journal).
    expect(logs).toHaveLength(0);
  });

  it('logs warn and returns enqueued:false when temporal submit throws', async () => {
    const client = makeMockClient({ rejectOn: REVIEW_GRAPH_WORKFLOW_NAME });
    const logs: string[] = [];
    const r = await enqueueReviewGraph({
      client: client as unknown as Parameters<typeof enqueueReviewGraph>[0]['client'],
      taskQueue: 'chitin-worker-q',
      parent_workflow_id: 'swarm-failure-1',
      pr_url: 'https://github.com/chitinhq/chitin/pull/400',
      worktree: makeWorktree(),
      entry: makeEntry(),
      repo: 'chitinhq/chitin',
      log: (l) => logs.push(l),
    });
    expect(r.enqueued).toBe(false);
    expect(r.error).toContain('temporal submit failed');
    // Implementor's work shipped — the warn log captures the event
    // but NEVER propagates as a thrown error.
    expect(logs).toHaveLength(1);
    const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
    expect(parsed.level).toBe('warn');
    expect(parsed.component).toBe('review-graph-dispatch');
  });

  it('passes the correct args shape (ReviewGraphInput) to client.workflow.start', async () => {
    const client = makeMockClient();
    await enqueueReviewGraph({
      client: client as unknown as Parameters<typeof enqueueReviewGraph>[0]['client'],
      taskQueue: 'chitin-worker-q',
      parent_workflow_id: 'parent-arg-shape-1',
      pr_url: 'https://github.com/chitinhq/chitin/pull/500',
      worktree: makeWorktree(),
      entry: makeEntry({ id: 'arg-shape-test' }),
      repo: 'chitinhq/chitin',
    });
    expect(client.__startCalls).toHaveLength(1);
    const [arg] = client.__startCalls[0].args as [
      {
        parent_workflow_id: string;
        pr_meta: { pr_number: number; pr_url: string };
        entry: BacklogEntry;
        repo: string;
      },
    ];
    expect(arg.parent_workflow_id).toBe('parent-arg-shape-1');
    expect(arg.pr_meta.pr_number).toBe(500);
    expect(arg.entry.id).toBe('arg-shape-test');
    expect(arg.repo).toBe('chitinhq/chitin');
  });

  it('embeds tier_config from CHITIN_REVIEWER_R<N>_{DRIVER,MODEL} env into workflow args', async () => {
    // The workflow runs in a V8 isolate without `process` — env
    // override resolution has to happen here in the dispatcher and
    // be threaded through input. Regression test for Copilot's
    // catch on PR #280: env overrides were a dead code path.
    const orig = {
      r1d: process.env.CHITIN_REVIEWER_R1_DRIVER,
      r1m: process.env.CHITIN_REVIEWER_R1_MODEL,
      r3d: process.env.CHITIN_REVIEWER_R3_DRIVER,
    };
    process.env.CHITIN_REVIEWER_R1_DRIVER = 'codex-cli';
    process.env.CHITIN_REVIEWER_R1_MODEL = 'gpt-5.4';
    process.env.CHITIN_REVIEWER_R3_DRIVER = 'gemini-cli';
    try {
      const client = makeMockClient();
      await enqueueReviewGraph({
        client: client as unknown as Parameters<typeof enqueueReviewGraph>[0]['client'],
        taskQueue: 'chitin-worker-q',
        parent_workflow_id: 'parent-tier-config-1',
        pr_url: 'https://github.com/chitinhq/chitin/pull/600',
        worktree: makeWorktree(),
        entry: makeEntry(),
        repo: 'chitinhq/chitin',
      });
      const [arg] = client.__startCalls[0].args as [
        { tier_config?: Record<string, { driver: string | null; model: string | null }> },
      ];
      expect(arg.tier_config).toBeDefined();
      expect(arg.tier_config?.R1).toEqual({ driver: 'codex-cli', model: 'gpt-5.4' });
      expect(arg.tier_config?.R3?.driver).toBe('gemini-cli');
      // R2 unset → falls back to the static default.
      expect(arg.tier_config?.R2?.driver).toBe('copilot');
    } finally {
      if (orig.r1d === undefined) delete process.env.CHITIN_REVIEWER_R1_DRIVER;
      else process.env.CHITIN_REVIEWER_R1_DRIVER = orig.r1d;
      if (orig.r1m === undefined) delete process.env.CHITIN_REVIEWER_R1_MODEL;
      else process.env.CHITIN_REVIEWER_R1_MODEL = orig.r1m;
      if (orig.r3d === undefined) delete process.env.CHITIN_REVIEWER_R3_DRIVER;
      else process.env.CHITIN_REVIEWER_R3_DRIVER = orig.r3d;
    }
  });
});
