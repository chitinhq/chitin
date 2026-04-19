import { ensureIndexed, replaySessionAsTree } from '@chitin/telemetry';
import { join } from 'node:path';

export function replayCommand(sessionId: string, opts: { workspace?: string }): void {
  const workspace = opts.workspace ?? process.cwd();
  const chitinDir = join(workspace, '.chitin');
  ensureIndexed(chitinDir);
  const dbPath = join(chitinDir, 'events.db');
  for (const ev of replaySessionAsTree(dbPath, sessionId)) {
    process.stdout.write(JSON.stringify(ev) + '\n');
  }
}
