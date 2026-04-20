import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { resolveChitinDir } from '@chitin/contracts';
import type { Command } from 'commander';
import { eventsListCommand } from './events-list.js';
import type { HealthReport } from './health.js';

export interface ReviewOpts {
  last: string;
  chitinDir?: string;
}

export function registerReview(program: Command): void {
  program
    .command('review')
    .description('Weekly review skim: health + recent session list')
    .option('--last <window>', 'window (e.g. 7d, 24h)', '7d')
    .option('--chitin-dir <path>', 'override .chitin dir (default: resolve from cwd)')
    .action((opts: ReviewOpts) => {
      const hours = parseWindow(opts.last);
      const chitinDir = opts.chitinDir ?? resolveChitinDir(process.cwd(), '');
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';

      console.log(`# chitin review — window: ${opts.last} (${hours}h)\n`);

      const h = spawnSync(
        kernelBin,
        ['health', '--dir', chitinDir, '--window-hours', String(hours)],
        { encoding: 'utf8' },
      );
      if (h.error) {
        console.error(`health: failed to start ${kernelBin}: ${h.error.message}`);
      } else if (h.status !== 0) {
        if (h.stderr) console.error(`health exited ${h.status}: ${h.stderr.trim()}`);
      } else {
        try {
          const r = JSON.parse(h.stdout) as HealthReport;
          for (const line of renderHealth(r, chitinDir)) console.log(line);
        } catch (err) {
          console.error(`health: could not parse kernel output: ${String(err)}`);
        }
      }
      console.log('');

      console.log('## Recent sessions');
      const dbPath = join(chitinDir, 'events.db');
      if (!existsSync(dbPath)) {
        console.log('(no events captured yet)');
      } else {
        eventsListCommand({ workspace: dirname(chitinDir), limit: 20 });
      }
    });
}

export function parseWindow(w: string): number {
  const m = /^(\d+)([hd])$/.exec(w);
  if (!m) throw new Error(`bad window: ${w}`);
  const n = parseInt(m[1], 10);
  return m[2] === 'd' ? n * 24 : n;
}

export function renderHealth(r: HealthReport, chitinDir: string): string[] {
  const lines: string[] = [`## Health (${chitinDir})`];
  if (!r.dir_exists) {
    lines.push('- chitin dir:        MISSING');
    return lines;
  }
  lines.push(`- events total:      ${r.events_total}`);
  for (const [s, c] of Object.entries(r.events_by_window)) {
    lines.push(`- events / ${s}:     ${c}`);
  }
  lines.push(`- hook failures:     ${r.hook_failure_count}`);
  lines.push(`- schema drift:      ${r.schema_drift_count}`);
  lines.push(`- orphaned chains:   ${r.orphaned_chains}`);
  return lines;
}
