import { spawnSync } from 'node:child_process';
import { resolveChitinDir } from '@chitin/contracts';
import type { Command } from 'commander';

interface HealthReport {
  events_total: number;
  events_by_window: Record<string, number>;
  hook_failure_count: number;
  schema_drift_count: number;
  orphaned_chains: number;
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
      if (res.status !== 0) {
        console.error(res.stderr);
        process.exit(res.status ?? 3);
      }
      const report = JSON.parse(res.stdout) as HealthReport;
      printReport(report, chitinDir);
      process.exit(exitCode(report));
    });
}

function printReport(r: HealthReport, chitinDir: string): void {
  const line = (label: string, value: string | number, status: 'pass' | 'warn' | 'fail') => {
    const tag = status === 'pass' ? '[PASS]' : status === 'warn' ? '[WARN]' : '[FAIL]';
    console.log(`${tag}  ${label.padEnd(28)} ${value}`);
  };

  console.log(`chitin health — ${chitinDir}`);
  line('events total', r.events_total, r.events_total > 0 ? 'pass' : 'warn');
  for (const [surface, count] of Object.entries(r.events_by_window)) {
    line(`  events / ${surface}`, count, count > 0 ? 'pass' : 'warn');
  }
  line('hook failures', r.hook_failure_count, r.hook_failure_count === 0 ? 'pass' : 'fail');
  line('schema drift', r.schema_drift_count, r.schema_drift_count === 0 ? 'pass' : 'fail');
  line('orphaned chains', r.orphaned_chains, r.orphaned_chains === 0 ? 'pass' : 'warn');
}

function exitCode(r: HealthReport): number {
  if (r.hook_failure_count > 0 || r.schema_drift_count > 0) return 1;
  return 0;
}
