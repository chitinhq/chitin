import BetterSqlite3 from 'better-sqlite3';

export function replaySessionAsTree(dbPath: string, sessionID: string): Array<Record<string, unknown>> {
  const db = new BetterSqlite3(dbPath, { readonly: true });
  const rows = db
    .prepare(`SELECT * FROM events WHERE session_id = ? ORDER BY ts ASC, seq ASC`)
    .all(sessionID) as any[];
  db.close();
  return rows.map((r) => ({
    ...r,
    driver_identity: JSON.parse(r.driver_identity),
    labels: JSON.parse(r.labels),
    payload: JSON.parse(r.payload),
  }));
}
