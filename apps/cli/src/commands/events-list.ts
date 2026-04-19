import { openDb, listEvents } from '@chitin/telemetry';
import { join } from 'node:path';

export interface ListOpts {
  workspace?: string;
  surface?: string;
  run?: string;
  action?: string;
  limit?: number;
}

export function eventsListCommand(opts: ListOpts): void {
  const workspace = opts.workspace ?? process.cwd();
  const db = openDb(join(workspace, '.chitin', 'events.db'));
  const events = listEvents(db, {
    surface: opts.surface,
    run_id: opts.run,
    action_type: opts.action,
    limit: opts.limit ?? 50,
  });
  for (const ev of events) {
    process.stdout.write(
      `${ev.ts}  ${ev.surface.padEnd(14)} ${ev.action_type.padEnd(10)} ${ev.tool_name.padEnd(12)} ${ev.run_id}\n`,
    );
  }
  db.close();
}
