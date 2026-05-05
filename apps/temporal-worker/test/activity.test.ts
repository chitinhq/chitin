import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { ExecutionRequest } from '@chitin/contracts';
import { homedir } from 'node:os';
import { resolve } from 'node:path';
import { __test__, isWorkerOwnedPath } from '../src/activity.ts';

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

  it('dispatches openclaw-glm-flash through openclaw with the prompt and timeout', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['openclaw-glm-flash'] });
    expect(plan.command).toBe('openclaw');
    expect(plan.args).toContain('--message');
    expect(plan.args).toContain(baseReq.prompt);
    expect(plan.args).toContain('--timeout');
    expect(plan.args).toContain(String(baseReq.bounds.wall_timeout_s));
  });

  it('dispatches openclaw-glm-cloud and openclaw-deepseek through openclaw (each to its own agent)', () => {
    const glm = planInvocation({ ...baseReq, allowed_drivers: ['openclaw-glm-cloud'] });
    const ds = planInvocation({ ...baseReq, allowed_drivers: ['openclaw-deepseek'] });
    expect(glm.command).toBe('openclaw');
    expect(ds.command).toBe('openclaw');
    // Each driver goes to its own openclaw agent so models can differ.
    expect(glm.args).toContain('glm-agent');
    expect(ds.args).toContain('deepseek-agent');
    expect(glm.args).not.toEqual(ds.args);
  });

  it('routes openclaw-glm-flash to glm-flash-agent (not main)', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['openclaw-glm-flash'] });
    expect(plan.args).toContain('glm-flash-agent');
    expect(plan.args).not.toContain('main');
  });

  it('dispatches gemini via the `gemini` CLI with -p prompt', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['gemini'] });
    expect(plan.command).toBe('gemini');
    expect(plan.args).toContain('-p');
    expect(plan.args).toContain(baseReq.prompt);
  });

  it('gemini respects CHITIN_MODEL_GEMINI env override', () => {
    const orig = process.env.CHITIN_MODEL_GEMINI;
    try {
      process.env.CHITIN_MODEL_GEMINI = 'gemini-3.0-pro';
      const plan = planInvocation({ ...baseReq, allowed_drivers: ['gemini'] });
      const idx = plan.args.indexOf('-m');
      expect(idx).toBeGreaterThan(-1);
      expect(plan.args[idx + 1]).toBe('gemini-3.0-pro');
    } finally {
      if (orig === undefined) delete process.env.CHITIN_MODEL_GEMINI;
      else process.env.CHITIN_MODEL_GEMINI = orig;
    }
  });

  it('dispatches codex via `codex exec --json` without --cd (req.repo is a slug, spawn cwd handles dir)', () => {
    const plan = planInvocation({
      ...baseReq,
      allowed_drivers: ['codex'],
      // baseReq.repo is "chitinhq/chitin" — a slug, not a filesystem
      // path. Confirm planInvocation does NOT pass it through --cd
      // (would point codex at a non-existent directory).
    });
    expect(plan.command).toBe('codex');
    expect(plan.args.slice(0, 3)).toEqual(['exec', '--json', '--skip-git-repo-check']);
    expect(plan.args).not.toContain('--cd');
    // Prompt should be the last arg
    expect(plan.args[plan.args.length - 1]).toBe(baseReq.prompt);
  });

  it('codex respects CHITIN_MODEL_CODEX env override (with whitespace trim)', () => {
    const orig = process.env.CHITIN_MODEL_CODEX;
    try {
      process.env.CHITIN_MODEL_CODEX = '  gpt-5.5  ';  // whitespace
      const plan = planInvocation({
        ...baseReq,
        allowed_drivers: ['codex'],
      });
      const idx = plan.args.indexOf('-m');
      expect(idx).toBeGreaterThan(-1);
      expect(plan.args[idx + 1]).toBe('gpt-5.5');
    } finally {
      if (orig === undefined) delete process.env.CHITIN_MODEL_CODEX;
      else process.env.CHITIN_MODEL_CODEX = orig;
    }
  });

  it('codex ignores whitespace-only CHITIN_MODEL_CODEX (no -m emitted)', () => {
    const orig = process.env.CHITIN_MODEL_CODEX;
    try {
      process.env.CHITIN_MODEL_CODEX = '   ';
      const plan = planInvocation({
        ...baseReq,
        allowed_drivers: ['codex'],
      });
      expect(plan.args).not.toContain('-m');
    } finally {
      if (orig === undefined) delete process.env.CHITIN_MODEL_CODEX;
      else process.env.CHITIN_MODEL_CODEX = orig;
    }
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

  it('openclaw-* drivers include --include-hook-events for chain visibility', () => {
    for (const driver of ['openclaw-glm-flash', 'openclaw-glm-cloud', 'openclaw-deepseek'] as const) {
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
    'CHITIN_AGENT_OPENCLAW_GLM_FLASH',
    'CHITIN_AGENT_OPENCLAW_GLM_CLOUD',
    'CHITIN_AGENT_OPENCLAW_DEEPSEEK',
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

  it('routes each openclaw-* driver to its dedicated agent by default', () => {
    expect(resolveAgent('openclaw-glm-flash')).toBe('glm-flash-agent');
    expect(resolveAgent('openclaw-glm-cloud')).toBe('glm-agent');
    expect(resolveAgent('openclaw-deepseek')).toBe('deepseek-agent');
  });

  it('falls back to main for any driver not in the map', () => {
    // copilot dispatches via the chitin-kernel shim (not openclaw), so
    // resolveAgent isn't called on it in normal use — but the fallback
    // contract still needs to be 'main' for any future driver added to
    // the schema before its mapping lands.
    expect(resolveAgent('copilot')).toBe('main');
  });

  it('honors CHITIN_AGENT_OPENCLAW_GLM_FLASH env override', () => {
    process.env.CHITIN_AGENT_OPENCLAW_GLM_FLASH = 'custom-flash-agent';
    expect(resolveAgent('openclaw-glm-flash')).toBe('custom-flash-agent');
  });

  it('treats whitespace-only env override as unset', () => {
    process.env.CHITIN_AGENT_OPENCLAW_GLM_FLASH = '   ';
    expect(resolveAgent('openclaw-glm-flash')).toBe('glm-flash-agent');
  });

  it('trims env override value', () => {
    process.env.CHITIN_AGENT_OPENCLAW_GLM_FLASH = '  trimmed-agent  ';
    expect(resolveAgent('openclaw-glm-flash')).toBe('trimmed-agent');
  });

  it('does not return the same agent for different openclaw-* drivers (no model collision)', () => {
    const agents = new Set([
      resolveAgent('openclaw-glm-flash'),
      resolveAgent('openclaw-glm-cloud'),
      resolveAgent('openclaw-deepseek'),
    ]);
    expect(agents.size).toBe(3);
  });

  it('exports DRIVER_AGENT_MAP with the expected default routes', () => {
    expect(DRIVER_AGENT_MAP['openclaw-glm-flash']).toBe('glm-flash-agent');
    expect(DRIVER_AGENT_MAP['openclaw-glm-cloud']).toBe('glm-agent');
    expect(DRIVER_AGENT_MAP['openclaw-deepseek']).toBe('deepseek-agent');
  });
});

describe('parseToolSummary', () => {
  const { parseToolSummary } = __test__;

  it('returns undefined when stdout has no toolSummary', () => {
    expect(parseToolSummary('')).toBeUndefined();
    expect(parseToolSummary('{"foo":1}\n{"bar":2}')).toBeUndefined();
  });

  it('extracts a flat NDJSON line containing toolSummary', () => {
    const stdout = `{"toolSummary":{"calls":3,"tools":["edit","search"],"failures":1}}\n`;
    expect(parseToolSummary(stdout)).toEqual({
      calls: 3,
      tools: ['edit', 'search'],
      failures: 1,
    });
  });

  it('extracts toolSummary from a wrapper object with NESTED braces (regression: previous regex dropped this)', () => {
    // The previous /\{[\s\S]*?\}/g extraction was non-greedy on `}`,
    // so for output like { ..., "toolSummary": { ... } } it stopped at
    // the first inner `}` and JSON.parse always failed. The brace-
    // balanced fallback path must handle this.
    const stdout = [
      'noise before the json',
      '{"event":"final","toolSummary":{"calls":7,"tools":["read","edit","bash"],"failures":2}}',
      'trailing log line',
    ].join('\n');
    expect(parseToolSummary(stdout)).toEqual({
      calls: 7,
      tools: ['read', 'edit', 'bash'],
      failures: 2,
    });
  });

  it('handles multi-line pretty-printed JSON via brace-balanced fallback', () => {
    const stdout = [
      'log: starting',
      '{',
      '  "event": "final",',
      '  "toolSummary": {',
      '    "calls": 2,',
      '    "tools": ["edit", "exec"],',
      '    "failures": 0',
      '  }',
      '}',
      'log: done',
    ].join('\n');
    expect(parseToolSummary(stdout)).toEqual({
      calls: 2,
      tools: ['edit', 'exec'],
      failures: 0,
    });
  });

  it('skips non-JSON lines and tolerates partial output', () => {
    const stdout = [
      'plain log line',
      '{"event":"telemetry","value":1}',
      '{"toolSummary":{"calls":0,"tools":[],"failures":0}}',
    ].join('\n');
    expect(parseToolSummary(stdout)).toEqual({
      calls: 0,
      tools: [],
      failures: 0,
    });
  });

  it('does not get confused by braces inside string literals', () => {
    // Failure mode: a naive bracket counter would treat `{` inside a
    // string as opening a new object and the toolSummary extraction
    // would slice mid-string.
    const stdout = `{"event":"oops","note":"path was {odd}","toolSummary":{"calls":1,"tools":["x"],"failures":0}}`;
    expect(parseToolSummary(stdout)).toEqual({
      calls: 1,
      tools: ['x'],
      failures: 0,
    });
  });
});

// ─── executeRequestWorkflow.name === WORKFLOW_NAME ─────────────────────────

import { WORKFLOW_NAME } from '../src/submit';
import type { executeRequestWorkflow } from '../src/submit';

describe('WORKFLOW_NAME matches executeRequestWorkflow.name', () => {
  it('should match the actual workflow function name', () => {
    expect((executeRequestWorkflow as any).name).toBe(WORKFLOW_NAME);
  });
});

// ─── isWorkerOwnedPath ─────────────────────────────────────────────────────
//
// Cleanup-cleanup-rmSync gate. The 2026-05-04 cwd-rm incident (#280)
// established the invariant: never rm a path the worker doesn't
// unambiguously own. This test pins the contract so a future refactor
// can't reintroduce the trap.

describe('isWorkerOwnedPath', () => {
  const repoRoot = '/home/red/workspace/chitin';
  const swarmRoot = resolve(homedir(), '.cache/chitin/swarm-worktrees');

  it('returns true for paths strictly under tmpdir()', () => {
    expect(isWorkerOwnedPath('/tmp/chitin-worker-abc-xyz', repoRoot)).toBe(true);
  });

  it('returns true for paths strictly under SWARM_WORKTREES_ROOT', () => {
    expect(isWorkerOwnedPath(`${swarmRoot}/wf-test-001`, repoRoot)).toBe(true);
  });

  it('returns false for repoRoot itself (reviewer mode)', () => {
    // Reviewer dispatch sets workDir = repoRoot so `gh pr diff` works.
    // The cleanup must never rm this — that's the #280 incident.
    expect(isWorkerOwnedPath(repoRoot, repoRoot)).toBe(false);
  });

  it('returns false for sibling paths that share a prefix with tmpdir()', () => {
    // The bug Copilot caught: raw startsWith('/tmp') would also match
    // '/tmp-backup'. Strictly-under check rejects sibling-prefix paths.
    expect(isWorkerOwnedPath('/tmp-backup/something', repoRoot)).toBe(false);
  });

  it('returns false for sibling paths that share a prefix with SWARM_WORKTREES_ROOT', () => {
    expect(isWorkerOwnedPath(`${swarmRoot}-old/wf-x`, repoRoot)).toBe(false);
  });

  it('returns false for arbitrary paths outside both roots', () => {
    expect(isWorkerOwnedPath('/etc', repoRoot)).toBe(false);
    expect(isWorkerOwnedPath('/home/red/workspace', repoRoot)).toBe(false);
    expect(isWorkerOwnedPath('/var/log/chitin.log', repoRoot)).toBe(false);
  });

  it('returns false when repoRoot itself is under tmpdir() (test rig edge case)', () => {
    // CI / test environments sometimes check out the repo into a tempdir.
    // The allowlist would normally match (path is under tmpdir), but the
    // explicit repoRoot guard short-circuits to false. Reviewer mode is
    // protected even on unusual rigs.
    const repoUnderTmp = '/tmp/chitin-checkout';
    expect(isWorkerOwnedPath(repoUnderTmp, repoUnderTmp)).toBe(false);
  });

  it('returns false for the empty path', () => {
    // resolve('') returns cwd, which is unlikely to be under tmpdir or
    // swarmRoot — but pin the behaviour so a regression is loud.
    expect(isWorkerOwnedPath('', repoRoot)).toBe(false);
  });
});
