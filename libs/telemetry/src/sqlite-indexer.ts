import Database from 'better-sqlite3';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { Event } from '@chitin/contracts';

const SCHEMA_PATH = join(dirname(fileURLToPath(import.meta.url)), 'schema.sql');

export function openDb(path: string): Database.Database {
  const db = new Database(path);
  db.pragma('journal_mode = WAL');
  db.exec(readFileSync(SCHEMA_PATH, 'utf8'));
  return db;
}

export function indexEvent(db: Database.Database, ev: Event): void {
  const stmt = db.prepare(`
    INSERT OR IGNORE INTO events
      (run_id, session_id, ts, surface, driver, agent_id, tool_name,
       action_type, result, duration_ms, error,
       raw_input, canonical_form, metadata)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  `);
  stmt.run(
    ev.run_id,
    ev.session_id,
    ev.ts,
    ev.surface,
    ev.driver,
    ev.agent_id,
    ev.tool_name,
    ev.action_type,
    ev.result,
    ev.duration_ms,
    ev.error,
    JSON.stringify(ev.raw_input),
    JSON.stringify(ev.canonical_form),
    JSON.stringify(ev.metadata),
  );
}

export interface ListFilter {
  surface?: string;
  run_id?: string;
  action_type?: string;
  limit?: number;
}

export function listEvents(db: Database.Database, f: ListFilter): Event[] {
  const where: string[] = [];
  const params: unknown[] = [];
  if (f.surface) { where.push('surface = ?'); params.push(f.surface); }
  if (f.run_id) { where.push('run_id = ?'); params.push(f.run_id); }
  if (f.action_type) { where.push('action_type = ?'); params.push(f.action_type); }

  const sql = `
    SELECT * FROM events
    ${where.length ? 'WHERE ' + where.join(' AND ') : ''}
    ORDER BY ts DESC
    LIMIT ${f.limit ?? 200}
  `;
  const rows = db.prepare(sql).all(...params) as Array<Record<string, unknown>>;
  return rows.map((r) => ({
    ...r,
    raw_input: JSON.parse(r['raw_input'] as string),
    canonical_form: JSON.parse(r['canonical_form'] as string),
    metadata: JSON.parse(r['metadata'] as string),
  })) as Event[];
}
