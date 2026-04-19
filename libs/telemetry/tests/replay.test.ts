import { describe, it, expect } from 'vitest';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { replayRun } from '../src/replay.js';
import type { Event } from '@chitin/contracts';

function ev(runId: string, tool: string, ts: string): Event {
  return {
    run_id: runId,
    session_id: '550e8400-e29b-41d4-a716-446655440001',
    surface: 'claude-code',
    driver: 'claude',
    agent_id: '',
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

describe('replayRun', () => {
  it('yields events in file order', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'chitin-replay-'));
    mkdirSync(join(workspace, '.chitin'));
    const path = join(workspace, '.chitin', 'events-r1.jsonl');
    writeFileSync(path, [
      JSON.stringify(ev('r1', 'A', '2026-04-19T12:00:00Z')),
      JSON.stringify(ev('r1', 'B', '2026-04-19T12:00:01Z')),
    ].join('\n') + '\n');

    const tools: string[] = [];
    for (const e of replayRun(workspace, 'r1')) tools.push(e.tool_name);
    expect(tools).toEqual(['A', 'B']);

    rmSync(workspace, { recursive: true, force: true });
  });

  it('throws a helpful error if the run_id file is missing', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'chitin-replay-'));
    mkdirSync(join(workspace, '.chitin'));
    expect(() => {
      for (const _ of replayRun(workspace, 'does-not-exist')) { /* noop */ }
    }).toThrow(/events-does-not-exist\.jsonl/);
    rmSync(workspace, { recursive: true, force: true });
  });
});
