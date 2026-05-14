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
