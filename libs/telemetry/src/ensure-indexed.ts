import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { indexEvents, type V2Event } from './sqlite-indexer.js';

/**
 * Re-materialize the v2 SQLite projection from canonical JSONL.
 *
 * The kernel owns appends to `events-<run_id>.jsonl`; the DB is a derived
 * sidecar (see design spec §7). `indexEvents` is idempotent via
 * `INSERT OR IGNORE` on `this_hash`, so this can be called freely before any
 * DB-backed query (`events list`, `events tree`, `replay`).
 *
 * No-op if the `.chitin` directory doesn't exist.
 */
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
        // Malformed line — skip.
      }
    }
  }
  if (events.length === 0) return;
  indexEvents(dbPath, events);
}
