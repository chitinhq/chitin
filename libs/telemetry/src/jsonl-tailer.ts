import { readFileSync } from 'node:fs';
import type { Event } from '@chitin/contracts';

export type EventHandler = (ev: Event) => void;

/**
 * Read every line of a JSONL file once and pass each parsed Event to `handler`.
 * Malformed lines are logged to stderr and skipped.
 */
export function tailJsonlOnce(path: string, handler: EventHandler): void {
  const data = readFileSync(path, 'utf8');
  for (const line of data.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      const ev = JSON.parse(trimmed) as Event;
      handler(ev);
    } catch (err) {
      console.error(`[telemetry] skipping malformed line in ${path}: ${(err as Error).message}`);
    }
  }
}
