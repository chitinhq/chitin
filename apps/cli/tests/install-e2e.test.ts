import { mkdtempSync, readFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { describe, it, expect } from 'vitest';

const repoRoot = join(__dirname, '..', '..', '..');
const kernelBin = join(repoRoot, 'dist/go/execution-kernel/chitin-kernel');
const cliEntry = join(repoRoot, 'apps/cli/src/main.ts');

function run(args: string[], env: Record<string, string>) {
  return spawnSync('pnpm', ['exec', 'tsx', cliEntry, ...args], {
    encoding: 'utf8',
    env: { ...process.env, ...env },
  });
}

describe('chitin install --surface claude-code --global (e2e)', () => {
  it('writes hooks into throwaway HOME and uninstall removes them cleanly', () => {
    if (!existsSync(kernelBin)) {
      console.warn(`skipping e2e: ${kernelBin} missing. Run: pnpm nx build execution-kernel`);
      return;
    }
    const fakeHome = mkdtempSync(join(tmpdir(), 'chitin-e2e-'));
    const fakeAdapter = '/tmp/fake-adapter-bin';

    const installRes = run(
      ['install', '--surface', 'claude-code', '--global', '--adapter', fakeAdapter],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    expect(installRes.status).toBe(0);

    const settingsPath = join(fakeHome, '.claude', 'settings.json');
    expect(existsSync(settingsPath)).toBe(true);
    const s = JSON.parse(readFileSync(settingsPath, 'utf8'));
    for (const h of ['SessionStart', 'PreToolUse', 'PostToolUse', 'SessionEnd']) {
      const list = s.hooks[h] ?? [];
      expect(list.some((e: { command: string }) => e.command === fakeAdapter)).toBe(true);
    }

    const uninstallRes = run(
      ['uninstall', '--surface', 'claude-code', '--global', '--adapter', fakeAdapter],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    expect(uninstallRes.status).toBe(0);

    const s2 = JSON.parse(readFileSync(settingsPath, 'utf8'));
    for (const h of ['SessionStart', 'PreToolUse', 'PostToolUse', 'SessionEnd']) {
      const list = s2.hooks?.[h] ?? [];
      expect(list.some((e: { command: string }) => e.command === fakeAdapter)).toBe(false);
    }
  });
});
