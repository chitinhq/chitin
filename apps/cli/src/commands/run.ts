import { spawnSync } from 'node:child_process';
import { existsSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import type { Command } from 'commander';
import { buildAdapterContext } from '../ctx.js';

export function registerRun(program: Command): void {
  program
    .command('run')
    .description('Launch an agent surface with chitin instrumentation attached')
    .argument('<surface>', 'agent surface to run (claude-code, ollama, openclaw)')
    .argument('[args...]', 'arguments passed through to the surface')
    .option('--label <kv>', 'add a label (key=value); can be repeated', collectLabels, {})
    .option('--chitin-dir <path>', 'chitin state dir', './.chitin')
    .action((surface: string, surfaceArgs: string[], opts: { label: Record<string, string>; chitinDir: string }) => {
      const chitinDir = opts.chitinDir;
      if (!existsSync(chitinDir)) mkdirSync(chitinDir, { recursive: true });

      const ctx = buildAdapterContext({
        surface: normalizeSurface(surface),
        chitinDir,
        labelsCli: opts.label,
      });

      runKernel(ctx.kernelBinary, ['init', '--dir', chitinDir], { fatal: true });
      runKernel(ctx.kernelBinary, ['sweep-transcripts', '--dir', chitinDir], { fatal: false });

      switch (ctx.surface) {
        case 'claude-code':
          launchClaudeCode(ctx, surfaceArgs);
          break;
        case 'ollama-local':
          launchOllama(ctx, surfaceArgs);
          break;
        case 'openclaw':
          launchOpenClaw(ctx, surfaceArgs);
          break;
        default:
          console.error(`unknown surface: ${ctx.surface}`);
          process.exit(2);
      }
    });
}

function normalizeSurface(s: string): string {
  if (s === 'ollama') return 'ollama-local';
  return s;
}

function collectLabels(kv: string, prev: Record<string, string>): Record<string, string> {
  const eq = kv.indexOf('=');
  if (eq === -1) return prev;
  const k = kv.slice(0, eq);
  const v = kv.slice(eq + 1);
  return { ...prev, [k]: v };
}

function runKernel(
  bin: string,
  args: string[],
  opts: { fatal: boolean },
): { status: number; stdout: string; stderr: string } {
  const res = spawnSync(bin, args, { encoding: 'utf8' });
  if (res.status !== 0 && opts.fatal) {
    console.error(`chitin-kernel ${args.join(' ')} failed: ${res.stderr}`);
    process.exit(3);
  }
  return { status: res.status ?? -1, stdout: res.stdout ?? '', stderr: res.stderr ?? '' };
}

function launchClaudeCode(ctx: ReturnType<typeof buildAdapterContext>, args: string[]): void {
  runKernel(ctx.kernelBinary, ['install-hook', '--dir', ctx.chitinDir, '--session-id', ctx.sessionID], { fatal: true });
  try {
    const res = spawnSync('claude', args, { stdio: 'inherit', env: { ...process.env, CLAUDE_CODE_SETTINGS: join(ctx.chitinDir, 'sessions', ctx.sessionID, 'settings.json') } });
    process.exit(res.status ?? 0);
  } finally {
    runKernel(ctx.kernelBinary, ['uninstall-hook', '--dir', ctx.chitinDir, '--session-id', ctx.sessionID], { fatal: false });
  }
}

function launchOllama(_ctx: ReturnType<typeof buildAdapterContext>, _args: string[]): void {
  console.error(`ollama adapter not yet wired (Task 14)`);
  process.exit(2);
}

function launchOpenClaw(_ctx: ReturnType<typeof buildAdapterContext>, _args: string[]): void {
  console.error(`openclaw adapter not yet wired (Task 15)`);
  process.exit(2);
}
