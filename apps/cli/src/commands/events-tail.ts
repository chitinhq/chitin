import { readdirSync, existsSync, statSync, watch, createReadStream } from 'node:fs';
import { createInterface } from 'node:readline';
import { join } from 'node:path';

export interface TailOpts {
  workspace?: string;
  surface?: string;
}

export function eventsTailCommand(opts: TailOpts): void {
  const workspace = opts.workspace ?? process.cwd();
  const chitinDir = join(workspace, '.chitin');
  if (!existsSync(chitinDir)) {
    process.stderr.write(`no .chitin dir at ${chitinDir} — nothing to tail\n`);
    return;
  }

  const offsets = new Map<string, number>();
  const draining = new Set<string>();

  async function drain(filename: string): Promise<void> {
    if (draining.has(filename)) return;
    draining.add(filename);
    try {
      const full = join(chitinDir, filename);
      const size = statSync(full).size;
      const start = offsets.get(filename) ?? 0;
      if (size <= start) return;
      const rl = createInterface({
        input: createReadStream(full, { start, encoding: 'utf8' }),
        crlfDelay: Infinity,
      });
      for await (const line of rl) {
        if (!line.trim()) continue;
        try {
          const ev = JSON.parse(line) as {
            ts: string;
            surface: string;
            event_type: string;
            chain_id: string;
          };
          if (opts.surface && ev.surface !== opts.surface) continue;
          process.stdout.write(
            `${ev.ts}  ${ev.surface.padEnd(14)} ${ev.event_type.padEnd(16)} ${ev.chain_id.slice(0, 12)}\n`,
          );
        } catch {
          // Malformed line — skip.
        }
      }
      offsets.set(filename, size);
    } finally {
      draining.delete(filename);
    }
  }

  for (const f of readdirSync(chitinDir)) {
    if (f.startsWith('events-') && f.endsWith('.jsonl')) void drain(f);
  }

  watch(chitinDir, { persistent: true }, (_event, filename) => {
    if (!filename) return;
    const name = String(filename);
    if (!name.startsWith('events-') || !name.endsWith('.jsonl')) return;
    void drain(name);
  });

  process.stdout.write(`tailing ${chitinDir}/events-*.jsonl (Ctrl-C to stop)\n`);
}
