import BetterSqlite3 from 'better-sqlite3';
import { join } from 'node:path';
import { ensureIndexed } from '@chitin/telemetry';

export interface ListOpts {
  workspace?: string;
  surface?: string;
  run?: string;
  limit?: number;
}

export function eventsListCommand(opts: ListOpts): void {
  const workspace = opts.workspace ?? process.cwd();
  const chitinDir = join(workspace, '.chitin');
  ensureIndexed(chitinDir);
  const dbPath = join(chitinDir, 'events.db');
  const db = new BetterSqlite3(dbPath, { readonly: true });
  const clauses: string[] = [];
  const params: unknown[] = [];
  if (opts.surface) { clauses.push('surface = ?'); params.push(opts.surface); }
  if (opts.run) { clauses.push('run_id = ?'); params.push(opts.run); }
  const where = clauses.length ? `WHERE ${clauses.join(' AND ')}` : '';
  const limit = opts.limit ?? 50;
  const rows = db
    .prepare(`SELECT ts, surface, event_type, chain_id, session_id FROM events ${where} ORDER BY ts DESC LIMIT ?`)
    .all(...params, limit) as Array<{ ts: string; surface: string; event_type: string; chain_id: string; session_id: string }>;
  for (const r of rows) {
    process.stdout.write(`${r.ts}  ${r.surface.padEnd(14)} ${r.event_type.padEnd(16)} ${r.chain_id.slice(0, 12).padEnd(14)} ${r.session_id}\n`);
  }
  db.close();
}
