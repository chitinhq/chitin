import { mkdtempSync, mkdirSync, existsSync, rmSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { spawnSync } from 'node:child_process';
import { describe, it, expect, afterEach } from 'vitest';

// Spawn from workspace root so `pnpm exec tsx` can resolve the tsx binary.
// The hook's cwd (inside the hook input JSON) is a separate /tmp dir —
// the resolver walks up from there.
const WORKSPACE_ROOT = resolve(__dirname, '../../../../');

const tempDirs: string[] = [];

afterEach(() => {
  for (const d of tempDirs.splice(0)) {
    rmSync(d, { recursive: true, force: true });
  }
});

function adapterEntry(): string {
  // Resolve to the bin file under test.
  return join(__dirname, 'cli.ts');
}

describe('claude-code adapter CLI bin', () => {
  it('resolves chitinDir from cwd via walk-up, emits an event', () => {
    const workspace = mkdtempSync(join(tmpdir(), 'adp-'));
    tempDirs.push(workspace);
    mkdirSync(join(workspace, '.chitin'));
    const cwd = join(workspace, 'a', 'b');
    mkdirSync(cwd, { recursive: true });

    const hookInput = JSON.stringify({
      hook_event_name: 'SessionStart',
      session_id: 'sess-test-1',
      cwd,
    });

    const res = spawnSync(
      'pnpm',
      ['exec', 'tsx', adapterEntry()],
      {
        input: hookInput,
        cwd: WORKSPACE_ROOT,
        encoding: 'utf8',
        env: {
          ...process.env,
          CHITIN_KERNEL_BINARY: process.env.CHITIN_KERNEL_BINARY ?? 'chitin-kernel',
        },
      },
    );
    expect(res.status).toBe(0);
    expect(existsSync(join(workspace, '.chitin'))).toBe(true);
  });
});
