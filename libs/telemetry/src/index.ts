export * from './sqlite-indexer.js';
export * from './jsonl-tailer.js';
export * from './replay.js';

import BetterSqlite3 from 'better-sqlite3';
import { join } from 'node:path';

export function getEventsBySession(
  chitinDir: string,
  sessionID: string,
): Promise<Array<{
  chain_id: string;
  chain_type: string;
  parent_chain_id: string | null;
  seq: number;
  event_type: string;
  ts: string;
}>> {
  const db = new BetterSqlite3(join(chitinDir, 'events.db'), { readonly: true });
  const rows = db
    .prepare(
      `SELECT chain_id, chain_type, parent_chain_id, seq, event_type, ts
       FROM events
       WHERE session_id = ?`,
    )
    .all(sessionID) as any[];
  db.close();
  return Promise.resolve(rows);
}
