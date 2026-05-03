import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import { resolveChitinDir } from '@chitin/contracts';
import { openStore, ingest, rank } from '@chitin/scheduler';
import type { RankContext, WindowPref } from '@chitin/scheduler';
import type { Command } from 'commander';

const DEFAULT_WINDOW_MAP: RankContext['window_clock_map'] = {
  morning:  [{ start_hhmm: '08:00', end_hhmm: '10:00' }],
  deep:     [{ start_hhmm: '10:00', end_hhmm: '13:00' }],
  shallow:  [{ start_hhmm: '13:00', end_hhmm: '15:00' }],
  evening:  [{ start_hhmm: '17:00', end_hhmm: '19:00' }],
  any:      [{ start_hhmm: '08:00', end_hhmm: '19:00' }],
};

function storeForOpts(opts: { chitinDir?: string }): ReturnType<typeof openStore> {
  const chitinDir = opts.chitinDir ?? resolveChitinDir(process.cwd(), '');
  const dbPath = join(chitinDir, 'scheduler', 'items.sqlite');
  return openStore(dbPath);
}

export function registerScheduler(program: Command): void {
  const sched = program.command('scheduler').description('Personal scheduler + swarm coordination');

  sched
    .command('ingest <text>')
    .description('Parse natural language into scheduled items and persist them')
    .option('--chitin-dir <path>', 'override .chitin dir')
    .option('--model <model>', 'Anthropic model to use', 'claude-opus-4-7')
    .option('--dry-run', 'print parsed items without persisting')
    .action(async (text: string, opts: { chitinDir?: string; model: string; dryRun?: boolean }) => {
      const now = new Date().toISOString();
      const result = await ingest(text, { now, preferred_model: opts.model });

      if (result.telemetry.parse_failure) {
        console.error('ingest failed:', result.telemetry.parse_failure.reason);
        process.exit(1);
      }

      if (result.items.length === 0) {
        console.log('No items parsed from input.');
        return;
      }

      const itemsWithIds = result.items.map((item) => ({
        ...item,
        id: item.id || randomUUID(),
        created_at: item.created_at || now,
      }));

      if (opts.dryRun) {
        console.log(JSON.stringify(itemsWithIds, null, 2));
        return;
      }

      const store = storeForOpts(opts);
      try {
        for (const item of itemsWithIds) {
          store.add(item);
        }
        console.log(`Ingested ${itemsWithIds.length} item(s):`);
        for (const item of itemsWithIds) {
          console.log(`  [${item.item_type}] ${item.title} (${item.id})`);
        }
      } finally {
        store.close();
      }
    });

  sched
    .command('today')
    .description('Show ranked slots for today')
    .option('--chitin-dir <path>', 'override .chitin dir')
    .option('--format <fmt>', 'output format: text|json', 'text')
    .action((opts: { chitinDir?: string; format: string }) => {
      const store = storeForOpts(opts);
      try {
        const now = new Date().toISOString();
        const openItems = store.list({ status: 'open' });
        const scheduledEvents = openItems.filter((i) => i.item_type === 'event') as Parameters<typeof rank>[0]['scheduled_events'];

        const ctx: RankContext = {
          now,
          open_items: openItems,
          scheduled_events: scheduledEvents,
          window_clock_map: DEFAULT_WINDOW_MAP,
          consumer: 'personal',
        };

        const result = rank(ctx);

        if (opts.format === 'json') {
          console.log(JSON.stringify(result, null, 2));
          return;
        }

        if (result.ordered.length === 0) {
          console.log('No open items for today.');
          return;
        }

        console.log(`Today — ${now.slice(0, 10)}\n`);

        const slotMap = new Map(result.slots.map((s) => [s.item_id, s]));
        for (const item of result.ordered) {
          const slot = slotMap.get(item.id);
          const slotStr = slot ? `  → ${slot.start.slice(11, 16)}–${slot.end.slice(11, 16)}` : '  → unslotted';
          console.log(`[${item.item_type}] ${item.title}${slotStr}`);
        }

        console.log(`\n${result.slots.length}/${result.ordered.length} items slotted`);
      } finally {
        store.close();
      }
    });

  sched
    .command('tick')
    .description('Notify items due in the next window')
    .option('--chitin-dir <path>', 'override .chitin dir')
    .option('--lookahead-min <n>', 'minutes ahead to check', (v) => parseInt(v, 10), 15)
    .option('--notifier <name>', 'notifier to use (ntfy|slack)', 'ntfy')
    .option('--dry-run', 'list items that WOULD notify without sending')
    .action(async (opts: { chitinDir?: string; lookaheadMin: number; notifier: string; dryRun?: boolean }) => {
      const store = storeForOpts(opts);
      try {
        const now = new Date();
        const windowEnd = new Date(now.getTime() + opts.lookaheadMin * 60_000);
        const allItems = store.list({ status: 'open' });

        const due = allItems.filter((item) => {
          if (item.item_type === 'task' && item.scheduled_start) {
            const start = new Date(item.scheduled_start);
            return start >= now && start <= windowEnd;
          }
          if (item.item_type === 'event') {
            const start = new Date(item.start);
            return start >= now && start <= windowEnd;
          }
          return false;
        });

        if (due.length === 0) {
          console.log('No items due in the next', opts.lookaheadMin, 'minutes.');
          return;
        }

        if (opts.dryRun) {
          console.log(`Would notify ${due.length} item(s) via ${opts.notifier}:`);
          for (const item of due) {
            console.log(`  [${item.item_type}] ${item.title}`);
          }
          return;
        }

        const { dispatch } = await import('@chitin/scheduler');
        // Load the notifier adapter (registers it)
        if (opts.notifier === 'ntfy') {
          await import('@chitin/scheduler/notify/ntfy');
        } else if (opts.notifier === 'slack') {
          await import('@chitin/scheduler/notify/slack');
        }

        for (const item of due) {
          try {
            await dispatch(item, opts.notifier);
            console.log(`Notified: ${item.title}`);
          } catch (err) {
            console.error(`Failed to notify ${item.id}:`, err instanceof Error ? err.message : err);
          }
        }
      } finally {
        store.close();
      }
    });

  sched
    .command('add')
    .description('Add a new item directly')
    .requiredOption('--title <title>', 'item title')
    .option('--type <type>', 'item type: task|event|backlog', 'task')
    .option('--deadline <iso>', 'deadline datetime (RFC3339)')
    .option('--priority <n>', 'priority 1-5', (v) => parseInt(v, 10))
    .option('--est-min <n>', 'estimated minutes', (v) => parseInt(v, 10))
    .option('--window <pref>', 'window preference: morning|deep|shallow|evening|any')
    .option('--chitin-dir <path>', 'override .chitin dir')
    .action((opts: {
      title: string;
      type: string;
      deadline?: string;
      priority?: number;
      estMin?: number;
      window?: string;
      chitinDir?: string;
    }) => {
      const store = storeForOpts(opts);
      try {
        const id = randomUUID();
        const now = new Date().toISOString();
        const base = { id, title: opts.title, status: 'open' as const, created_at: now };

        let item;
        if (opts.type === 'event') {
          item = { ...base, item_type: 'event' as const, start: opts.deadline ?? now };
        } else if (opts.type === 'backlog') {
          item = { ...base, item_type: 'backlog' as const };
        } else {
          item = {
            ...base,
            item_type: 'task' as const,
            deadline: opts.deadline,
            priority: opts.priority as (1|2|3|4|5) | undefined,
            est_min: opts.estMin,
            window_pref: opts.window as WindowPref | undefined,
          };
        }

        store.add(item);
        console.log(`Added ${opts.type} item: ${opts.title} (${id})`);
      } finally {
        store.close();
      }
    });

  sched
    .command('list')
    .description('List items in the store')
    .option('--chitin-dir <path>', 'override .chitin dir')
    .option('--status <s>', 'filter by status')
    .option('--type <t>', 'filter by item type')
    .option('--format <fmt>', 'output format: text|json', 'text')
    .action((opts: { chitinDir?: string; status?: string; type?: string; format: string }) => {
      const store = storeForOpts(opts);
      try {
        const items = store.list({ status: opts.status, item_type: opts.type });

        if (opts.format === 'json') {
          console.log(JSON.stringify(items, null, 2));
          return;
        }

        if (items.length === 0) {
          console.log('No items found.');
          return;
        }

        for (const item of items) {
          const extra =
            item.item_type === 'task' && item.deadline ? ` (due ${item.deadline.slice(0, 10)})` :
            item.item_type === 'event' ? ` @ ${item.start.slice(11, 16)}` : '';
          console.log(`[${item.item_type}] ${item.title}${extra}  (${item.id})`);
        }
      } finally {
        store.close();
      }
    });

  sched
    .command('complete <id>')
    .description('Mark an item as completed')
    .option('--chitin-dir <path>', 'override .chitin dir')
    .action((id: string, opts: { chitinDir?: string }) => {
      const store = storeForOpts(opts);
      try {
        store.update(id, { status: 'completed' });
        console.log(`Marked ${id} as completed`);
      } finally {
        store.close();
      }
    });
}
