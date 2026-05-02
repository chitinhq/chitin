import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { ExecutionRequest } from '@chitin/contracts';
import { __test__ } from '../src/activity.ts';

const { planInvocation, resolvePolicySrc, resolveAgent, DRIVER_AGENT_MAP } = __test__;

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

  it('dispatches local-glm and local-deepseek through openclaw (each to their own agent)', () => {
    const glm = planInvocation({ ...baseReq, allowed_drivers: ['local-glm'] });
    const ds = planInvocation({ ...baseReq, allowed_drivers: ['local-deepseek'] });
    expect(glm.command).toBe('openclaw');
    expect(ds.command).toBe('openclaw');
    // Slice 3: each driver goes to its own agent so models can differ.
    expect(glm.args).toContain('glm-agent');
    expect(ds.args).toContain('deepseek-agent');
    expect(glm.args).not.toEqual(ds.args);
  });

  it('routes local-qwen to qwen-agent (not main) post-slice-3', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['local-qwen'] });
    expect(plan.args).toContain('qwen-agent');
    expect(plan.args).not.toContain('main');
  });

  it('throws on an unknown driver (zod-bypassed input)', () => {
    const bad = { ...baseReq, allowed_drivers: ['gpt-5'] } as unknown as ExecutionRequest;
    expect(() => planInvocation(bad)).toThrow(/unknown driver/);
  });

  // Slice 5b: claude-code-headless dispatch.
  it('dispatches claude-code-headless via the `claude` CLI in headless mode', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['claude-code-headless'] });
    expect(plan.command).toBe('claude');
    expect(plan.args).toContain('-p');
    expect(plan.args).toContain(baseReq.prompt);
    expect(plan.args).toContain('--dangerously-skip-permissions');
    expect(plan.args).toContain('--output-format');
    expect(plan.args).toContain('stream-json');
  });

  it('claude-code-headless includes a default --allowedTools scope', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['claude-code-headless'] });
    const idx = plan.args.indexOf('--allowedTools');
    expect(idx).toBeGreaterThan(-1);
    const value = plan.args[idx + 1];
    expect(value).toMatch(/Read|Edit|Bash/);
  });

  it('honors CHITIN_CLAUDE_ALLOWED_TOOLS env override for tighter scope', () => {
    const saved = process.env.CHITIN_CLAUDE_ALLOWED_TOOLS;
    process.env.CHITIN_CLAUDE_ALLOWED_TOOLS = 'Read,Edit';
    try {
      const plan = planInvocation({ ...baseReq, allowed_drivers: ['claude-code-headless'] });
      const idx = plan.args.indexOf('--allowedTools');
      expect(plan.args[idx + 1]).toBe('Read,Edit');
    } finally {
      if (saved === undefined) delete process.env.CHITIN_CLAUDE_ALLOWED_TOOLS;
      else process.env.CHITIN_CLAUDE_ALLOWED_TOOLS = saved;
    }
  });

  it('claude-code-headless includes --include-hook-events for chain visibility', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['claude-code-headless'] });
    expect(plan.args).toContain('--include-hook-events');
  });

  it('local-* (openclaw) drivers include --include-hook-events for chain visibility', () => {
    for (const driver of ['local-qwen', 'local-glm', 'local-deepseek'] as const) {
      const plan = planInvocation({ ...baseReq, allowed_drivers: [driver] });
      expect(plan.command).toBe('openclaw');
      expect(plan.args).toContain('--include-hook-events');
    }
  });
});

describe('extractHookEvents', () => {
  const { extractHookEvents } = __test__;

  it('returns undefined when stdout has no hook_event lines', () => {
    expect(extractHookEvents('')).toBeUndefined();
    expect(extractHookEvents('not json\n{"type":"other"}\n')).toBeUndefined();
  });

  it('projects only the documented summary fields', () => {
    const stdout = [
      JSON.stringify({
        type: 'hook_event',
        hook_name: 'PreToolUse',
        tool_name: 'Bash',
        decision: 'allow',
        reason: 'allowed',
        // extra fields that should be dropped
        agent_id: 'test',
        timestamp: '2026-05-02T00:00:00Z',
      }),
      JSON.stringify({
        type: 'hook_event',
        hook_name: 'PreToolUse',
        tool_name: 'Edit',
        decision: 'deny',
        reason: 'no-governance-self-modification',
      }),
    ].join('\n');
    expect(extractHookEvents(stdout)).toEqual([
      { hook_name: 'PreToolUse', tool_name: 'Bash', decision: 'allow', reason: 'allowed' },
      {
        hook_name: 'PreToolUse',
        tool_name: 'Edit',
        decision: 'deny',
        reason: 'no-governance-self-modification',
      },
    ]);
  });

  it('skips non-JSON lines, JSON without type=hook_event, and unknown decisions', () => {
    const stdout = [
      'plain log line',
      '{"type":"telemetry","value":1}',
      JSON.stringify({ type: 'hook_event', hook_name: 'Stop' }),
      JSON.stringify({ type: 'hook_event', tool_name: 'Read', decision: 'maybe' }),
    ].join('\n');
    const events = extractHookEvents(stdout);
    expect(events).toEqual([
      { hook_name: 'Stop' },
      { tool_name: 'Read' }, // unknown decision dropped, kept the rest
    ]);
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

describe('resolveAgent', () => {
  const envKeys = [
    'CHITIN_AGENT_LOCAL_QWEN',
    'CHITIN_AGENT_LOCAL_GLM',
    'CHITIN_AGENT_LOCAL_DEEPSEEK',
    'CHITIN_AGENT_COPILOT',
  ];
  const saved: Record<string, string | undefined> = {};

  beforeEach(() => {
    for (const k of envKeys) {
      saved[k] = process.env[k];
      delete process.env[k];
    }
  });

  afterEach(() => {
    for (const k of envKeys) {
      if (saved[k] === undefined) delete process.env[k];
      else process.env[k] = saved[k];
    }
  });

  it('routes each local-* driver to its dedicated agent by default', () => {
    expect(resolveAgent('local-qwen')).toBe('qwen-agent');
    expect(resolveAgent('local-glm')).toBe('glm-agent');
    expect(resolveAgent('local-deepseek')).toBe('deepseek-agent');
  });

  it('falls back to main for any driver not in the map', () => {
    // copilot dispatches via the chitin-kernel shim (not openclaw), so
    // resolveAgent isn't called on it in normal use — but the fallback
    // contract still needs to be 'main' for any future driver added to
    // the schema before its mapping lands.
    expect(resolveAgent('copilot')).toBe('main');
  });

  it('honors CHITIN_AGENT_LOCAL_QWEN env override', () => {
    process.env.CHITIN_AGENT_LOCAL_QWEN = 'custom-qwen-agent';
    expect(resolveAgent('local-qwen')).toBe('custom-qwen-agent');
  });

  it('treats whitespace-only env override as unset', () => {
    process.env.CHITIN_AGENT_LOCAL_QWEN = '   ';
    expect(resolveAgent('local-qwen')).toBe('qwen-agent');
  });

  it('trims env override value', () => {
    process.env.CHITIN_AGENT_LOCAL_QWEN = '  trimmed-agent  ';
    expect(resolveAgent('local-qwen')).toBe('trimmed-agent');
  });

  it('does not return the same agent for different local-* drivers (no model collision)', () => {
    const agents = new Set([
      resolveAgent('local-qwen'),
      resolveAgent('local-glm'),
      resolveAgent('local-deepseek'),
    ]);
    expect(agents.size).toBe(3);
  });

  it('exports DRIVER_AGENT_MAP with the expected default routes', () => {
    expect(DRIVER_AGENT_MAP['local-qwen']).toBe('qwen-agent');
    expect(DRIVER_AGENT_MAP['local-glm']).toBe('glm-agent');
    expect(DRIVER_AGENT_MAP['local-deepseek']).toBe('deepseek-agent');
  });
});
