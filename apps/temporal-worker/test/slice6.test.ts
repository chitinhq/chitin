// Slice 6 unit tests: tier-based model routing (6c), claude-code worktree
// settings injection (6a), per-workflow openclaw state remap (6b).
//
// Integration tests (6 — three-driver smoke against the real swarm) live
// in the slice 6 PR description as observation records, not vitest.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, rmSync, readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { ExecutionRequest, Tier } from '@chitin/contracts';
import { __test__ } from '../src/activity.ts';

const {
  planInvocation,
  resolveClaudeModel,
  resolveCopilotModel,
  writeWorktreeClaudeSettings,
  provisionOpenclawState,
  CLAUDE_TIER_MODEL,
  COPILOT_TIER_MODEL,
} = __test__;

const baseReq: ExecutionRequest = {
  schema_version: '1',
  workflow_id: 'wf-slice6-test',
  run_id: 'wf-slice6-test-attempt-1',
  repo: 'chitinhq/chitin',
  task_class: 'refactor',
  risk_level: 'low',
  allowed_drivers: ['claude-code-headless'],
  network_policy: 'allowlist',
  write_policy: 'worktree',
  bounds: { max_tool_calls: 50, max_cost_usd: 0, wall_timeout_s: 600 },
  prompt: 'do the thing',
};

// ─── 6c: tier → model resolution ──────────────────────────────────────────

describe('slice 6c — tier → model resolution', () => {
  const envKeys = [
    'CHITIN_MODEL_CLAUDE_CODE_HEADLESS_T0',
    'CHITIN_MODEL_CLAUDE_CODE_HEADLESS_T4',
    'CHITIN_MODEL_COPILOT_T0',
    'CHITIN_MODEL_COPILOT_T4',
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

  it('resolveClaudeModel returns null when tier is undefined', () => {
    expect(resolveClaudeModel(undefined)).toBeNull();
  });

  it('resolveClaudeModel maps T0/T1 to haiku and T4 to opus by default', () => {
    expect(resolveClaudeModel('T0' as Tier)).toBe('claude-haiku-4-5');
    expect(resolveClaudeModel('T1' as Tier)).toBe('claude-haiku-4-5');
    expect(resolveClaudeModel('T2' as Tier)).toBe('claude-sonnet-4-6');
    expect(resolveClaudeModel('T3' as Tier)).toBe('claude-sonnet-4-6');
    expect(resolveClaudeModel('T4' as Tier)).toBe('claude-opus-4-7');
  });

  it('resolveClaudeModel honors per-tier env override', () => {
    process.env.CHITIN_MODEL_CLAUDE_CODE_HEADLESS_T0 = 'claude-tiny-experimental';
    expect(resolveClaudeModel('T0' as Tier)).toBe('claude-tiny-experimental');
    expect(resolveClaudeModel('T4' as Tier)).toBe('claude-opus-4-7'); // unaffected
  });

  it('resolveCopilotModel maps T0/T1 to free gpt-5-mini and T2-T4 to haiku-4-5 (cheap multipliers)', () => {
    // 2026-05-04 reshuffle: Copilot bulk tier defaults bound by premium-
    // request multipliers measured by the operator. gpt-5-mini = 0× free
    // unlimited; haiku-4-5 = 0.33× cheap. Sonnet (1×) and Opus (3×) are
    // never default — operator opts in via env override only.
    expect(resolveCopilotModel('T0' as Tier)).toBe('gpt-5-mini');
    expect(resolveCopilotModel('T1' as Tier)).toBe('gpt-5-mini');
    expect(resolveCopilotModel('T2' as Tier)).toBe('claude-haiku-4-5');
    expect(resolveCopilotModel('T3' as Tier)).toBe('claude-haiku-4-5');
    expect(resolveCopilotModel('T4' as Tier)).toBe('claude-haiku-4-5');
  });

  it('resolveCopilotModel honors per-tier env override', () => {
    process.env.CHITIN_MODEL_COPILOT_T4 = 'gpt-5.4-experimental';
    expect(resolveCopilotModel('T4' as Tier)).toBe('gpt-5.4-experimental');
  });

  it('CLAUDE/COPILOT_TIER_MODEL maps cover all 5 tiers', () => {
    for (const t of ['T0', 'T1', 'T2', 'T3', 'T4'] as const) {
      expect(CLAUDE_TIER_MODEL[t]).toBeTruthy();
      expect(COPILOT_TIER_MODEL[t]).toBeTruthy();
    }
  });
});

// ─── 6c: planInvocation threads --model into the spawn args ───────────────

describe('slice 6c — planInvocation tier wiring', () => {
  it('claude-code-headless without tier omits --model (driver default)', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['claude-code-headless'] });
    expect(plan.args).not.toContain('--model');
  });

  it('claude-code-headless with tier appends --model with the right id', () => {
    const planT0 = planInvocation({ ...baseReq, allowed_drivers: ['claude-code-headless'], tier: 'T0' as Tier });
    expect(planT0.args).toContain('--model');
    expect(planT0.args[planT0.args.indexOf('--model') + 1]).toBe('claude-haiku-4-5');

    const planT4 = planInvocation({ ...baseReq, allowed_drivers: ['claude-code-headless'], tier: 'T4' as Tier });
    expect(planT4.args[planT4.args.indexOf('--model') + 1]).toBe('claude-opus-4-7');
  });

  it('copilot without tier omits --model (Copilot CLI picks its own default)', () => {
    const plan = planInvocation({ ...baseReq, allowed_drivers: ['copilot'] });
    expect(plan.args).not.toContain('--model');
  });

  it('copilot with tier appends --model into the chitin-kernel shim args', () => {
    const planT2 = planInvocation({ ...baseReq, allowed_drivers: ['copilot'], tier: 'T2' as Tier });
    expect(planT2.args).toContain('--model');
    // 2026-05-04 reshuffle: T2 Copilot model defaults to claude-haiku-4-5
    // (0.33× premium multiplier — the cheap-bulk reviewer/programmer tier).
    // Pre-2026-05-04 this was claude-sonnet-4.6 (1×) which burned the Pro
    // premium-request budget too aggressively for the bulk tier.
    expect(planT2.args[planT2.args.indexOf('--model') + 1]).toBe('claude-haiku-4-5');
  });
});

// ─── 6a: claude-code worktree settings.json ───────────────────────────────

describe('slice 6a — writeWorktreeClaudeSettings', () => {
  let scratch: string;
  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'chitin-6a-test-'));
  });
  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('creates .claude/settings.json with a PreToolUse router-hook entrypoint', () => {
    writeWorktreeClaudeSettings(scratch);
    const settingsPath = join(scratch, '.claude/settings.json');
    expect(existsSync(settingsPath)).toBe(true);
    const settings = JSON.parse(readFileSync(settingsPath, 'utf8'));
    const hooks = settings.hooks?.PreToolUse;
    expect(Array.isArray(hooks)).toBe(true);
    expect(hooks.length).toBeGreaterThan(0);
    const cmd = hooks[0]?.hooks?.[0]?.command;
    // The hook command points at chitin-router-hook (which has a hot-path
    // bypass that exec's chitin-kernel directly when the operator hasn't
    // touched ~/.chitin/router-enabled). The command therefore contains
    // chitin-router-hook in default install; older builds pointed at
    // chitin-kernel directly. Either path is valid; assert on the
    // structural shape (hook present + agent param) instead of binary name.
    expect(cmd).toMatch(/chitin-(router-hook|kernel)/);
    expect(cmd).toContain('--agent=claude-code');
  });

  it('uses matcher: "" so the hook applies to all tool calls', () => {
    writeWorktreeClaudeSettings(scratch);
    const settings = JSON.parse(readFileSync(join(scratch, '.claude/settings.json'), 'utf8'));
    expect(settings.hooks.PreToolUse[0].matcher).toBe('');
  });

  it('overwrites an existing settings.json (idempotent for re-runs)', () => {
    mkdirSync(join(scratch, '.claude'), { recursive: true });
    writeFileSync(join(scratch, '.claude/settings.json'), '{"stale": true}');
    writeWorktreeClaudeSettings(scratch);
    const settings = JSON.parse(readFileSync(join(scratch, '.claude/settings.json'), 'utf8'));
    expect(settings.stale).toBeUndefined();
    expect(settings.hooks).toBeDefined();
  });
});

// ─── 6b: provisionOpenclawState (per-workflow STATE_DIR with workspace remap) ─

describe('slice 6b — provisionOpenclawState', () => {
  let scratch: string;
  let savedHome: string | undefined;
  let fakeHome: string;
  let fakeOpenclaw: string;

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'chitin-6b-test-'));
    fakeHome = mkdtempSync(join(tmpdir(), 'chitin-6b-home-'));
    fakeOpenclaw = join(fakeHome, '.openclaw');
    mkdirSync(fakeOpenclaw, { recursive: true });
    // Source openclaw config with a couple of agents.
    writeFileSync(
      join(fakeOpenclaw, 'openclaw.json'),
      JSON.stringify(
        {
          agents: {
            list: [
              { id: 'main' },
              { id: 'qwen-agent', workspace: '/some/old/qwen-workspace' },
              { id: 'glm-agent', workspace: '/some/old/glm-workspace' },
            ],
          },
          plugins: { allow: ['chitin-governance'] },
        },
        null,
        2,
      ),
    );
    // Other state-dir entries that should get symlinked.
    mkdirSync(join(fakeOpenclaw, 'agents'));
    writeFileSync(join(fakeOpenclaw, 'extensions'), 'placeholder file\n');
    savedHome = process.env.HOME;
    process.env.HOME = fakeHome;
  });

  afterEach(() => {
    if (savedHome === undefined) delete process.env.HOME;
    else process.env.HOME = savedHome;
    rmSync(scratch, { recursive: true, force: true });
    rmSync(fakeHome, { recursive: true, force: true });
  });

  it('returns null when no openclaw.json exists in $HOME/.openclaw', () => {
    rmSync(join(fakeOpenclaw, 'openclaw.json'));
    const req = { ...baseReq, allowed_drivers: ['openclaw-glm-flash'] as const };
    const result = provisionOpenclawState(req, scratch, 'qwen-agent');
    expect(result).toBeNull();
  });

  it('writes a state dir with openclaw.json that remaps the named agent workspace', () => {
    const req = { ...baseReq, allowed_drivers: ['openclaw-glm-flash'] as const };
    const result = provisionOpenclawState(req, scratch, 'qwen-agent');
    expect(result).not.toBeNull();
    expect(result!.envOverride.OPENCLAW_STATE_DIR).toBe(result!.stateDir);
    const cfg = JSON.parse(readFileSync(join(result!.stateDir, 'openclaw.json'), 'utf8'));
    const qwen = cfg.agents.list.find((a: { id: string }) => a.id === 'qwen-agent');
    expect(qwen.workspace).toBe(scratch);
    // Other agents are unchanged
    const glm = cfg.agents.list.find((a: { id: string }) => a.id === 'glm-agent');
    expect(glm.workspace).toBe('/some/old/glm-workspace');
    rmSync(result!.stateDir, { recursive: true, force: true });
  });

  it('symlinks state-dir contents (excluding openclaw.json) so plugins/agents are reachable', () => {
    const req = { ...baseReq, allowed_drivers: ['openclaw-glm-flash'] as const };
    const result = provisionOpenclawState(req, scratch, 'qwen-agent');
    expect(result).not.toBeNull();
    expect(existsSync(join(result!.stateDir, 'agents'))).toBe(true);
    expect(existsSync(join(result!.stateDir, 'extensions'))).toBe(true);
    expect(existsSync(join(result!.stateDir, 'openclaw.json'))).toBe(true);
    rmSync(result!.stateDir, { recursive: true, force: true });
  });

  it("does not mutate the user's source openclaw.json", () => {
    const req = { ...baseReq, allowed_drivers: ['openclaw-glm-flash'] as const };
    const before = readFileSync(join(fakeOpenclaw, 'openclaw.json'), 'utf8');
    const result = provisionOpenclawState(req, scratch, 'qwen-agent');
    const after = readFileSync(join(fakeOpenclaw, 'openclaw.json'), 'utf8');
    expect(after).toBe(before);
    rmSync(result!.stateDir, { recursive: true, force: true });
  });
});
