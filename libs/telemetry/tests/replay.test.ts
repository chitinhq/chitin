import { describe, expect, it } from 'vitest';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { indexEvents } from '../src/sqlite-indexer';
import { replaySessionAsTree } from '../src/replay';

function baseEvent(over: Record<string, unknown>) {
  return {
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
  };
}

describe('replaySessionAsTree', () => {
  it('returns rows for the requested session, time-ordered, JSON columns parsed', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-tel-'));
    const dbPath = join(dir, 'events.db');
    indexEvents(dbPath, [
      baseEvent({ this_hash: 'a'.repeat(64), ts: '2026-04-19T12:00:00Z', labels: { env: 'dev' } }),
      baseEvent({ this_hash: 'b'.repeat(64), event_type: 'user_prompt', seq: 1, ts: '2026-04-19T12:00:01Z' }),
      baseEvent({ this_hash: 'd'.repeat(64), session_id: 'sess-B', ts: '2026-04-19T12:00:02Z' }),
    ]);
    const rows = replaySessionAsTree(dbPath, 'sess-A');
    expect(rows.length).toBe(2);
    expect(rows[0].event_type).toBe('session_start');
    expect(rows[1].event_type).toBe('user_prompt');
    expect((rows[0].labels as any).env).toBe('dev');
  });
});
