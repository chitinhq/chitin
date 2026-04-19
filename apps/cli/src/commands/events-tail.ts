import { openDb, indexEvent, tailJsonlOnce } from '@chitin/telemetry';
import { readdirSync, existsSync, statSync, watch } from 'node:fs';
import { join } from 'node:path';

export interface TailOpts {
  workspace?: string;
  surface?: string;
}

/**
 * Watch .chitin/events-*.jsonl; for each new line, index into SQLite and
 * print a human-readable row. Blocks until SIGINT.
 */
export function eventsTailCommand(opts: TailOpts): void {
  const workspace = opts.workspace ?? process.cwd();
  const chitinDir = join(workspace, '.chitin');
  if (!existsSync(chitinDir)) {
    process.stderr.write(`no .chitin dir at ${chitinDir} — nothing to tail\n`);
    return;
  }

  const db = openDb(join(chitinDir, 'events.db'));
  const offsets = new Map<string, number>();

  function drain(filename: string): void {
    const full = join(chitinDir, filename);
    const lastOffset = offsets.get(full) ?? 0;
    const size = statSync(full).size;
    if (size <= lastOffset) return;

    tailJsonlOnce(full, (ev) => {
      if (opts.surface && ev.surface !== opts.surface) return;
      indexEvent(db, ev);
      process.stdout.write(
        `${ev.ts}  ${ev.surface.padEnd(14)} ${ev.action_type.padEnd(10)} ${ev.tool_name.padEnd(12)} ${ev.run_id}\n`,
      );
    });
    offsets.set(full, size);
  }

  for (const f of readdirSync(chitinDir)) {
    if (f.startsWith('events-') && f.endsWith('.jsonl')) drain(f);
  }

  watch(chitinDir, { persistent: true }, (_event, filename) => {
    if (!filename) return;
    if (!filename.startsWith('events-') || !filename.endsWith('.jsonl')) return;
    drain(filename);
  });

  process.stdout.write(`tailing ${chitinDir}/events-*.jsonl (Ctrl-C to stop)\n`);
}
