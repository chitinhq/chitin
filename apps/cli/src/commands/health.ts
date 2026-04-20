import { spawnSync } from 'node:child_process';
import { resolveChitinDir } from '@chitin/contracts';
import type { Command } from 'commander';

export interface HealthReport {
  events_total: number;
  events_by_window: Record<string, number>;
  hook_failure_count: number;
  schema_drift_count: number;
  orphaned_chains: number;
  dir_exists: boolean;
  clock_skew_suspected: boolean;
  latest_event_ts?: string;
}

export function registerHealth(program: Command): void {
  program
    .command('health')
    .description('Dogfooding health metrics for the current .chitin')
    .option('--window-hours <n>', 'window size in hours', '24')
    .option('--chitin-dir <path>', 'override .chitin dir (default: resolve from cwd)')
    .action((opts: { windowHours: string; chitinDir?: string }) => {
      const chitinDir = opts.chitinDir ?? resolveChitinDir(process.cwd(), '');
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const res = spawnSync(
        kernelBin,
        ['health', '--dir', chitinDir, '--window-hours', opts.windowHours],
        { encoding: 'utf8' },
      );
      if (res.error) {
        console.error(`chitin health: failed to start ${kernelBin}: ${res.error.message}`);
        process.exit(3);
      }
      if (res.status !== 0) {
        if (res.stderr) console.error(res.stderr);
        process.exit(res.status ?? 3);
      }
      let report: HealthReport;
      try {
        report = JSON.parse(res.stdout) as HealthReport;
      } catch (err) {
        const snippet = res.stdout.slice(0, 200);
        console.error(
          `chitin health: could not parse kernel output: ${String(err)}\nstdout: ${snippet}`,
        );
        process.exit(3);
      }
      printReport(report, chitinDir);
      process.exit(exitCode(report));
    });
}

export function renderReport(r: HealthReport, chitinDir: string): string[] {
  const lines: string[] = [];
  const add = (label: string, value: string | number, status: 'pass' | 'warn' | 'fail') => {
    const tag = status === 'pass' ? '[PASS]' : status === 'warn' ? '[WARN]' : '[FAIL]';
    lines.push(`${tag}  ${label.padEnd(28)} ${value}`);
  };

  lines.push(`chitin health — ${chitinDir}`);
  if (!r.dir_exists) {
    add('chitin dir', 'MISSING', 'fail');
    return lines;
  }
  add('events total', r.events_total, r.events_total > 0 ? 'pass' : 'warn');
  for (const [surface, count] of Object.entries(r.events_by_window)) {
    add(`  events / ${surface}`, count, count > 0 ? 'pass' : 'warn');
  }
  add('hook failures', r.hook_failure_count, r.hook_failure_count === 0 ? 'pass' : 'fail');
  add('schema drift', r.schema_drift_count, r.schema_drift_count === 0 ? 'pass' : 'fail');
  add('orphaned chains', r.orphaned_chains, r.orphaned_chains === 0 ? 'pass' : 'warn');
  if (r.clock_skew_suspected) {
    add('clock skew', 'suspected', 'warn');
  }
  return lines;
}

function printReport(r: HealthReport, chitinDir: string): void {
  for (const line of renderReport(r, chitinDir)) console.log(line);
}

export function exitCode(r: HealthReport): number {
  if (!r.dir_exists) return 1;
  if (r.hook_failure_count > 0 || r.schema_drift_count > 0) return 1;
  return 0;
}
