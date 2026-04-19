import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type Database from 'better-sqlite3';
import { openDb, indexEvent, listEvents } from '../src/sqlite-indexer.js';
import type { Event } from '@chitin/contracts';

const SAMPLE: Event = {
  run_id: '550e8400-e29b-41d4-a716-446655440000',
  session_id: '550e8400-e29b-41d4-a716-446655440001',
  surface: 'claude-code',
  driver: 'claude',
  agent_id: 'agent-xyz',
  tool_name: 'Bash',
  raw_input: { command: 'git status' },
  canonical_form: { tool: 'git', action: 'status' },
  action_type: 'git',
  result: 'success',
  duration_ms: 12,
  error: null,
  ts: '2026-04-19T12:00:00Z',
  metadata: {},
};

describe('sqlite-indexer', () => {
  let dir: string;
  let db: Database.Database;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'chitin-test-'));
    db = openDb(join(dir, 'events.db'));
  });

  afterEach(() => {
    db.close();
    rmSync(dir, { recursive: true, force: true });
  });

  it('creates schema on open', () => {
    const tables = db.prepare("SELECT name FROM sqlite_master WHERE type='table'").all() as { name: string }[];
    expect(tables.map((t) => t.name)).toContain('events');
  });

  it('indexes one event', () => {
    indexEvent(db, SAMPLE);
    const events = listEvents(db, {});
    expect(events).toHaveLength(1);
    expect(events[0].tool_name).toBe('Bash');
    expect(events[0].action_type).toBe('git');
  });

  it('dedupes on (run_id, session_id, ts, tool_name)', () => {
    indexEvent(db, SAMPLE);
    indexEvent(db, SAMPLE);
    const events = listEvents(db, {});
    expect(events).toHaveLength(1);
  });

  it('filters by surface', () => {
    indexEvent(db, SAMPLE);
    indexEvent(db, { ...SAMPLE, surface: 'openclaw', driver: 'openclaw', ts: '2026-04-19T12:00:01Z' });
    expect(listEvents(db, { surface: 'claude-code' })).toHaveLength(1);
    expect(listEvents(db, { surface: 'openclaw' })).toHaveLength(1);
  });
});
