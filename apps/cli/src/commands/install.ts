import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Command } from 'commander';

interface InstallOpts {
  surface: string;
  global: boolean;
  adapter?: string;
}

export function registerInstall(program: Command): void {
  program
    .command('install')
    .description('Install chitin capture for a surface')
    .requiredOption('--surface <name>', 'surface to install (claude-code)')
    .option('--global', 'install user-level (always-on)', false)
    .option(
      '--adapter <path>',
      'adapter invocation command; defaults to $CHITIN_ADAPTER_BINARY',
    )
    .action((opts: InstallOpts) => {
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const adapter = opts.adapter ?? process.env.CHITIN_ADAPTER_BINARY ?? '';

      const args = ['install', '--surface', opts.surface];
      if (opts.global) args.push('--global');
      if (adapter) args.push('--adapter', adapter);

      const res = spawnSync(kernelBin, args, { stdio: 'inherit' });
      if (res.status !== 0) process.exit(res.status ?? 3);

      if (opts.surface === 'claude-code' && opts.global) {
        verifyClaudeCodeInstall(adapter);
      }
    });

  program
    .command('uninstall')
    .description('Remove chitin capture for a surface')
    .requiredOption('--surface <name>', 'surface to uninstall (claude-code)')
    .option('--global', 'uninstall from user-level settings', false)
    .action((opts: { surface: string; global: boolean }) => {
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const args = ['uninstall', '--surface', opts.surface];
      if (opts.global) args.push('--global');

      const res = spawnSync(kernelBin, args, { stdio: 'inherit' });
      if (res.status !== 0) process.exit(res.status ?? 3);
    });
}

// verifyClaudeCodeInstall walks ~/.claude/settings.json and confirms the
// kernel's matcher-wrapper entries carry the expected adapter command for
// each subscribed hook event. Hook wrapper shape:
//
//   { "_tag": "chitin", "matcher": "", "hooks": [ { "type": "command", "command": <adapter> } ] }
//
// Exits non-zero and prints a diagnostic if any expected hook is missing.
function verifyClaudeCodeInstall(adapterCommand: string): void {
  const home = process.env.HOME;
  if (!home) return;
  const settingsPath = join(home, '.claude', 'settings.json');
  if (!existsSync(settingsPath)) {
    console.error(`verify: settings.json not found at ${settingsPath}`);
    process.exit(4);
  }
  const settings = JSON.parse(readFileSync(settingsPath, 'utf8')) as {
    hooks?: Record<string, Array<{ _tag?: string; hooks?: Array<{ command?: string }> }>>;
  };
  const hooks = settings.hooks ?? {};
  const expected = ['SessionStart', 'PreToolUse', 'PostToolUse', 'SessionEnd'];
  for (const h of expected) {
    const list = hooks[h] ?? [];
    const hit = list.some((w) => {
      if (w._tag !== 'chitin') return false;
      const inner = w.hooks ?? [];
      return inner.some((e) => !adapterCommand || e.command === adapterCommand);
    });
    if (!hit) {
      console.error(
        `verify: hook ${h} missing chitin wrapper${adapterCommand ? ` with command=${adapterCommand}` : ''}`,
      );
      process.exit(4);
    }
  }
  console.log(`verify: OK — chitin adapter wired for ${expected.join(', ')} ...`);
}
