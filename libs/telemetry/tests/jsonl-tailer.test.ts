import { describe, expect, it } from 'vitest';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { tailJSONL } from '../src/jsonl-tailer';

const sampleLine = JSON.stringify({
  schema_version: '2',
  run_id: 'r-1',
  session_id: 's-1',
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
});

describe('tailJSONL', () => {
  it('yields parsed v2 events from a JSONL file', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-tel-'));
    const path = join(dir, 'events.jsonl');
    writeFileSync(path, sampleLine + '\n' + sampleLine + '\n');
    const out: unknown[] = [];
    for await (const e of tailJSONL(path)) out.push(e);
    expect(out.length).toBe(2);
    expect((out[0] as any).event_type).toBe('session_start');
  });

  it('tolerates malformed lines by skipping them', async () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-tel-'));
    const path = join(dir, 'events.jsonl');
    writeFileSync(path, sampleLine + '\n{bad\n' + sampleLine + '\n');
    const out: unknown[] = [];
    for await (const e of tailJSONL(path)) out.push(e);
    expect(out.length).toBe(2);
  });
});
