import { spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, rmSync, existsSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
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
  const adapterBin = process.env.CHITIN_ADAPTER_BINARY;
  if (!adapterBin) {
    console.error(
      `chitin run claude-code requires CHITIN_ADAPTER_BINARY to point at the Claude Code adapter executable.\n` +
        `Set it to the built adapter binary before invoking (see libs/adapters/claude-code).`,
    );
    process.exit(2);
  }
  runKernel(
    ctx.kernelBinary,
    ['install-hook', '--dir', ctx.chitinDir, '--session-id', ctx.sessionID, '--adapter', adapterBin],
    { fatal: true },
  );
  try {
    const res = spawnSync('claude', args, {
      stdio: 'inherit',
      env: { ...process.env, CLAUDE_CODE_SETTINGS: join(ctx.chitinDir, 'sessions', ctx.sessionID, 'settings.json') },
    });
    process.exit(res.status ?? 0);
  } finally {
    runKernel(ctx.kernelBinary, ['uninstall-hook', '--dir', ctx.chitinDir, '--session-id', ctx.sessionID], { fatal: false });
  }
}

function launchOllama(ctx: ReturnType<typeof buildAdapterContext>, args: string[]): void {
  emitEvent(ctx, 'session_start', ctx.sessionID, 'session', null, {
    cwd: process.cwd(),
    client_info: { name: 'ollama', version: 'unknown' },
    model: { name: firstModelArg(args) ?? 'unknown', provider: 'ollama' },
    system_prompt_hash: '0'.repeat(64),
    tool_allowlist_hash: '0'.repeat(64),
    agent_version: 'unknown',
  });
  const res = spawnSync('ollama', args, { stdio: 'inherit' });
  emitEvent(ctx, 'session_end', ctx.sessionID, 'session', null, {
    reason: res.status === 0 ? 'clean' : 'exit_nonzero',
    totals: { turn_count: 0, tool_call_count: 0, total_input_tokens: 0, total_output_tokens: 0, total_duration_ms: 0 },
  });
  process.exit(res.status ?? 0);
}

function firstModelArg(args: string[]): string | null {
  const runIdx = args.indexOf('run');
  return runIdx >= 0 && runIdx + 1 < args.length ? args[runIdx + 1] : null;
}

function emitEvent(
  ctx: ReturnType<typeof buildAdapterContext>,
  eventType: string,
  chainID: string,
  chainType: 'session' | 'tool_call',
  parentChainID: string | null,
  payload: Record<string, unknown>,
): void {
  const ev = {
    schema_version: '2',
    run_id: ctx.runID,
    session_id: ctx.sessionID,
    surface: ctx.surface,
    driver_identity: ctx.driverIdentity,
    agent_instance_id: ctx.agentInstanceID,
    parent_agent_id: null,
    agent_fingerprint: ctx.agentFingerprint,
    event_type: eventType,
    chain_id: chainID,
    chain_type: chainType,
    parent_chain_id: parentChainID,
    seq: 0,
    prev_hash: null,
    this_hash: '',
    ts: new Date().toISOString(),
    labels: ctx.labels,
    payload,
  };
  const dir = mkdtempSync(join(tmpdir(), 'chitin-emit-'));
  const evPath = join(dir, 'ev.json');
  writeFileSync(evPath, JSON.stringify(ev));
  try {
    runKernel(ctx.kernelBinary, ['emit', '--dir', ctx.chitinDir, '--event-file', evPath], { fatal: false });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function launchOpenClaw(ctx: ReturnType<typeof buildAdapterContext>, args: string[]): void {
  emitEvent(ctx, 'session_start', ctx.sessionID, 'session', null, {
    cwd: process.cwd(),
    client_info: { name: 'openclaw', version: 'unknown' },
    model: { name: 'unknown', provider: 'openclaw' },
    system_prompt_hash: '0'.repeat(64),
    tool_allowlist_hash: '0'.repeat(64),
    agent_version: 'unknown',
  });
  const res = spawnSync('openclaw', args, { stdio: 'inherit' });
  emitEvent(ctx, 'session_end', ctx.sessionID, 'session', null, {
    reason: res.status === 0 ? 'clean' : 'exit_nonzero',
    totals: { turn_count: 0, tool_call_count: 0, total_input_tokens: 0, total_output_tokens: 0, total_duration_ms: 0 },
  });
  process.exit(res.status ?? 0);
}
