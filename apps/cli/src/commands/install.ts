import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Command } from 'commander';

export function registerInstall(program: Command): void {
  program
    .command('install')
    .description('Install chitin capture for a surface')
    .requiredOption('--surface <name>', 'surface to install (claude-code)')
    .option('--global', 'install user-level (always-on)', false)
    .option('--adapter <path>', 'adapter binary path (default: resolve from workspace)')
    .action((opts: { surface: string; global: boolean; adapter?: string }) => {
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const adapterBin = opts.adapter ?? resolveAdapterBin(opts.surface);

      const args = ['install', '--surface', opts.surface];
      if (opts.global) args.push('--global');
      if (adapterBin) args.push('--adapter', adapterBin);

      const res = spawnSync(kernelBin, args, { stdio: 'inherit' });
      if (res.status !== 0) process.exit(res.status ?? 3);

      if (opts.surface === 'claude-code' && opts.global) {
        verifyClaudeCodeInstall(adapterBin);
      }
    });

  program
    .command('uninstall')
    .description('Remove chitin capture for a surface')
    .requiredOption('--surface <name>', 'surface to uninstall (claude-code)')
    .option('--global', 'uninstall from user-level settings', false)
    .option('--adapter <path>', 'adapter binary path (must match install)')
    .action((opts: { surface: string; global: boolean; adapter?: string }) => {
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const adapterBin = opts.adapter ?? resolveAdapterBin(opts.surface);

      const args = ['uninstall', '--surface', opts.surface];
      if (opts.global) args.push('--global');
      if (adapterBin) args.push('--adapter', adapterBin);

      const res = spawnSync(kernelBin, args, { stdio: 'inherit' });
      if (res.status !== 0) process.exit(res.status ?? 3);
    });
}

function resolveAdapterBin(surface: string): string {
  if (surface !== 'claude-code') return '';
  // Expect the adapter's bin to have been pnpm-linked; fall back to repo-local tsx invocation.
  const env = process.env.CHITIN_ADAPTER_BINARY;
  if (env) return env;
  const repoLocal = findRepoRoot();
  if (!repoLocal) return '';
  const cliPath = join(repoLocal, 'libs/adapters/claude-code/bin/cli.ts');
  // Wrap as a shell command that invokes tsx on the TS entry. The settings.json
  // command field accepts a full shell invocation.
  return existsSync(cliPath) ? `node --import tsx ${cliPath}` : '';
}

function findRepoRoot(): string | null {
  let dir = process.cwd();
  while (dir !== '/') {
    if (existsSync(join(dir, 'pnpm-workspace.yaml'))) return dir;
    dir = join(dir, '..');
  }
  return null;
}

function verifyClaudeCodeInstall(adapterBin: string): void {
  const home = process.env.HOME;
  if (!home) return;
  const settingsPath = join(home, '.claude', 'settings.json');
  if (!existsSync(settingsPath)) {
    console.error(`verify: settings.json not found at ${settingsPath}`);
    process.exit(4);
  }
  const s = JSON.parse(readFileSync(settingsPath, 'utf8'));
  const hooks = s.hooks ?? {};
  const expected = ['SessionStart', 'PreToolUse', 'PostToolUse', 'SessionEnd'];
  for (const h of expected) {
    const list = hooks[h] ?? [];
    if (!list.some((e: { command: string }) => e.command === adapterBin)) {
      console.error(`verify: hook ${h} missing chitin entry pointing at ${adapterBin}`);
      process.exit(4);
    }
  }
  console.log(`verify: OK — chitin adapter wired for ${expected.join(', ')} ...`);
}
