import type { Item, TaskItem, EventItem, WindowPref, Telemetry, ItemDecision } from './schema.js';

export interface RankContext {
  now: string;
  open_items: Item[];
  scheduled_events: EventItem[];
  working_hours?: { start_hhmm: string; end_hhmm: string }[];
  window_clock_map: Record<WindowPref, { start_hhmm: string; end_hhmm: string }[]>;
  consumer: 'personal' | 'swarm';
}

export interface SlotAssignment {
  item_id: string;
  start: string;
  end: string;
  rationale: string;
}

export interface RankResult {
  ordered: Item[];
  slots: SlotAssignment[];
  telemetry: Telemetry;
}

// Invariant: task items with deadlines appear before tasks without deadlines
// in ordered[]. Within deadline tasks, closer deadline = earlier position.
// Within non-deadline tasks, lower priority number = earlier position.
// Tie-breaker: lexicographic id (stable, deterministic).

function parseHHMM(date: Date, hhmm: string): number {
  const [h, m] = hhmm.split(':').map(Number);
  const d = new Date(date);
  d.setHours(h, m, 0, 0);
  return d.getTime();
}

function urgencyKey(item: TaskItem, nowMs: number): [number, number, string] {
  if (item.deadline) {
    const deadlineMs = new Date(item.deadline).getTime();
    return [0, deadlineMs - nowMs, item.id];
  }
  const priority = item.priority ?? 3;
  return [1, priority, item.id];
}

function compareTaskUrgency(a: TaskItem, b: TaskItem, nowMs: number): number {
  const [bucketA, scoreA, idA] = urgencyKey(a, nowMs);
  const [bucketB, scoreB, idB] = urgencyKey(b, nowMs);
  if (bucketA !== bucketB) return bucketA - bucketB;
  if (scoreA !== scoreB) return scoreA - scoreB;
  return idA < idB ? -1 : idA > idB ? 1 : 0;
}

export function rank(ctx: RankContext): RankResult {
  const nowMs = new Date(ctx.now).getTime();
  const nowDate = new Date(ctx.now);

  const tasks = ctx.open_items.filter((i): i is TaskItem => i.item_type === 'task');
  const others = ctx.open_items.filter((i) => i.item_type !== 'task');

  const sorted = [...tasks].sort((a, b) => compareTaskUrgency(a, b, nowMs));
  const ordered: Item[] = [...sorted, ...others];

  const occupied: Array<{ start: number; end: number }> = ctx.scheduled_events.map((e) => ({
    start: new Date(e.start).getTime(),
    end: new Date(e.start).getTime() + (e.duration_min ?? 60) * 60_000,
  }));

  const slots: SlotAssignment[] = [];
  const decisions: ItemDecision[] = [];

  for (const task of sorted) {
    const pref = task.window_pref ?? 'any';
    const estMs = (task.est_min ?? 30) * 60_000;

    const windowRanges: { start_hhmm: string; end_hhmm: string }[] =
      pref === 'any'
        ? Object.values(ctx.window_clock_map).flat()
        : (ctx.window_clock_map[pref] ?? []);

    let assigned = false;

    for (const range of windowRanges) {
      const windowStart = parseHHMM(nowDate, range.start_hhmm);
      const windowEnd = parseHHMM(nowDate, range.end_hhmm);

      let candidateStart = Math.max(windowStart, nowMs);

      while (candidateStart + estMs <= windowEnd) {
        const candidateEnd = candidateStart + estMs;
        const blocker = occupied.find(
          (o) => candidateStart < o.end && candidateEnd > o.start,
        );
        if (!blocker) {
          const startIso = new Date(candidateStart).toISOString();
          const endIso = new Date(candidateEnd).toISOString();
          slots.push({
            item_id: task.id,
            start: startIso,
            end: endIso,
            rationale: `window:${pref}`,
          });
          occupied.push({ start: candidateStart, end: candidateEnd });
          decisions.push({
            event_type: 'item_decision',
            item_id: task.id,
            rationale: `slotted:${pref}:${startIso}`,
            scheduled_start: startIso,
            ts: ctx.now,
          });
          assigned = true;
          break;
        }
        candidateStart = blocker.end;
      }
      if (assigned) break;
    }

    if (!assigned) {
      decisions.push({
        event_type: 'item_decision',
        item_id: task.id,
        rationale: 'unslotted:no-window',
        ts: ctx.now,
      });
    }
  }

  const telemetry: Telemetry = {
    consumer: ctx.consumer,
    item_decisions: decisions,
  };

  return { ordered, slots, telemetry };
}
