import { tailJSONL } from '@chitin/telemetry';
import { readdirSync, existsSync, watch } from 'node:fs';
import { join } from 'node:path';

export interface TailOpts {
  workspace?: string;
  surface?: string;
}

export async function eventsTailCommand(opts: TailOpts): Promise<void> {
  const workspace = opts.workspace ?? process.cwd();
  const chitinDir = join(workspace, '.chitin');
  if (!existsSync(chitinDir)) {
    process.stderr.write(`no .chitin dir at ${chitinDir} — nothing to tail\n`);
    return;
  }
  const drained = new Set<string>();

  async function drain(filename: string): Promise<void> {
    if (drained.has(filename)) return;
    drained.add(filename);
    for await (const ev of tailJSONL(join(chitinDir, filename))) {
      if (opts.surface && ev.surface !== opts.surface) continue;
      process.stdout.write(`${ev.ts}  ${ev.surface.padEnd(14)} ${ev.event_type.padEnd(16)} ${ev.chain_id.slice(0, 12)}\n`);
    }
  }

  for (const f of readdirSync(chitinDir)) {
    if (f.startsWith('events-') && f.endsWith('.jsonl')) await drain(f);
  }

  watch(chitinDir, { persistent: true }, (_event, filename) => {
    if (!filename) return;
    const name = String(filename);
    if (!name.startsWith('events-') || !name.endsWith('.jsonl')) return;
    drained.delete(name);
    void drain(name);
  });

  process.stdout.write(`tailing ${chitinDir}/events-*.jsonl (Ctrl-C to stop)\n`);
}
