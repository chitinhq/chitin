import { replayRun } from '@chitin/telemetry';

export function replayCommand(runId: string, opts: { workspace?: string }): void {
  const workspace = opts.workspace ?? process.cwd();
  for (const ev of replayRun(workspace, runId)) {
    process.stdout.write(JSON.stringify(ev) + '\n');
  }
}
