import { describe, expect, it } from 'vitest';
import { mkdirSync, writeFileSync, rmSync, utimesSync, readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { tmpdir } from 'node:os';
import {
  spawnExecuteRequest,
  isExecuteRequestRunning,
  listRunningExecuteRequestsFromDisk,
  type SpawnFnInput,
} from '../src/spawn-execute-request.ts';
import type { ExecutionRequest } from '@chitin/contracts';

function makeRequest(overrides: Partial<ExecutionRequest> = {}): ExecutionRequest {
  return {
    schema_version: '1',
    workflow_id: 'test-wf-1',
    run_id: 'test-wf-1-attempt-1',
    repo: 'chitinhq/chitin',
    task_class: 'exploration',
    risk_level: 'low',
    allowed_drivers: ['copilot'],
    network_policy: 'allowlist',
    write_policy: 'none',
    bounds: { max_tool_calls: 5, max_cost_usd: 0, wall_timeout_s: 60 },
    prompt: 'noop',
    role: 'peer-reviewer',
    ...overrides,
  } as ExecutionRequest;
}

function withFreshDirs<T>(body: (logDir: string, argsDir: string) => Promise<T>): Promise<T> {
  const root = resolve(tmpdir(), `spawn-exec-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
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
  return body(logDir, argsDir).finally(() => {
    if (orig.log === undefined) delete process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR;
    else process.env.CHITIN_EXECUTE_REQUEST_LOG_DIR = orig.log;
    if (orig.args === undefined) delete process.env.CHITIN_EXECUTE_REQUEST_ARGS_DIR;
    else process.env.CHITIN_EXECUTE_REQUEST_ARGS_DIR = orig.args;
    rmSync(root, { recursive: true, force: true });
  });
}

describe('isExecuteRequestRunning', () => {
  it('returns false when log file does not exist', () =>
    withFreshDirs(async () => {
      expect(isExecuteRequestRunning('does-not-exist')).toBe(false);
    }));

  it('returns true when log file exists with recent mtime', () =>
    withFreshDirs(async (logDir) => {
      writeFileSync(resolve(logDir, 'fresh-wf.log'), '');
      expect(isExecuteRequestRunning('fresh-wf')).toBe(true);
    }));

  it('returns false when log file exists but mtime is older than the dedup window', () =>
    withFreshDirs(async (logDir) => {
      const p = resolve(logDir, 'stale-wf.log');
      writeFileSync(p, '');
      const t = (Date.now() - 2 * 60 * 60 * 1000) / 1000;  // 2h ago
      utimesSync(p, t, t);
      expect(isExecuteRequestRunning('stale-wf')).toBe(false);
    }));
});

describe('listRunningExecuteRequestsFromDisk', () => {
  it('returns empty when log dir does not exist', () =>
    withFreshDirs(async (logDir) => {
      rmSync(logDir, { recursive: true, force: true });
      expect(listRunningExecuteRequestsFromDisk().size).toBe(0);
    }));

  it('returns recent log file ids; skips stale + non-.log files', () =>
    withFreshDirs(async (logDir) => {
      writeFileSync(resolve(logDir, 'fresh-1.log'), '');
      writeFileSync(resolve(logDir, 'fresh-2.log'), '');
      writeFileSync(resolve(logDir, 'stale.log'), '');
      const t = (Date.now() - 2 * 60 * 60 * 1000) / 1000;
      utimesSync(resolve(logDir, 'stale.log'), t, t);
      writeFileSync(resolve(logDir, 'README'), '');  // ignored

      const ids = listRunningExecuteRequestsFromDisk();
      expect(ids.has('fresh-1')).toBe(true);
      expect(ids.has('fresh-2')).toBe(true);
      expect(ids.has('stale')).toBe(false);
      expect(ids.has('README')).toBe(false);
      expect(ids.size).toBe(2);
    }));
});

describe('spawnExecuteRequest', () => {
  it('writes the request JSON to disk and invokes spawnFn with the right paths', () =>
    withFreshDirs(async (logDir, argsDir) => {
      const calls: SpawnFnInput[] = [];
      const r = await spawnExecuteRequest({
        request: makeRequest({ workflow_id: 'wf-write-test' }),
        spawnFn: async (input) => {
          calls.push(input);
        },
      });
      expect(r.enqueued).toBe(true);
      expect(r.workflow_id).toBe('wf-write-test');
      expect(calls).toHaveLength(1);
      expect(calls[0].workflow_id).toBe('wf-write-test');
      expect(calls[0].requestPath).toBe(resolve(argsDir, 'wf-write-test.json'));
      expect(calls[0].logPath).toBe(resolve(logDir, 'wf-write-test.log'));
      expect(calls[0].bin).toMatch(/chitin-execute-request$/);

      // Request file actually written + parseable
      const written = JSON.parse(readFileSync(calls[0].requestPath, 'utf8'));
      expect(written.workflow_id).toBe('wf-write-test');
    }));

  it('skips spawn when dedup oracle says already running', () =>
    withFreshDirs(async (logDir) => {
      writeFileSync(resolve(logDir, 'wf-running.log'), '');
      const calls: SpawnFnInput[] = [];
      const r = await spawnExecuteRequest({
        request: makeRequest({ workflow_id: 'wf-running' }),
        spawnFn: async (input) => { calls.push(input); },
      });
      expect(r.enqueued).toBe(false);
      expect(r.skipped_already_running).toBe(true);
      expect(r.workflow_id).toBe('wf-running');
      expect(calls).toHaveLength(0);
    }));

  it('bypasses dedup when input.dedup is false', () =>
    withFreshDirs(async (logDir) => {
      writeFileSync(resolve(logDir, 'wf-bypass.log'), '');
      const calls: SpawnFnInput[] = [];
      const r = await spawnExecuteRequest({
        request: makeRequest({ workflow_id: 'wf-bypass' }),
        dedup: false,
        spawnFn: async (input) => { calls.push(input); },
      });
      expect(r.enqueued).toBe(true);
      expect(calls).toHaveLength(1);
    }));

  it('returns enqueued=false with error when spawnFn throws', () =>
    withFreshDirs(async () => {
      const r = await spawnExecuteRequest({
        request: makeRequest({ workflow_id: 'wf-throw' }),
        spawnFn: async () => {
          throw new Error('mock spawn explosion');
        },
      });
      expect(r.enqueued).toBe(false);
      expect(r.skipped_already_running).toBeUndefined();
      expect(r.error).toContain('mock spawn explosion');
    }));

  it('re-spawns when log mtime is older than the dedup window', () =>
    withFreshDirs(async (logDir) => {
      const p = resolve(logDir, 'wf-stale.log');
      writeFileSync(p, '');
      const t = (Date.now() - 2 * 60 * 60 * 1000) / 1000;
      utimesSync(p, t, t);
      const calls: SpawnFnInput[] = [];
      const r = await spawnExecuteRequest({
        request: makeRequest({ workflow_id: 'wf-stale' }),
        spawnFn: async (input) => { calls.push(input); },
      });
      expect(r.enqueued).toBe(true);
      expect(calls).toHaveLength(1);
    }));
});
