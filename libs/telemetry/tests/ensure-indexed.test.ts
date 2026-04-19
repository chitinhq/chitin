import { describe, expect, it } from 'vitest';
import BetterSqlite3 from 'better-sqlite3';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ensureIndexed } from '../src/ensure-indexed';

const sampleEvent = (over: Record<string, unknown> = {}) => ({
  schema_version: '2',
  run_id: 'r-1',
  session_id: 'sess-A',
  surface: 'claude-code',
  driver_identity: { user: 'u', machine_id: 'm', machine_fingerprint: 'a'.repeat(64) },
  agent_instance_id: 'ai-1',
  parent_agent_id: null,
  agent_fingerprint: 'b'.repeat(64),
  event_type: 'session_start',
  chain_id: 'c-1',
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

describe('ensureIndexed', () => {
  it('materializes JSONL into events.db', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-ei-'));
    writeFileSync(
      join(dir, 'events-r-1.jsonl'),
      JSON.stringify(sampleEvent({ this_hash: 'a'.repeat(64) })) + '\n' +
        JSON.stringify(sampleEvent({ this_hash: 'b'.repeat(64), event_type: 'user_prompt', seq: 1 })) + '\n',
    );
    ensureIndexed(dir);
    const db = new BetterSqlite3(join(dir, 'events.db'));
    const count = db.prepare('SELECT COUNT(*) c FROM events').get() as { c: number };
    db.close();
    expect(count.c).toBe(2);
  });

  it('is idempotent on re-run', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-ei-'));
    writeFileSync(
      join(dir, 'events-r-1.jsonl'),
      JSON.stringify(sampleEvent({ this_hash: 'a'.repeat(64) })) + '\n',
    );
    ensureIndexed(dir);
    ensureIndexed(dir);
    const db = new BetterSqlite3(join(dir, 'events.db'));
    const count = db.prepare('SELECT COUNT(*) c FROM events').get() as { c: number };
    db.close();
    expect(count.c).toBe(1);
  });

  it('tolerates malformed lines', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-ei-'));
    writeFileSync(
      join(dir, 'events-r-1.jsonl'),
      JSON.stringify(sampleEvent({ this_hash: 'a'.repeat(64) })) + '\n{bad\n' + JSON.stringify(sampleEvent({ this_hash: 'b'.repeat(64) })) + '\n',
    );
    ensureIndexed(dir);
    const db = new BetterSqlite3(join(dir, 'events.db'));
    const count = db.prepare('SELECT COUNT(*) c FROM events').get() as { c: number };
    db.close();
    expect(count.c).toBe(2);
  });

  it('is a no-op when directory does not exist', () => {
    expect(() => ensureIndexed(join(tmpdir(), 'does-not-exist-chitin-xyz'))).not.toThrow();
  });
});
