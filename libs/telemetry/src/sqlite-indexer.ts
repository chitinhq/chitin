import BetterSqlite3 from 'better-sqlite3';

export interface V2Event {
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
CREATE INDEX IF NOT EXISTS idx_events_parent_chain ON events(parent_chain_id);
CREATE INDEX IF NOT EXISTS idx_events_surface ON events(surface);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
`;

export function indexEvents(dbPath: string, events: V2Event[]): void {
  const db = new BetterSqlite3(dbPath);
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
    for (const e of rows) {
      stmt.run(
        e.this_hash,
        e.schema_version,
        e.run_id,
        e.session_id,
        e.surface,
        JSON.stringify(e.driver_identity),
        e.agent_instance_id,
        e.parent_agent_id,
        e.agent_fingerprint,
        e.event_type,
        e.chain_id,
        e.chain_type,
        e.parent_chain_id,
        e.seq,
        e.prev_hash,
        e.ts,
        JSON.stringify(e.labels),
        JSON.stringify(e.payload),
      );
    }
  });
  tx(events);
  db.close();
}
