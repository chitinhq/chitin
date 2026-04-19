import { mkdtempSync, mkdirSync, existsSync, rmSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { describe, it, expect, afterEach } from 'vitest';

// Workspace root — temp dirs must live here so pnpm exec can resolve the
// workspace and find tsx.
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
    // Create temp workspace inside the monorepo so pnpm exec can find tsx.
    const workspace = mkdtempSync(join(WORKSPACE_ROOT, 'adp-'));
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
        cwd,
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
