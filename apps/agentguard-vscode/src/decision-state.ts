import type { DecisionRecord, DecisionSnapshot } from './decision-types';

export function reduceDecisionSnapshot(
  current: DecisionSnapshot,
  next: DecisionRecord,
  limit = 50,
): DecisionSnapshot {
  const recent = [next, ...current.recent.filter((item) => item.eventId !== next.eventId)].slice(0, limit);
  const blocked = next.decision !== 'allow' ? next : current.lastBlocked;
  const lockdown = current.lockdown || next.ruleId === 'lockdown' || next.escalation === 'lockdown';
  return {
    recent,
    lastBlocked: blocked,
    lockdown,
  };
}

export function emptyDecisionSnapshot(): DecisionSnapshot {
  return {
    recent: [],
    lastBlocked: null,
    lockdown: false,
  };
}
