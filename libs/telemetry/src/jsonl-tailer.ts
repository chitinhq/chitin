import { createReadStream } from 'node:fs';
import { createInterface } from 'node:readline';
import type { V2Event } from './sqlite-indexer';

export async function* tailJSONL(path: string): AsyncGenerator<V2Event> {
  const rl = createInterface({
    input: createReadStream(path, { encoding: 'utf8' }),
    crlfDelay: Infinity,
  });
  for await (const line of rl) {
    if (!line.trim()) continue;
    try {
      yield JSON.parse(line) as V2Event;
    } catch {
      // Malformed line tolerance — skip.
    }
  }
}
