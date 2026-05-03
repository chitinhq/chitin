import { describe, it, expect } from 'vitest';
import { rank } from '../src/rank.js';
import type { RankContext, TaskItem, EventItem } from '../src/index.js';

const WINDOW_MAP: RankContext['window_clock_map'] = {
  morning:  [{ start_hhmm: '08:00', end_hhmm: '10:00' }],
  deep:     [{ start_hhmm: '10:00', end_hhmm: '13:00' }],
  shallow:  [{ start_hhmm: '13:00', end_hhmm: '15:00' }],
  evening:  [{ start_hhmm: '17:00', end_hhmm: '19:00' }],
  any:      [{ start_hhmm: '08:00', end_hhmm: '19:00' }],
};

function makeTask(overrides: Partial<TaskItem> & { id: string; title: string }): TaskItem {
  return {
    item_type: 'task',
    status: 'open',
    created_at: '2026-05-03T08:00:00.000Z',
    ...overrides,
  };
}

function makeEvent(id: string, start: string, duration_min = 60): EventItem {
  return {
    id,
    item_type: 'event',
    title: 'meeting',
    status: 'open',
    created_at: '2026-05-03T08:00:00.000Z',
    start,
    duration_min,
  };
}

describe('rank', () => {
  it('places deadline tasks before non-deadline tasks', () => {
    const tasks = [
      makeTask({ id: 'b', title: 'No deadline task' }),
      makeTask({ id: 'a', title: 'Deadline task', deadline: '2026-05-04T09:00:00.000Z' }),
    ];

    const ctx: RankContext = {
      now: '2026-05-03T08:00:00.000Z',
      open_items: tasks,
      scheduled_events: [],
      window_clock_map: WINDOW_MAP,
      consumer: 'personal',
    };

    const { ordered } = rank(ctx);
    expect(ordered[0].id).toBe('a');
    expect(ordered[1].id).toBe('b');
  });

  it('orders deadline tasks by deadline proximity ascending', () => {
    const tasks = [
      makeTask({ id: 'far', title: 'Far deadline', deadline: '2026-05-10T09:00:00.000Z' }),
      makeTask({ id: 'near', title: 'Near deadline', deadline: '2026-05-04T09:00:00.000Z' }),
    ];

    const ctx: RankContext = {
      now: '2026-05-03T08:00:00.000Z',
      open_items: tasks,
      scheduled_events: [],
      window_clock_map: WINDOW_MAP,
      consumer: 'personal',
    };

    const { ordered } = rank(ctx);
    expect(ordered[0].id).toBe('near');
    expect(ordered[1].id).toBe('far');
  });

  it('orders non-deadline tasks by priority then id', () => {
    const tasks = [
      makeTask({ id: 'c', title: 'Priority 3', priority: 3 }),
      makeTask({ id: 'a', title: 'Priority 1', priority: 1 }),
      makeTask({ id: 'b', title: 'Priority 1 second', priority: 1 }),
    ];

    const ctx: RankContext = {
      now: '2026-05-03T08:00:00.000Z',
      open_items: tasks,
      scheduled_events: [],
      window_clock_map: WINDOW_MAP,
      consumer: 'personal',
    };

    const { ordered } = rank(ctx);
    expect(ordered[0].id).toBe('a');
    expect(ordered[1].id).toBe('b');
    expect(ordered[2].id).toBe('c');
  });

  it('slots tasks into matching windows', () => {
    const tasks = [
      makeTask({ id: 't1', title: 'Morning task', window_pref: 'morning', est_min: 30 }),
    ];

    const ctx: RankContext = {
      now: '2026-05-03T07:00:00.000Z',
      open_items: tasks,
      scheduled_events: [],
      window_clock_map: WINDOW_MAP,
      consumer: 'personal',
    };

    const { slots } = rank(ctx);
    expect(slots).toHaveLength(1);
    expect(slots[0].item_id).toBe('t1');
    expect(slots[0].start).toContain('T08:00:');
  });

  it('skips occupied windows and slots in next gap', () => {
    const tasks = [
      makeTask({ id: 't1', title: 'Any task', window_pref: 'morning', est_min: 60 }),
    ];
    const events = [
      makeEvent('e1', '2026-05-03T08:00:00.000Z', 60),
    ];

    const ctx: RankContext = {
      now: '2026-05-03T07:00:00.000Z',
      open_items: tasks,
      scheduled_events: events,
      window_clock_map: WINDOW_MAP,
      consumer: 'personal',
    };

    const { slots } = rank(ctx);
    expect(slots).toHaveLength(1);
    expect(new Date(slots[0].start).getTime()).toBeGreaterThanOrEqual(
      new Date('2026-05-03T09:00:00.000Z').getTime(),
    );
  });

  it('records unslotted decision when no window available', () => {
    const tasks = [
      makeTask({ id: 't1', title: 'Evening task', window_pref: 'evening', est_min: 180 }),
    ];

    const ctx: RankContext = {
      now: '2026-05-03T07:00:00.000Z',
      open_items: tasks,
      scheduled_events: [],
      window_clock_map: WINDOW_MAP,
      consumer: 'personal',
    };

    const { slots, telemetry } = rank(ctx);
    expect(slots).toHaveLength(0);
    expect(telemetry.item_decisions[0].rationale).toBe('unslotted:no-window');
  });

  it('emits item_decision telemetry for each task', () => {
    const tasks = [
      makeTask({ id: 't1', title: 'Task 1', window_pref: 'morning', est_min: 30 }),
      makeTask({ id: 't2', title: 'Task 2', window_pref: 'deep', est_min: 60 }),
    ];

    const ctx: RankContext = {
      now: '2026-05-03T07:00:00.000Z',
      open_items: tasks,
      scheduled_events: [],
      window_clock_map: WINDOW_MAP,
      consumer: 'swarm',
    };

    const { telemetry } = rank(ctx);
    expect(telemetry.consumer).toBe('swarm');
    expect(telemetry.item_decisions).toHaveLength(2);
    expect(telemetry.item_decisions.every((d) => d.event_type === 'item_decision')).toBe(true);
  });

  it('returns empty results for empty input', () => {
    const ctx: RankContext = {
      now: '2026-05-03T07:00:00.000Z',
      open_items: [],
      scheduled_events: [],
      window_clock_map: WINDOW_MAP,
      consumer: 'personal',
    };

    const { ordered, slots, telemetry } = rank(ctx);
    expect(ordered).toHaveLength(0);
    expect(slots).toHaveLength(0);
    expect(telemetry.item_decisions).toHaveLength(0);
  });
});
