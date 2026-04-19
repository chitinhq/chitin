import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Event } from '@chitin/contracts';

/**
 * Stream events for a given run_id from the JSONL ground truth, in file order.
 * Reads the immutable log directly rather than SQLite — replay is forensic.
 */
export function* replayRun(workspace: string, runId: string): Generator<Event> {
  const path = join(workspace, '.chitin', `events-${runId}.jsonl`);
  if (!existsSync(path)) {
    throw new Error(`no JSONL for run_id=${runId} (expected at ${path})`);
  }
  const data = readFileSync(path, 'utf8');
  for (const line of data.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    yield JSON.parse(trimmed) as Event;
  }
}
