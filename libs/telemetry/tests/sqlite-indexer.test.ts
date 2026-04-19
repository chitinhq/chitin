import { describe, expect, it } from 'vitest';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { indexEvents } from '../src/sqlite-indexer';
import BetterSqlite3 from 'better-sqlite3';

const sampleEvent = (over: Record<string, unknown> = {}) => ({
  schema_version: '2',
  run_id: 'run-1',
  session_id: 'sess-1',
  surface: 'claude-code',
  driver_identity: { user: 'u', machine_id: 'm', machine_fingerprint: 'a'.repeat(64) },
  agent_instance_id: 'ai-1',
  parent_agent_id: null,
  agent_fingerprint: 'b'.repeat(64),
  event_type: 'session_start',
  chain_id: 'chain-1',
  chain_type: 'session',
  parent_chain_id: null,
  seq: 0,
  prev_hash: null,
  this_hash: 'c'.repeat(64),
  ts: '2026-04-19T12:00:00Z',
  labels: {},
  payload: {},
  ...over,
});

describe('indexEvents', () => {
  it('creates events table and inserts a v2 envelope row', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-tel-'));
    const dbPath = join(dir, 'events.db');
    const events = [sampleEvent()];
    indexEvents(dbPath, events);
    const db = new BetterSqlite3(dbPath);
    const row = db.prepare('SELECT * FROM events WHERE chain_id = ?').get('chain-1') as any;
    db.close();
    expect(row.session_id).toBe('sess-1');
    expect(row.event_type).toBe('session_start');
    expect(row.seq).toBe(0);
  });

  it('is idempotent on duplicate this_hash', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-tel-'));
    const dbPath = join(dir, 'events.db');
    const e = sampleEvent();
    indexEvents(dbPath, [e]);
    indexEvents(dbPath, [e]);
    const db = new BetterSqlite3(dbPath);
    const count = db.prepare('SELECT COUNT(*) c FROM events WHERE this_hash = ?').get('c'.repeat(64)) as any;
    db.close();
    expect(count.c).toBe(1);
  });
});
