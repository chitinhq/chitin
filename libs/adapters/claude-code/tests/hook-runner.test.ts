import { describe, it, expect } from 'vitest';
import { mkdtempSync, rmSync, readFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { runHook } from '../src/hook-runner.js';

const KERNEL_BIN =
  process.env['CHITIN_KERNEL_BIN'] ??
  join(process.cwd(), 'dist', 'go', 'execution-kernel', 'chitin-kernel');

describe('runHook (claude-code adapter)', () => {
  it('invokes the kernel binary with stdin payload and creates a JSONL event', async () => {
    if (!existsSync(KERNEL_BIN)) {
      throw new Error(`kernel binary not found at ${KERNEL_BIN}. Run: pnpm exec nx run execution-kernel:build`);
    }
    const workspace = mkdtempSync(join(tmpdir(), 'chitin-adapter-'));
    const payload = {
      session_id: '550e8400-e29b-41d4-a716-446655440000',
      hook_event_name: 'PreToolUse',
      tool_name: 'Bash',
      tool_input: { command: 'ls -la' },
      cwd: workspace,
      model: 'claude-3.5',
    };

    const exit = await runHook({
      kernelBin: KERNEL_BIN,
      workspace,
      runId: 'adapter-run-1',
      payload,
    });

    expect(exit).toBe(0);
    const jsonlPath = join(workspace, '.chitin', 'events-adapter-run-1.jsonl');
    expect(existsSync(jsonlPath)).toBe(true);
    const content = readFileSync(jsonlPath, 'utf8').trim();
    const ev = JSON.parse(content);
    expect(ev.tool_name).toBe('Bash');
    expect(ev.surface).toBe('claude-code');

    rmSync(workspace, { recursive: true, force: true });
  });
});
