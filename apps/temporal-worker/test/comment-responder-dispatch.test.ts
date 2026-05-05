import { describe, expect, it } from 'vitest';
import { mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import {
  buildCommentResponderEntry,
  buildCommentResponderRequest,
  commentResponderWorkflowIdForPr,
  enqueueCommentResponder,
} from '../src/comment-responder/dispatch.ts';
import type { SpawnFnInput } from '../src/spawn-execute-request.ts';

function withFreshDirs<T>(body: () => Promise<T>): Promise<T> {
  const root = resolve(tmpdir(), `cr-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
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

describe('commentResponderWorkflowIdForPr', () => {
  it('produces a stable id per PR number', () => {
    expect(commentResponderWorkflowIdForPr(199)).toBe('comment-respond-pr-199');
    expect(commentResponderWorkflowIdForPr(207)).toBe('comment-respond-pr-207');
  });
});

describe('buildCommentResponderEntry', () => {
  it('embeds the PR URL in the description (prompt step-0 guard reads it)', () => {
    const entry = buildCommentResponderEntry(
      'https://github.com/chitinhq/chitin/pull/207',
      'chitinhq/chitin',
    );
    expect(entry.description).toContain('https://github.com/chitinhq/chitin/pull/207');
    expect(entry.role).toBe('comment-responder');
    expect(entry.id).toBe('comment-respond-pr-207');
  });

  it('mentions the do-NOT-dismiss-as-noise rule in the description', () => {
    const entry = buildCommentResponderEntry(
      'https://github.com/chitinhq/chitin/pull/1',
      'chitinhq/chitin',
    );
    expect(entry.description).toMatch(/do NOT dismiss as noise/);
  });
});

describe('buildCommentResponderRequest', () => {
  it('produces an ExecutionRequest with branch-write bounds', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.write_policy).toBe('branch');
    expect(req.network_policy).toBe('allowlist');
    expect(req.role).toBe('comment-responder');
    expect(req.task_class).toBe('bug_fix');
    expect(req.risk_level).toBe('medium');
  });

  it('omits base_ref so the activity skips worktree+apply (agent does its own gh pr checkout)', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.base_ref).toBeUndefined();
  });

  it('default driver is copilot at T2 (judgment-tier reasoning)', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.allowed_drivers).toEqual(['copilot']);
    expect(req.tier).toBe('T2');
  });

  it('honors driver + tier overrides', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
      driver: 'claude-code-headless',
      tier: 'T4',
    });
    expect(req.allowed_drivers).toEqual(['claude-code-headless']);
    expect(req.tier).toBe('T4');
  });

  it('caps tool calls at 80 (handles 5-15 comments × evaluate+edit+commit+push)', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.bounds.max_tool_calls).toBe(80);
  });

  it('wall_timeout=1800s — tracks the R3 reviewer ceiling', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/1',
      repo: 'chitinhq/chitin',
    });
    expect(req.bounds.wall_timeout_s).toBe(1800);
  });

  it('workflow_id matches the stable per-PR id', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.workflow_id).toBe('comment-respond-pr-207');
  });

  it('embeds the prompt with the PR URL in entry detail (step-0 guard wired)', () => {
    const req = buildCommentResponderRequest({
      pr_url: 'https://github.com/chitinhq/chitin/pull/207',
      repo: 'chitinhq/chitin',
    });
    expect(req.prompt).toContain('https://github.com/chitinhq/chitin/pull/207');
    expect(req.prompt).toMatch(/Verify your dispatch shape FIRST/);
    expect(req.prompt).toMatch(/Reply to each individual review comment thread/);
  });
});

describe('enqueueCommentResponder', () => {
  it('spawns chitin-execute-request when not already running', () =>
    withFreshDirs(async () => {
      const calls: SpawnFnInput[] = [];
      const logs: string[] = [];
      const r = await enqueueCommentResponder({
        pr_url: 'https://github.com/chitinhq/chitin/pull/300',
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnFn: async (input) => { calls.push(input); },
      });
      expect(r.enqueued).toBe(true);
      expect(r.workflow_id).toBe('comment-respond-pr-300');
      expect(calls).toHaveLength(1);
      expect(calls[0].workflow_id).toBe('comment-respond-pr-300');
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.msg).toBe('comment-responder enqueued');
    }));

  it('skips spawn + logs info when an in-flight log file exists', () =>
    withFreshDirs(async () => {
      const logDir = process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR!;
      writeFileSync(resolve(logDir, 'comment-respond-pr-301.log'), '');
      const calls: SpawnFnInput[] = [];
      const logs: string[] = [];
      const r = await enqueueCommentResponder({
        pr_url: 'https://github.com/chitinhq/chitin/pull/301',
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnFn: async (input) => { calls.push(input); },
      });
      expect(r.enqueued).toBe(false);
      expect(r.workflow_id).toBe('comment-respond-pr-301');
      expect(calls).toHaveLength(0);
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.level).toBe('info');
      expect(parsed.msg).toBe('comment-responder already in flight; skipping spawn');
    }));

  it('logs warn + returns enqueued=false when spawn throws', () =>
    withFreshDirs(async () => {
      const logs: string[] = [];
      const r = await enqueueCommentResponder({
        pr_url: 'https://github.com/chitinhq/chitin/pull/302',
        repo: 'chitinhq/chitin',
        log: (l) => logs.push(l),
        spawnFn: async () => { throw new Error('mock spawn explosion'); },
      });
      expect(r.enqueued).toBe(false);
      expect(r.error).toContain('mock spawn explosion');
      const parsed = JSON.parse(logs[0]) as Record<string, unknown>;
      expect(parsed.level).toBe('warn');
    }));
});
