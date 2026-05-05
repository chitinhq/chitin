import { describe, expect, it } from 'vitest';
import { mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import {
  buildPeerReviewerEntry,
  buildPeerReviewerRequest,
  enqueuePeerReviewer,
  extractPrNumber,
  peerReviewerWorkflowIdForPr,
} from '../src/peer-reviewer/dispatch.ts';
import type { SpawnFnInput } from '../src/spawn-execute-request.ts';

function withFreshDirs<T>(body: () => Promise<T>): Promise<T> {
  const root = resolve(tmpdir(), `peer-rev-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
  const logDir = resolve(root, 'logs');
  const argsDir = resolve(root, 'args');
  mkdirSync(logDir, { recursive: true });
  mkdirSync(argsDir, { recursive: true });
  const orig = {
    log: process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR,
    args: process.env.CHITIN_EXECUTE_REQUEST_ARGS_DIR,
  };
  process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR = logDir;
  process.env.CHITIN_EXECUTE_REQUEST_ARGS_DIR = argsDir;
  return body().finally(() => {
    if (orig.log === undefined) delete process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR;
    else process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR = orig.log;
    if (orig.args === undefined) delete process.env.CHITIN_EXECUTE_REQUEST_ARGS_DIR;
    else process.env.CHITIN_EXECUTE_REQUEST_ARGS_DIR = orig.args;
    rmSync(root, { recursive: true, force: true });
  });
}

describe('peerReviewerWorkflowIdForPr', () => {
  it('produces a stable id per PR number', () => {
    expect(peerReviewerWorkflowIdForPr(199)).toBe('peer-review-pr-199');
    expect(peerReviewerWorkflowIdForPr(207)).toBe('peer-review-pr-207');
  });
});

describe('extractPrNumber', () => {
  it('parses a github pull URL', () => {
    expect(extractPrNumber('https://github.com/chitinhq/chitin/pull/207')).toBe(207);
  });

  it('throws on a URL without /pull/', () => {
    expect(() => extractPrNumber('https://example.com/x')).toThrow(/does not contain/);
  });
});

describe('buildPeerReviewerEntry', () => {
  it('embeds the PR URL in the description (the prompt step-0 guard reads it)', () => {
    const entry = buildPeerReviewerEntry(
      'https://github.com/chitinhq/chitin/pull/207',
      'chitinhq/chitin',
    );
    expect(entry.description).toContain('https://github.com/chitinhq/chitin/pull/207');
    expect(entry.role).toBe('peer-reviewer');
    expect(entry.id).toBe('peer-review-pr-207');
    expect(entry.status).toBe('ready');
  });

  it('includes the repo slug', () => {
    const entry = buildPeerReviewerEntry(
      'https://github.com/chitinhq/chitin/pull/1',
      'chitinhq/chitin',
    );
    expect(entry.description).toContain('chitinhq/chitin');
  });
});

describe('buildPeerReviewerRequest', () => {
  it('produces an ExecutionRequest with read-only bounds', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.write_policy).toBe('none');
    expect(req.network_policy).toBe('allowlist');
    expect(req.role).toBe('peer-reviewer');
    expect(req.task_class).toBe('exploration');
    expect(req.risk_level).toBe('low');
  });

  it('omits base_ref so the activity skips worktree+apply', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.base_ref).toBeUndefined();
  });

  it('default driver is copilot, default tier is T2', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.allowed_drivers).toEqual(['copilot']);
    expect(req.tier).toBe('T2');
  });

  it('honors driver + tier overrides', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
      driver: 'claude-code-headless',
      tier: 'T3',
    });
    expect(req.allowed_drivers).toEqual(['claude-code-headless']);
    expect(req.tier).toBe('T3');
  });

  it('workflow_id matches the stable per-PR id', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.workflow_id).toBe('peer-review-pr-207');
  });

  it('embeds the prompt with the PR URL in entry detail (step-0 guard wired)', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.prompt).toContain('https://github.com/chitinhq/chitin/pull/207');
    // Sanity: the prompt's step-0 guard markers are present
    expect(req.prompt).toMatch(/Verify your dispatch shape FIRST/);
  });

  it('wall_timeout=900s — peer review shouldn\'t be slow', () => {
    const req = buildPeerReviewerRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.bounds.wall_timeout_s).toBe(900);
  });
});

describe('enqueuePeerReviewer', () => {
  it('spawns chitin-execute-request with the built request when not already running', () =>
    withFreshDirs(async () => {
      const calls: SpawnFnInput[] = [];
      const logs: string[] = [];
      const r = await enqueuePeerReviewer({
        pr_url: 'https://github.com/chitinhq/chitin/pull/200',
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnFn: async (input) => { calls.push(input); },
      });
      expect(r.enqueued).toBe(true);
      expect(r.workflow_id).toBe('peer-review-pr-200');
      expect(calls).toHaveLength(1);
      expect(calls[0].workflow_id).toBe('peer-review-pr-200');
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.msg).toBe('peer-reviewer enqueued');
      expect(parsed.workflow_id).toBe('peer-review-pr-200');
    }));

  it('skips spawn + logs info when an in-flight log file exists', () =>
    withFreshDirs(async () => {
      const logDir = process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR!;
      writeFileSync(resolve(logDir, 'peer-review-pr-201.log'), '');
      const calls: SpawnFnInput[] = [];
      const logs: string[] = [];
      const r = await enqueuePeerReviewer({
        pr_url: 'https://github.com/chitinhq/chitin/pull/201',
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnFn: async (input) => { calls.push(input); },
      });
      expect(r.enqueued).toBe(false);
      expect(r.workflow_id).toBe('peer-review-pr-201');
      expect(calls).toHaveLength(0);
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.level).toBe('info');
      expect(parsed.msg).toBe('peer-reviewer already in flight; skipping spawn');
    }));

  it('logs warn + returns enqueued=false when spawn throws', () =>
    withFreshDirs(async () => {
      const logs: string[] = [];
      const r = await enqueuePeerReviewer({
        pr_url: 'https://github.com/chitinhq/chitin/pull/202',
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnFn: async () => { throw new Error('mock spawn failed'); },
      });
      expect(r.enqueued).toBe(false);
      expect(r.error).toContain('mock spawn failed');
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.level).toBe('warn');
      expect(parsed.msg).toContain('spawn failed');
    }));
});
