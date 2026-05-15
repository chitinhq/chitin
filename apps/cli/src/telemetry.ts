import BetterSqlite3 from 'better-sqlite3';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

interface V2Event {
  schema_version: string;
  run_id: string;
  session_id: string;
  surface: string;
  driver_identity: { user: string; machine_id: string; machine_fingerprint: string };
  agent_instance_id: string;
  parent_agent_id: string | null;
  agent_fingerprint: string;
  event_type: string;
  chain_id: string;
  chain_type: string;
  parent_chain_id: string | null;
  seq: number;
  prev_hash: string | null;
  this_hash: string;
  ts: string;
  labels: Record<string, string>;
  payload: Record<string, unknown>;
}

const CREATE_SCHEMA = `
CREATE TABLE IF NOT EXISTS events (
  this_hash          TEXT PRIMARY KEY,
  schema_version     TEXT NOT NULL,
  run_id             TEXT NOT NULL,
  session_id         TEXT NOT NULL,
  surface            TEXT NOT NULL,
  driver_identity    TEXT NOT NULL,
  agent_instance_id  TEXT NOT NULL,
  parent_agent_id    TEXT,
  agent_fingerprint  TEXT NOT NULL,
  event_type         TEXT NOT NULL,
  chain_id           TEXT NOT NULL,
  chain_type         TEXT NOT NULL,
  parent_chain_id    TEXT,
  seq                INTEGER NOT NULL,
  prev_hash          TEXT,
  ts                 TEXT NOT NULL,
  labels             TEXT NOT NULL,
  payload            TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_chain ON events(chain_id, seq);
`;

export function ensureIndexed(chitinDir: string): void {
  if (!existsSync(chitinDir)) return;
  const dbPath = join(chitinDir, 'events.db');
  const events: V2Event[] = [];
  for (const name of readdirSync(chitinDir)) {
    if (!name.startsWith('events-') || !name.endsWith('.jsonl')) continue;
    const content = readFileSync(join(chitinDir, name), 'utf8');
    for (const line of content.split('\n')) {
      if (!line.trim()) continue;
      try {
        events.push(JSON.parse(line) as V2Event);
      } catch {
        // Skip malformed lines.
      }
    }
  }
  if (events.length === 0) return;
  indexEvents(dbPath, events);
}

export function replaySessionAsTree(dbPath: string, sessionID: string): Array<Record<string, unknown>> {
  const db = new BetterSqlite3(dbPath, { readonly: true });
  const rows = db
    .prepare(`SELECT * FROM events WHERE session_id = ? ORDER BY ts ASC, seq ASC`)
    .all(sessionID) as Array<Record<string, unknown> & { driver_identity: string; labels: string; payload: string }>;
  db.close();
  return rows.map((row) => ({
    ...row,
    driver_identity: JSON.parse(row.driver_identity),
    labels: JSON.parse(row.labels),
    payload: JSON.parse(row.payload),
  }));
}

export async function getEventsBySession(
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
    .all(sessionID) as Array<{
      chain_id: string;
      chain_type: string;
      parent_chain_id: string | null;
      seq: number;
      event_type: string;
      ts: string;
    }>;
  db.close();
  return rows;
}

function indexEvents(dbPath: string, events: V2Event[]): void {
  const db = new BetterSqlite3(dbPath);
  db.pragma('journal_mode = WAL');
  db.pragma('synchronous = NORMAL');
  db.exec(CREATE_SCHEMA);
  const stmt = db.prepare(`
    INSERT OR IGNORE INTO events (
      this_hash, schema_version, run_id, session_id, surface,
      driver_identity, agent_instance_id, parent_agent_id, agent_fingerprint,
      event_type, chain_id, chain_type, parent_chain_id, seq, prev_hash,
      ts, labels, payload
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  `);
  const tx = db.transaction((rows: V2Event[]) => {
    for (const event of rows) {
      stmt.run(
        event.this_hash,
        event.schema_version,
        event.run_id,
        event.session_id,
        event.surface,
        JSON.stringify(event.driver_identity),
        event.agent_instance_id,
        event.parent_agent_id,
        event.agent_fingerprint,
        event.event_type,
        event.chain_id,
        event.chain_type,
        event.parent_chain_id,
        event.seq,
        event.prev_hash,
        event.ts,
        JSON.stringify(event.labels),
        JSON.stringify(event.payload),
      );
    }
  });
  tx(events);
  db.close();
}
