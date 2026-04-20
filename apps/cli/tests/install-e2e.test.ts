import {
  mkdtempSync,
  readFileSync,
  existsSync,
  mkdirSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { describe, it, expect } from 'vitest';

const repoRoot = join(__dirname, '..', '..', '..');
const kernelBin = join(repoRoot, 'dist/go/execution-kernel/chitin-kernel');
const cliEntry = join(repoRoot, 'apps/cli/src/main.ts');
const tsxBin = join(repoRoot, 'node_modules/.bin/tsx');

function run(args: string[], env: Record<string, string>) {
  return spawnSync(tsxBin, [cliEntry, ...args], {
    encoding: 'utf8',
    env: { ...process.env, ...env },
  });
}

describe('chitin install --surface claude-code --global (e2e)', () => {
  it('writes matcher-wrapper hooks into throwaway HOME and uninstall removes them cleanly', () => {
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
    const s = JSON.parse(readFileSync(settingsPath, 'utf8')) as {
      hooks?: Record<
        string,
        Array<{ _tag?: string; hooks?: Array<{ command?: string }> }>
      >;
    };
    for (const h of [
      'SessionStart',
      'UserPromptSubmit',
      'PreToolUse',
      'PostToolUse',
      'SessionEnd',
    ]) {
      const list = s.hooks?.[h] ?? [];
      const hit = list.some(
        (w) =>
          w._tag === 'chitin' &&
          (w.hooks ?? []).some((e) => e.command === fakeAdapter),
      );
      expect(hit).toBe(true);
    }

    const uninstallRes = run(
      ['uninstall', '--surface', 'claude-code', '--global'],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    expect(uninstallRes.status).toBe(0);

    const s2 = JSON.parse(readFileSync(settingsPath, 'utf8')) as {
      hooks?: Record<
        string,
        Array<{ _tag?: string; hooks?: Array<{ command?: string }> }>
      >;
    };
    // The hooks key is removed entirely when all chitin wrappers are gone.
    expect(s2.hooks).toBeUndefined();
  });

  it('preserves pre-existing non-chitin hooks across install and uninstall', () => {
    if (!existsSync(kernelBin)) {
      console.warn(`skipping e2e: ${kernelBin} missing.`);
      return;
    }
    const fakeHome = mkdtempSync(join(tmpdir(), 'chitin-e2e-preserve-'));
    const settingsDir = join(fakeHome, '.claude');
    const settingsPath = join(settingsDir, 'settings.json');
    mkdirSync(settingsDir);
    writeFileSync(
      settingsPath,
      JSON.stringify({
        theme: 'dark',
        hooks: {
          PreToolUse: [
            { matcher: '', hooks: [{ type: 'command', command: '/usr/local/bin/other-tool' }] },
          ],
        },
      }),
      'utf8',
    );

    const installRes = run(
      ['install', '--surface', 'claude-code', '--global', '--adapter', '/tmp/fake-adapter-2'],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    expect(installRes.status).toBe(0);

    const uninstallRes = run(
      ['uninstall', '--surface', 'claude-code', '--global'],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    expect(uninstallRes.status).toBe(0);

    const s = JSON.parse(readFileSync(settingsPath, 'utf8')) as {
      theme?: string;
      hooks?: Record<string, Array<{ hooks?: Array<{ command?: string }> }>>;
    };
    expect(s.theme).toBe('dark');
    const pre = s.hooks?.PreToolUse ?? [];
    expect(pre.length).toBe(1);
    expect(pre[0]?.hooks?.[0]?.command).toBe('/usr/local/bin/other-tool');
  });
});
