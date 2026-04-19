import { describe, it, expect } from 'vitest';
import { mkdtempSync, rmSync, writeFileSync, appendFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { tailJsonlOnce } from '../src/jsonl-tailer.js';
import type { Event } from '@chitin/contracts';

function ev(ts: string, tool: string): Event {
  return {
    run_id: '550e8400-e29b-41d4-a716-446655440000',
    session_id: '550e8400-e29b-41d4-a716-446655440001',
    surface: 'claude-code',
    driver: 'claude',
    agent_id: 'a',
    tool_name: tool,
    raw_input: {},
    canonical_form: {},
    action_type: 'exec',
    result: 'success',
    duration_ms: 1,
    error: null,
    ts,
    metadata: {},
  };
}

describe('tailJsonlOnce', () => {
  it('reads every JSONL line and passes to the handler', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-tail-'));
    const path = join(dir, 'events-abc.jsonl');
    writeFileSync(path, '');
    appendFileSync(path, JSON.stringify(ev('2026-04-19T12:00:00Z', 'Read')) + '\n');
    appendFileSync(path, JSON.stringify(ev('2026-04-19T12:00:01Z', 'Bash')) + '\n');

    const seen: string[] = [];
    tailJsonlOnce(path, (e) => { seen.push(e.tool_name); });
    expect(seen).toEqual(['Read', 'Bash']);

    rmSync(dir, { recursive: true, force: true });
  });

  it('ignores malformed JSON lines (logs, does not throw)', () => {
    const dir = mkdtempSync(join(tmpdir(), 'chitin-tail-'));
    const path = join(dir, 'events-abc.jsonl');
    writeFileSync(path, '');
    appendFileSync(path, 'not-json\n');
    appendFileSync(path, JSON.stringify(ev('2026-04-19T12:00:00Z', 'Read')) + '\n');

    const seen: string[] = [];
    expect(() => tailJsonlOnce(path, (e) => { seen.push(e.tool_name); })).not.toThrow();
    expect(seen).toEqual(['Read']);

    rmSync(dir, { recursive: true, force: true });
  });
});
