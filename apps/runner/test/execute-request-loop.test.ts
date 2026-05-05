import { describe, expect, it } from 'vitest';
import { runWithEscalation } from '../src/execute-request.ts';
import type { ExecutionRequest, Tier } from '@chitin/contracts';
import type { ActivityResult } from '../src/activity-types.ts';

function makeReq(overrides: Partial<ExecutionRequest> = {}): ExecutionRequest {
  return {
    schema_version: '1',
    workflow_id: 'wf-test',
    run_id: 'wf-test-attempt-1',
    repo: 'chitinhq/chitin',
    task_class: 'exploration',
    risk_level: 'low',
    allowed_drivers: ['copilot'],
    network_policy: 'allowlist',
    write_policy: 'none',
    bounds: { max_tool_calls: 5, max_cost_usd: 0, wall_timeout_s: 60 },
    prompt: 'do the thing',
    role: 'programmer',
    tier: 'T0' as Tier,
    ...overrides,
  } as ExecutionRequest;
}

function ok(): ActivityResult {
  return { exit_code: 0, stdout_tail: 'done', stderr_tail: '', duration_ms: 100 };
}

function escalate(from_tier: string, nudge = 'needs-more-juice'): ActivityResult {
  return {
    exit_code: 0, stdout_tail: 'escalating', stderr_tail: '', duration_ms: 100,
    escalation_requested: { from_tier, advisor_nudge: nudge },
  };
}

describe('runWithEscalation', () => {
  it('returns terminal on first success without escalating', async () => {
    const calls: ExecutionRequest[] = [];
    const r = await runWithEscalation(makeReq(), {
      runFn: async (req) => { calls.push(req); return ok(); },
      log: () => {},
    });
    expect(r.exit_code).toBe(0);
    expect(r.escalation_exhausted).toBeUndefined();
    expect(calls).toHaveLength(1);
    expect(calls[0].tier).toBe('T0');
  });

  it('bumps tier on escalation_requested and re-runs', async () => {
    const calls: ExecutionRequest[] = [];
    let n = 0;
    const r = await runWithEscalation(makeReq(), {
      runFn: async (req) => {
        calls.push(req);
        return ++n === 1 ? escalate('T0') : ok();
      },
      log: () => {},
    });
    expect(r.exit_code).toBe(0);
    expect(calls).toHaveLength(2);
    expect(calls[0].tier).toBe('T0');
    expect(calls[1].tier).toBe('T1');
    // Escalation context threaded in on attempt 2
    expect(calls[1].escalation_context).toEqual({
      from_tier: 'T0', advisor_nudge: 'needs-more-juice', attempt: 2,
    });
    // Prompt prefix injected
    expect(calls[1].prompt).toMatch(/MID-TASK CONTINUATION/);
    expect(calls[1].prompt).toMatch(/prior_tier: T0/);
  });

  it('cascades T0 → T1 → T2 → T3 → T4 across 5 attempts', async () => {
    const calls: ExecutionRequest[] = [];
    let n = 0;
    const r = await runWithEscalation(makeReq(), {
      runFn: async (req) => {
        calls.push(req);
        n++;
        if (n < 5) return escalate(req.tier ?? 'T0');
        return ok();
      },
      log: () => {},
      maxAttempts: 5,
    });
    expect(r.exit_code).toBe(0);
    expect(calls.map((c) => c.tier)).toEqual(['T0', 'T1', 'T2', 'T3', 'T4']);
  });

  it('switches to advisor role when T4 escalates', async () => {
    const calls: ExecutionRequest[] = [];
    let n = 0;
    const r = await runWithEscalation(makeReq({ tier: 'T4' as Tier }), {
      runFn: async (req) => {
        calls.push(req);
        n++;
        if (n === 1) return escalate('T4', 'opus stuck on a foundational gap');
        return ok();
      },
      log: () => {},
    });
    expect(r.exit_code).toBe(0);
    expect(calls).toHaveLength(2);
    expect(calls[0].tier).toBe('T4');
    expect(calls[0].role).toBe('programmer');
    expect(calls[1].tier).toBe('T4');
    expect(calls[1].role).toBe('advisor');
  });

  it('marks escalation_exhausted when T4 advisor still escalates', async () => {
    const calls: ExecutionRequest[] = [];
    const r = await runWithEscalation(makeReq({ tier: 'T4' as Tier, role: 'advisor' }), {
      runFn: async (req) => { calls.push(req); return escalate('T4'); },
      log: () => {},
    });
    expect(r.escalation_exhausted).toBe(true);
    expect(calls).toHaveLength(1);
  });

  it('marks escalation_exhausted when attempt cap is hit while still escalating', async () => {
    const calls: ExecutionRequest[] = [];
    const r = await runWithEscalation(makeReq(), {
      runFn: async (req) => { calls.push(req); return escalate(req.tier ?? 'T0'); },
      log: () => {},
      maxAttempts: 3,
    });
    expect(r.escalation_exhausted).toBe(true);
    expect(calls).toHaveLength(3);
    expect(calls.map((c) => c.tier)).toEqual(['T0', 'T1', 'T2']);
  });

  it('preserves the original prompt body across continuations', async () => {
    const original = 'implement the foo function';
    const calls: ExecutionRequest[] = [];
    let n = 0;
    await runWithEscalation(makeReq({ prompt: original }), {
      runFn: async (req) => {
        calls.push(req);
        return ++n === 1 ? escalate('T0') : ok();
      },
      log: () => {},
    });
    expect(calls[0].prompt).toBe(original);
    expect(calls[1].prompt).toContain(original);
    expect(calls[1].prompt).toMatch(/^# MID-TASK CONTINUATION/);
  });

  it('surfaces the FINAL ActivityResult (not the first one)', async () => {
    let n = 0;
    const r = await runWithEscalation(makeReq(), {
      runFn: async () => {
        n++;
        if (n === 1) return escalate('T0');
        return { exit_code: 0, stdout_tail: 'final-attempt-content', stderr_tail: '', duration_ms: 200 };
      },
      log: () => {},
    });
    expect(r.stdout_tail).toBe('final-attempt-content');
  });
});
