import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { ExecutionRequest } from '@chitin/contracts';
import { __test__ } from '../src/activity.ts';

const { planInvocation, resolvePolicySrc } = __test__;

const baseReq: ExecutionRequest = {
  schema_version: '1',
  workflow_id: 'wf-test-001',
  run_id: 'wf-test-001-attempt-1',
  repo: 'chitinhq/chitin',
  task_class: 'refactor',
  risk_level: 'low',
  allowed_drivers: ['copilot'],
  network_policy: 'allowlist',
  write_policy: 'worktree',
  bounds: { max_tool_calls: 50, max_cost_usd: 0.5, wall_timeout_s: 600 },
  prompt: 'rename FooBar to BarBaz',
};

describe('planInvocation', () => {
  it('dispatches copilot through the chitin shim', () => {
    const plan = planInvocation(baseReq);
    expect(plan.command).toBe('chitin-kernel');
    expect(plan.args).toEqual(['drive', 'copilot', baseReq.prompt]);
  });

  it('dispatches local-qwen through openclaw with the prompt and timeout', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['local-qwen'] });
    expect(plan.command).toBe('openclaw');
    expect(plan.args).toContain('--message');
    expect(plan.args).toContain(baseReq.prompt);
    expect(plan.args).toContain('--timeout');
    expect(plan.args).toContain(String(baseReq.bounds.wall_timeout_s));
  });

  it('dispatches local-glm and local-deepseek through openclaw (same shape)', () => {
    const glm = planInvocation({ ...baseReq, allowed_drivers: ['local-glm'] });
    const ds = planInvocation({ ...baseReq, allowed_drivers: ['local-deepseek'] });
    expect(glm.command).toBe('openclaw');
    expect(ds.command).toBe('openclaw');
    expect(glm.args).toEqual(ds.args);
  });

  it('throws on an unknown driver (zod-bypassed input)', () => {
    const bad = { ...baseReq, allowed_drivers: ['gpt-5'] } as unknown as ExecutionRequest;
    expect(() => planInvocation(bad)).toThrow(/unknown driver/);
  });
});

describe('resolvePolicySrc', () => {
  let saved: string | undefined;
  let savedCwd: string;
  let scratch: string;

  beforeEach(() => {
    saved = process.env.CHITIN_POLICY_FILE;
    savedCwd = process.cwd();
    scratch = mkdtempSync(join(tmpdir(), 'chitin-policy-test-'));
  });

  afterEach(() => {
    if (saved === undefined) delete process.env.CHITIN_POLICY_FILE;
    else process.env.CHITIN_POLICY_FILE = saved;
    process.chdir(savedCwd);
    rmSync(scratch, { recursive: true, force: true });
  });

  it('honors CHITIN_POLICY_FILE when set (absolute path)', () => {
    const explicit = join(scratch, 'custom.yaml');
    writeFileSync(explicit, 'id: test\n');
    process.env.CHITIN_POLICY_FILE = explicit;
    expect(resolvePolicySrc()).toBe(explicit);
  });

  it('falls back to <cwd>/chitin.yaml when env var is unset', () => {
    delete process.env.CHITIN_POLICY_FILE;
    process.chdir(scratch);
    expect(resolvePolicySrc()).toBe(join(scratch, 'chitin.yaml'));
  });

  it('does not hardcode any developer-machine path', () => {
    delete process.env.CHITIN_POLICY_FILE;
    process.chdir(scratch);
    expect(resolvePolicySrc()).not.toMatch(/\/home\/red\//);
  });
});
