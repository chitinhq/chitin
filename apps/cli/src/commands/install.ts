import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import type { Command } from 'commander';

interface InstallOpts {
  surface: string;
  global: boolean;
  adapter?: string;
  adapterShell?: boolean;
}

const SHELL_METACHAR_RE = /[;&|`\x60$()\n\r]/;

/** Returns true if `s` contains shell metacharacters that could enable
 *  command injection when the string is later passed to a shell. */
export function hasShellMetacharacters(s: string): boolean {
  return SHELL_METACHAR_RE.test(s);
}

/** Validates that `adapter` is an absolute path to an existing executable,
 *  and contains no shell metacharacters. Throws on failure. */
export function validateAdapterPath(adapter: string): void {
  if (!adapter) {
    throw new Error('adapter path cannot be empty');
  }
  if (!adapter.startsWith('/')) {
    throw new Error(`adapter path must be absolute, got: ${adapter}`);
  }
  if (hasShellMetacharacters(adapter)) {
    throw new Error(
      `adapter path contains shell metacharacters; use --adapter-shell if you intend a shell command: ${adapter}`,
    );
  }
  if (!existsSync(adapter)) {
    throw new Error(`adapter path does not exist: ${adapter}`);
  }
  const stat = statSync(adapter);
  if ((stat.mode & 0o111) === 0) {
    throw new Error(`adapter file is not executable: ${adapter}`);
  }
}

export function registerInstall(program: Command): void {
  program
    .command('install')
    .description('Install chitin capture for a surface')
    .requiredOption('--surface <name>', 'surface to install (claude-code)')
    .option('--global', 'install user-level (always-on)', false)
    .option(
      '--adapter <path>',
      'adapter binary path (must be absolute path to executable; use --adapter-shell for shell commands)',
    )
    .option(
      '--adapter-shell',
      'allow shell command string for adapter (security: trust-on-use)',
      false,
    )
    .action((opts: InstallOpts) => {
      const kernelBin = process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel';
      const adapter = opts.adapter ?? process.env.CHITIN_ADAPTER_BINARY ?? '';

      if (adapter) {
        if (opts.adapterShell) {
          console.error(
            'WARNING: --adapter-shell skips path validation. The adapter string will be invoked via shell semantics on every hook.',
          );
        } else {
          try {
            validateAdapterPath(adapter);
          } catch (err: unknown) {
            console.error(`error: ${(err as Error).message}`);
            process.exit(1);
          }
        }
      }

      const args = ['install', '--surface', opts.surface];
      if (opts.global) args.push('--global');
      if (adapter) {
        args.push('--adapter', adapter);
        if (opts.adapterShell) args.push('--adapter-shell');
      }

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
  // Must match SubscribedHooks in
  // go/execution-kernel/internal/hookinstall/install.go. Drift here would
  // let the verifier say OK while hooks are silently missing.
  const expected = [
    'SessionStart',
    'UserPromptSubmit',
    'PreToolUse',
    'PostToolUse',
    'SessionEnd',
  ];
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