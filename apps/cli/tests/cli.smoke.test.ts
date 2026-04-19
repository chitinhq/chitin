import { describe, it, expect } from 'vitest';
import { mkdtempSync, rmSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { eventsListCommand } from '../src/commands/events-list.js';
import { replayCommand } from '../src/commands/replay.js';
import { openDb, indexEvent } from '@chitin/telemetry';
import type { Event } from '@chitin/contracts';

function sampleEvent(runId: string, tool: string): Event {
  return {
    run_id: runId,
    session_id: runId,
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
    ts: '2026-04-19T12:00:00Z',
    metadata: {},
  };
}

describe('cli smoke', () => {
  it('events list then replay round-trips', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'chitin-cli-'));
    mkdirSync(join(workspace, '.chitin'));

    const runId = '550e8400-e29b-41d4-a716-446655440000';
    writeFileSync(
      join(workspace, '.chitin', `events-${runId}.jsonl`),
      JSON.stringify(sampleEvent(runId, 'Read')) + '\n',
    );

    const db = openDb(join(workspace, '.chitin', 'events.db'));
    indexEvent(db, sampleEvent(runId, 'Read'));
    db.close();

    const chunks: string[] = [];
    const orig = process.stdout.write.bind(process.stdout);
    (process.stdout as { write: (s: string) => boolean }).write = (c: string) => {
      chunks.push(c);
      return true;
    };

    try {
      eventsListCommand({ workspace });
      expect(chunks.join('')).toContain('Read');
      chunks.length = 0;

      replayCommand(runId, { workspace });
      expect(chunks.join('')).toContain('"tool_name":"Read"');
    } finally {
      (process.stdout as { write: typeof orig }).write = orig;
      rmSync(workspace, { recursive: true, force: true });
    }
  });
});
