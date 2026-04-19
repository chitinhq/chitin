import { replaySessionAsTree } from '@chitin/telemetry';
import { join } from 'node:path';

export function replayCommand(sessionId: string, opts: { workspace?: string }): void {
  const workspace = opts.workspace ?? process.cwd();
  const dbPath = join(workspace, '.chitin', 'events.db');
  for (const ev of replaySessionAsTree(dbPath, sessionId)) {
    process.stdout.write(JSON.stringify(ev) + '\n');
  }
}
