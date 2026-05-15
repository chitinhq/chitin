import { describe, expect, it } from 'vitest';
import { emptyDecisionSnapshot, reduceDecisionSnapshot } from '../src/decision-state';
import { parseDecisionEventLine } from '../src/decision-types';

function decisionLine(overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({
    event_type: 'decision',
    ts: '2026-05-14T12:00:00Z',
    labels: { agent: 'codex', driver: 'codex' },
    payload: {
      event_id: 'evt-1',
      decision: 'deny',
      rule_id: 'no-write',
      action_type: 'file.write',
      action_target: '/tmp/a',
      reason: 'blocked',
      ...overrides,
    },
  });
}

describe('parseDecisionEventLine', () => {
  it('parses v3 decision events from the chain', () => {
    const decision = parseDecisionEventLine(decisionLine());
    expect(decision).not.toBeNull();
    expect(decision?.actionType).toBe('file.write');
    expect(decision?.driver).toBe('codex');
  });

  it('parses max bounds decision envelopes without payload event_id', () => {
    const decision = parseDecisionEventLine(JSON.stringify({
      schema_version: '2',
      run_id: '550e8400-e29b-41d4-a716-446655441000',
      session_id: '550e8400-e29b-41d4-a716-446655441001',
      surface: 'codex',
      driver_identity: {
        user: 'red',
        machine_id: 'chimera-ant',
        machine_fingerprint: 'a'.repeat(64),
      },
      agent_instance_id: '550e8400-e29b-41d4-a716-446655441002',
      parent_agent_id: null,
      agent_fingerprint: 'd'.repeat(64),
      event_type: 'decision',
      chain_id: '550e8400-e29b-41d4-a716-446655441001',
      chain_type: 'session',
      parent_chain_id: null,
      seq: 7,
      prev_hash: 'b'.repeat(64),
      this_hash: 'c'.repeat(64),
      ts: '2026-05-14T12:00:00Z',
      labels: { agent: 'codex-worker', driver: 'codex' },
      payload: {
        decision: 'deny',
        rule_id: 'bounds:max_lines_changed',
        action_type: 'git.push',
        action_target: 'origin swarm/codex-ecfb2c7e',
        reason: 'max lines changed exceeded',
      },
    }));

    expect(decision).not.toBeNull();
    expect(decision?.eventId).toBe('c'.repeat(64));
    expect(decision?.ruleId).toBe('bounds:max_lines_changed');
    expect(decision?.actionType).toBe('git.push');
  });

  it('ignores non-decision events', () => {
    const decision = parseDecisionEventLine(JSON.stringify({ event_type: 'user_prompt' }));
    expect(decision).toBeNull();
  });
});

describe('reduceDecisionSnapshot', () => {
  it('tracks the last blocked action and sticky lockdown', () => {
    const first = parseDecisionEventLine(decisionLine());
    const second = parseDecisionEventLine(decisionLine({
      event_id: 'evt-2',
      rule_id: 'lockdown',
      escalation: 'lockdown',
      action_type: 'shell.exec',
      action_target: 'rm -rf /tmp',
    }));
    expect(first).not.toBeNull();
    expect(second).not.toBeNull();
    if (!first || !second) {
      return;
    }

    const afterFirst = reduceDecisionSnapshot(emptyDecisionSnapshot(), first);
    expect(afterFirst.lastBlocked?.eventId).toBe('evt-1');
    expect(afterFirst.lockdown).toBe(false);

    const afterSecond = reduceDecisionSnapshot(afterFirst, second);
    expect(afterSecond.lastBlocked?.eventId).toBe('evt-2');
    expect(afterSecond.lockdown).toBe(true);
    expect(afterSecond.recent).toHaveLength(2);
  });
});
