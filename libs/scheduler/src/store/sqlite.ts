import BetterSqlite3 from 'better-sqlite3';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import type { Item, TaskItem, EventItem, BacklogItem } from '../schema.js';
import { ItemSchema } from '../schema.js';

const CREATE_SCHEMA = `
CREATE TABLE IF NOT EXISTS items (
  id TEXT PRIMARY KEY,
  item_type TEXT NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  source_url TEXT,
  tags TEXT,
  est_min INTEGER,
  deadline TEXT,
  window_pref TEXT,
  priority INTEGER,
  scheduled_start TEXT,
  event_start TEXT,
  duration_min INTEGER,
  source_calendar TEXT,
  tier TEXT,
  blocks TEXT,
  file_scope TEXT,
  estimated_loc INTEGER
);
CREATE INDEX IF NOT EXISTS idx_items_status ON items(status);
CREATE INDEX IF NOT EXISTS idx_items_type ON items(item_type);
CREATE INDEX IF NOT EXISTS idx_items_deadline ON items(deadline);
`;

export interface ItemFilter {
  status?: string;
  item_type?: string;
}

function rowToItem(row: Record<string, unknown>): Item {
  const base = {
    id: row['id'] as string,
    title: row['title'] as string,
    status: row['status'] as Item['status'],
    created_at: row['created_at'] as string,
    source_url: (row['source_url'] as string | null) ?? undefined,
    tags: row['tags'] ? (JSON.parse(row['tags'] as string) as string[]) : undefined,
  };

  if (row['item_type'] === 'task') {
    return {
      ...base,
      item_type: 'task',
      est_min: (row['est_min'] as number | null) ?? undefined,
      deadline: (row['deadline'] as string | null) ?? undefined,
      window_pref: (row['window_pref'] as TaskItem['window_pref']) ?? undefined,
      priority: (row['priority'] as TaskItem['priority']) ?? undefined,
      scheduled_start: (row['scheduled_start'] as string | null) ?? undefined,
    };
  }

  if (row['item_type'] === 'event') {
    return {
      ...base,
      item_type: 'event',
      start: row['event_start'] as string,
      duration_min: (row['duration_min'] as number | null) ?? undefined,
      source_calendar: (row['source_calendar'] as EventItem['source_calendar']) ?? undefined,
    };
  }

  return {
    ...base,
    item_type: 'backlog',
    tier: (row['tier'] as BacklogItem['tier']) ?? undefined,
    blocks: row['blocks'] ? (JSON.parse(row['blocks'] as string) as string[]) : undefined,
    file_scope: row['file_scope'] ? (JSON.parse(row['file_scope'] as string) as string[]) : undefined,
    estimated_loc: (row['estimated_loc'] as number | null) ?? undefined,
  };
}

function itemToRow(item: Item): Record<string, unknown> {
  const row: Record<string, unknown> = {
    id: item.id,
    item_type: item.item_type,
    title: item.title,
    status: item.status,
    created_at: item.created_at,
    source_url: item.source_url ?? null,
    tags: item.tags ? JSON.stringify(item.tags) : null,
    est_min: null,
    deadline: null,
    window_pref: null,
    priority: null,
    scheduled_start: null,
    event_start: null,
    duration_min: null,
    source_calendar: null,
    tier: null,
    blocks: null,
    file_scope: null,
    estimated_loc: null,
  };

  if (item.item_type === 'task') {
    row['est_min'] = item.est_min ?? null;
    row['deadline'] = item.deadline ?? null;
    row['window_pref'] = item.window_pref ?? null;
    row['priority'] = item.priority ?? null;
    row['scheduled_start'] = item.scheduled_start ?? null;
  } else if (item.item_type === 'event') {
    row['event_start'] = item.start;
    row['duration_min'] = item.duration_min ?? null;
    row['source_calendar'] = item.source_calendar ?? null;
  } else if (item.item_type === 'backlog') {
    row['tier'] = item.tier ?? null;
    row['blocks'] = item.blocks ? JSON.stringify(item.blocks) : null;
    row['file_scope'] = item.file_scope ? JSON.stringify(item.file_scope) : null;
    row['estimated_loc'] = item.estimated_loc ?? null;
  }

  return row;
}

export class ItemStore {
  private db: BetterSqlite3.Database;

  constructor(dbPath: string) {
    mkdirSync(dirname(dbPath), { recursive: true });
    this.db = new BetterSqlite3(dbPath);
    this.db.pragma('journal_mode = WAL');
    this.db.pragma('synchronous = NORMAL');
    this.db.exec(CREATE_SCHEMA);
  }

  add(item: Item): void {
    const row = itemToRow(item);
    const keys = Object.keys(row);
    const placeholders = keys.map(() => '?').join(', ');
    const cols = keys.join(', ');
    this.db
      .prepare(`INSERT OR REPLACE INTO items (${cols}) VALUES (${placeholders})`)
      .run(Object.values(row));
  }

  get(id: string): Item | undefined {
    const row = this.db
      .prepare('SELECT * FROM items WHERE id = ?')
      .get(id) as Record<string, unknown> | undefined;
    return row ? rowToItem(row) : undefined;
  }

  list(filter: ItemFilter = {}): Item[] {
    let sql = 'SELECT * FROM items WHERE 1=1';
    const params: unknown[] = [];
    if (filter.status) {
      sql += ' AND status = ?';
      params.push(filter.status);
    }
    if (filter.item_type) {
      sql += ' AND item_type = ?';
      params.push(filter.item_type);
    }
    const rows = this.db.prepare(sql).all(...params) as Record<string, unknown>[];
    return rows.map(rowToItem);
  }

  update(id: string, patch: Partial<Item>): void {
    const existing = this.get(id);
    if (!existing) throw new Error(`Item not found: ${id}`);
    // Re-validate the merged shape via the discriminated union before
    // persisting. Without this, callers can patch into invalid states
    // — e.g., switch item_type from `task` to `event` without a `start`,
    // or set a status outside the allowed set — and reads later break.
    const merged = ItemSchema.parse({ ...existing, ...patch, id });
    this.add(merged);
  }

  delete(id: string): void {
    this.db.prepare('DELETE FROM items WHERE id = ?').run(id);
  }

  close(): void {
    this.db.close();
  }
}

export function openStore(dbPath: string): ItemStore {
  return new ItemStore(dbPath);
}
