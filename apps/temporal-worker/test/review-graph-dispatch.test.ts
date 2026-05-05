import { describe, expect, it } from 'vitest';
import { mkdirSync, writeFileSync, rmSync, utimesSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import {
  buildReviewGraphInput,
  parseDiffShortstat,
  extractPrNumber,
  enqueueReviewGraph,
  listRunningReviewGraphWorkflowsFromDisk,
  type LobsterSpawnInput,
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

// ─── enqueueReviewGraph (post-Temporal cut-over) ────────────────────────

interface SpawnCall {
  reviewGraphId: string;
  args: Record<string, string>;  // parsed argsJson for assertion convenience
}

function makeMockSpawn(opts?: { reject?: boolean }) {
  const calls: SpawnCall[] = [];
  const fn = async (input: LobsterSpawnInput) => {
    if (opts?.reject) {
      throw new Error('lobster spawn failed (mock)');
    }
    calls.push({ reviewGraphId: input.reviewGraphId, args: JSON.parse(input.argsJson) });
  };
  return { fn, calls };
}

// Each test gets its own log dir so the mtime-based dedup oracle
// doesn't cross-contaminate between tests. Set via env var which
// review-graph-dispatch.ts reads at log-path resolution time.
function withFreshLogDir<T>(body: (logDir: string) => Promise<T>): Promise<T> {
  const d = resolve(tmpdir(), `review-graph-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
  mkdirSync(d, { recursive: true });
  const orig = process.env.LOBSTER_REVIEW_GRAPH_LOG_DIR;
  process.env.LOBSTER_REVIEW_GRAPH_LOG_DIR = d;
  return body(d).finally(() => {
    if (orig === undefined) delete process.env.LOBSTER_REVIEW_GRAPH_LOG_DIR;
    else process.env.LOBSTER_REVIEW_GRAPH_LOG_DIR = orig;
    rmSync(d, { recursive: true, force: true });
  });
}

describe('enqueueReviewGraph', () => {
  it('spawns lobster when pr_url is set', () =>
    withFreshLogDir(async () => {
      const spawn = makeMockSpawn();
      const logs: string[] = [];
      const r = await enqueueReviewGraph({
        parent_workflow_id: 'swarm-sample-12345',
        pr_url: 'https://github.com/chitinhq/chitin/pull/300',
        worktree: makeWorktree(),
        entry: makeEntry(),
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnLobster: spawn.fn,
      });
      expect(r.enqueued).toBe(true);
      expect(r.workflow_id).toBe('swarm-sample-12345-review-graph');
      expect(spawn.calls).toHaveLength(1);
      expect(spawn.calls[0].reviewGraphId).toBe('swarm-sample-12345-review-graph');
      // Telemetry log line on success.
      expect(logs).toHaveLength(1);
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.component).toBe('review-graph-dispatch');
      expect(parsed.msg).toBe('review-graph enqueued');
      expect(parsed.diff_loc).toBe(59);
    }));

  it('no-ops when pr_url is undefined (PR did not open)', () =>
    withFreshLogDir(async () => {
      const spawn = makeMockSpawn();
      const logs: string[] = [];
      const r = await enqueueReviewGraph({
        parent_workflow_id: 'swarm-x-1',
        pr_url: undefined,
        worktree: makeWorktree(),
        entry: makeEntry(),
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnLobster: spawn.fn,
      });
      expect(r.enqueued).toBe(false);
      expect(r.workflow_id).toBeUndefined();
      expect(spawn.calls).toHaveLength(0);
      expect(logs).toHaveLength(0);
    }));

  it('logs warn and returns enqueued:false when lobster spawn throws', () =>
    withFreshLogDir(async () => {
      const spawn = makeMockSpawn({ reject: true });
      const logs: string[] = [];
      const r = await enqueueReviewGraph({
        parent_workflow_id: 'swarm-failure-1',
        pr_url: 'https://github.com/chitinhq/chitin/pull/400',
        worktree: makeWorktree(),
        entry: makeEntry(),
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnLobster: spawn.fn,
      });
      expect(r.enqueued).toBe(false);
      expect(r.error).toContain('lobster spawn failed');
      expect(logs).toHaveLength(1);
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.level).toBe('warn');
      expect(parsed.component).toBe('review-graph-dispatch');
    }));

  it('passes the correct args (PrMeta + entry_id + repo) to lobster --args-json', () =>
    withFreshLogDir(async () => {
      const spawn = makeMockSpawn();
      await enqueueReviewGraph({
        parent_workflow_id: 'parent-arg-shape-1',
        pr_url: 'https://github.com/chitinhq/chitin/pull/500',
        worktree: makeWorktree(),
        entry: makeEntry({ id: 'arg-shape-test' }),
        repo: 'chitinhq/chitin',
        spawnLobster: spawn.fn,
      });
      expect(spawn.calls).toHaveLength(1);
      const args = spawn.calls[0].args;
      expect(args.entry_id).toBe('arg-shape-test');
      expect(args.repo).toBe('chitinhq/chitin');
      expect(args.parent_workflow_id).toBe('parent-arg-shape-1');
      const prMeta = JSON.parse(args.pr_meta_json) as { pr_number?: number; pr_url?: string };
      expect(prMeta.pr_number).toBe(500);
      expect(prMeta.pr_url).toBe('https://github.com/chitinhq/chitin/pull/500');
    }));

  it('skips spawn when log file mtime is recent (dedup oracle)', () =>
    withFreshLogDir(async (logDir) => {
      // Pre-create a recent log file simulating an in-flight graph.
      const reviewGraphId = 'parent-already-running-review-graph';
      writeFileSync(resolve(logDir, `${reviewGraphId}.log`), 'pretend lobster output\n');
      const spawn = makeMockSpawn();
      const logs: string[] = [];
      const r = await enqueueReviewGraph({
        parent_workflow_id: 'parent-already-running',
        pr_url: 'https://github.com/chitinhq/chitin/pull/700',
        worktree: makeWorktree(),
        entry: makeEntry(),
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnLobster: spawn.fn,
      });
      expect(r.enqueued).toBe(false);
      expect(r.skipped_already_running).toBe(true);
      expect(r.workflow_id).toBe(reviewGraphId);
      expect(spawn.calls).toHaveLength(0);
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.level).toBe('info');
      expect(parsed.msg).toBe('review-graph already in flight; skipping spawn');
    }));

  it('re-spawns when log file mtime is older than dedup window (stale)', () =>
    withFreshLogDir(async (logDir) => {
      const reviewGraphId = 'parent-stale-review-graph';
      const logPath = resolve(logDir, `${reviewGraphId}.log`);
      writeFileSync(logPath, 'old lobster run\n');
      // Backdate mtime by 2 hours (window default is 1 hour).
      const twoHoursAgo = (Date.now() - 2 * 60 * 60 * 1000) / 1000;
      utimesSync(logPath, twoHoursAgo, twoHoursAgo);

      const spawn = makeMockSpawn();
      const r = await enqueueReviewGraph({
        parent_workflow_id: 'parent-stale',
        pr_url: 'https://github.com/chitinhq/chitin/pull/800',
        worktree: makeWorktree(),
        entry: makeEntry(),
        repo: 'chitinhq/chitin',
        spawnLobster: spawn.fn,
      });
      expect(r.enqueued).toBe(true);
      expect(spawn.calls).toHaveLength(1);
    }));
});

// ─── listRunningReviewGraphWorkflowsFromDisk ────────────────────────────

describe('listRunningReviewGraphWorkflowsFromDisk', () => {
  it('returns empty when log dir does not exist', () =>
    withFreshLogDir(async (d) => {
      rmSync(d, { recursive: true, force: true });  // delete the dir
      const ids = listRunningReviewGraphWorkflowsFromDisk();
      expect(ids.size).toBe(0);
    }));

  it('returns ids whose log files have mtime within the dedup window', () =>
    withFreshLogDir(async (d) => {
      writeFileSync(resolve(d, 'fresh-1-review-graph.log'), '');
      writeFileSync(resolve(d, 'fresh-2-review-graph.log'), '');
      // Stale: backdate by 2 hours
      writeFileSync(resolve(d, 'stale-review-graph.log'), '');
      const t = (Date.now() - 2 * 60 * 60 * 1000) / 1000;
      utimesSync(resolve(d, 'stale-review-graph.log'), t, t);
      // Non-log file ignored
      writeFileSync(resolve(d, 'README'), '');

      const ids = listRunningReviewGraphWorkflowsFromDisk();
      expect(ids.has('fresh-1-review-graph')).toBe(true);
      expect(ids.has('fresh-2-review-graph')).toBe(true);
      expect(ids.has('stale-review-graph')).toBe(false);
      expect(ids.size).toBe(2);
    }));
});
