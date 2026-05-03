import { describe, expect, it } from 'vitest';
import { scoreBlastRadius } from '../../src/router/heuristics/blast-radius.ts';
import {
  detectFloundering,
  type ChainEventLite,
} from '../../src/router/heuristics/floundering.ts';
import type { HookInput } from '../../src/router/types.ts';

const baseInput: HookInput = {
  hook_event_name: 'PreToolUse',
  tool_name: 'Read',
  tool_input: {},
  cwd: '/tmp',
};

describe('scoreBlastRadius', () => {
  it('scores read-only tools as 0', () => {
    const score = scoreBlastRadius({ ...baseInput, tool_name: 'Read' });
    expect(score.score).toBe(0);
    expect(score.fired).toBe(false);
    expect(score.reason).toBe('read-only-tool');
  });

  it('scores rm -rf as max (1.0 reversibility=0 + scope=1)', () => {
    const score = scoreBlastRadius({
      ...baseInput,
      tool_name: 'Bash',
      tool_input: { command: 'rm -rf /tmp/foo' },
    });
    // (1-0)*0.4 + 1.0*0.25 + 0*0.2 + 0*0.15 = 0.65
    expect(score.score).toBeCloseTo(0.65, 2);
    expect(score.fired).toBe(true);
    expect(score.reason).toBe('recursive-delete');
  });

  it('scores force-push as high', () => {
    const score = scoreBlastRadius({
      ...baseInput,
      tool_name: 'Bash',
      tool_input: { command: 'git push --force origin main' },
    });
    // (1-0)*0.4 + 0.5*0.25 + 0.9*0.2 + 0.7*0.15 = 0.4 + 0.125 + 0.18 + 0.105 = 0.81
    expect(score.score).toBeCloseTo(0.81, 2);
    expect(score.fired).toBe(true);
    expect(score.reason).toBe('force-push');
  });

  it('scores governance-config writes as elevated (reason flagged for telemetry; threshold-fired depends on threshold)', () => {
    // Score = (1-0.6)*0.4 + 0.8*0.25 + 0 + 0 = 0.36 — below default 0.6 threshold.
    // The kernel's `no-governance-self-modification` rule already DENIES these
    // at the deterministic layer, so the heuristic doesn't need to fire.
    // What matters is the REASON tag for telemetry / advisor context.
    const score = scoreBlastRadius({
      ...baseInput,
      tool_name: 'Edit',
      tool_input: { file_path: '/repo/chitin.yaml' },
    });
    expect(score.reason).toBe('governance-config-write');
    expect(score.score).toBeGreaterThan(0.3);
    // At a lower threshold (0.3), it WOULD fire — operator can opt-in
    const lowThresh = scoreBlastRadius(
      {
        ...baseInput,
        tool_name: 'Edit',
        tool_input: { file_path: '/repo/chitin.yaml' },
      },
      0.3,
    );
    expect(lowThresh.fired).toBe(true);
  });

  it('scores npm publish as max blast', () => {
    const score = scoreBlastRadius({
      ...baseInput,
      tool_name: 'Bash',
      tool_input: { command: 'pnpm publish --access public' },
    });
    // (1-0)*0.4 + 0.5*0.25 + 1.0*0.2 + 1.0*0.15 = 0.4 + 0.125 + 0.2 + 0.15 = 0.875
    expect(score.score).toBeCloseTo(0.875, 2);
    expect(score.fired).toBe(true);
    expect(score.reason).toBe('package-publish');
  });

  it('respects custom threshold', () => {
    const lowThresh = scoreBlastRadius(
      { ...baseInput, tool_name: 'Bash', tool_input: { command: 'echo hi' } },
      0.1,
    );
    expect(lowThresh.fired).toBe(true); // generic-shell-exec scores 0.275 > 0.1
    const highThresh = scoreBlastRadius(
      { ...baseInput, tool_name: 'Bash', tool_input: { command: 'echo hi' } },
      0.9,
    );
    expect(highThresh.fired).toBe(false);
  });

  it('handles unknown tools with moderate caution', () => {
    const score = scoreBlastRadius({ ...baseInput, tool_name: 'TotallyMadeUp' });
    expect(score.reason).toBe('unknown-tool:TotallyMadeUp');
    expect(score.score).toBeGreaterThan(0);
    expect(score.score).toBeLessThan(0.6);
  });
});

describe('detectFloundering', () => {
  const thresholds = { max_loop_count: 3, max_stall_seconds: 600 };

  it('returns no-signals on empty events', () => {
    const result = detectFloundering([], thresholds);
    expect(result.fired).toBe(false);
    expect(result.reason).toBe('no-signals');
  });

  it('detects looping tool calls (3 same-target in a row)', () => {
    const event = (target: string): ChainEventLite => ({
      ts: '2026-05-03T20:00:00Z',
      event_type: 'decision',
      payload: { tool_name: 'Bash', action_target: target, decision: 'allow' },
    });
    const result = detectFloundering(
      [event('rm /tmp/x'), event('rm /tmp/x'), event('rm /tmp/x')],
      thresholds,
    );
    expect(result.fired).toBe(true);
    expect(result.score).toBe(1.0);
    expect(result.reason).toMatch(/looping-tool-call/);
  });

  it('does not flag varying tool calls', () => {
    const event = (target: string): ChainEventLite => ({
      ts: '2026-05-03T20:00:00Z',
      event_type: 'decision',
      payload: { tool_name: 'Bash', action_target: target, decision: 'allow' },
    });
    // Pass an explicit `now` close to event ts so stall doesn't fire instead.
    const result = detectFloundering(
      [event('cmd a'), event('cmd b'), event('cmd c')],
      thresholds,
      new Date('2026-05-03T20:00:30Z'),
    );
    expect(result.fired).toBe(false);
  });

  it('detects stall (no writes in window)', () => {
    const events: ChainEventLite[] = [
      {
        ts: '2026-05-03T20:00:00Z',
        event_type: 'decision',
        payload: { tool_name: 'Read', action_type: 'file.read', decision: 'allow' },
      },
    ];
    // now is 700s after the only event; max_stall_seconds=600
    const result = detectFloundering(events, thresholds, new Date('2026-05-03T20:11:40Z'));
    expect(result.fired).toBe(true);
    expect(result.reason).toMatch(/no-writes/);
  });

  it('detects denial cascade (4+ denials in last 5)', () => {
    // Vary the action_target so loop detector doesn't fire first;
    // we want denial-cascade to be the reported signal.
    const denial = (target: string): ChainEventLite => ({
      ts: '2026-05-03T20:00:00Z',
      event_type: 'decision',
      payload: { tool_name: 'Bash', action_target: target, decision: 'deny', rule_id: 'no-rm-recursive' },
    });
    const allow: ChainEventLite = {
      ts: '2026-05-03T20:00:00Z',
      event_type: 'decision',
      payload: { tool_name: 'Read', action_target: '/tmp/x', decision: 'allow' },
    };
    const result = detectFloundering(
      [allow, denial('cmd1'), denial('cmd2'), denial('cmd3'), denial('cmd4')],
      thresholds,
      new Date('2026-05-03T20:00:30Z'),
    );
    expect(result.fired).toBe(true);
    expect(result.reason).toMatch(/denial-cascade/);
  });

  it('does not flag mostly-allowed sessions with varying targets', () => {
    const allow = (target: string): ChainEventLite => ({
      ts: '2026-05-03T20:00:00Z',
      event_type: 'decision',
      payload: { tool_name: 'Read', action_target: target, decision: 'allow' },
    });
    const result = detectFloundering(
      [allow('a'), allow('b'), allow('c'), allow('d'), allow('e')],
      thresholds,
      new Date('2026-05-03T20:00:30Z'),
    );
    expect(result.fired).toBe(false);
  });
});
