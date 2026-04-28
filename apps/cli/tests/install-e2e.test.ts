import {
  mkdtempSync,
  readFileSync,
  existsSync,
  mkdirSync,
  writeFileSync,
  chmodSync,
  rmSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { describe, it, expect, afterAll } from 'vitest';

const repoRoot = join(__dirname, '..', '..', '..');
const kernelBin = join(repoRoot, 'dist/go/execution-kernel/chitin-kernel');
const cliEntry = join(repoRoot, 'apps/cli/src/main.ts');
const tsxBin = join(repoRoot, 'node_modules/.bin/tsx');

const tmpDirs: string[] = [];
function makeTmpDir(): string {
  const d = mkdtempSync(join(tmpdir(), 'chitin-e2e-'));
  tmpDirs.push(d);
  return d;
}
afterAll(() => {
  for (const d of tmpDirs) {
    try { rmSync(d, { recursive: true }); } catch { /* best effort */ }
  }
});

function makeFakeAdapter(dir: string): string {
  const file = join(dir, 'adapter');
  writeFileSync(file, '#!/bin/sh\nexit 0\n');
  chmodSync(file, 0o755);
  return file;
}

function run(args: string[], env: Record<string, string>) {
  return spawnSync(tsxBin, [cliEntry, ...args], {
    encoding: 'utf8',
    env: { ...process.env, ...env },
  });
}

// In worktree checkouts that haven't run pnpm install, tsx can't resolve
// workspace packages. Detect and skip gracefully.
function skipIfWorkspaceBroken(res: { stderr: string | null }): boolean {
  return (res.stderr ?? '').includes('ERR_MODULE_NOT_FOUND');
}

describe('chitin install --surface claude-code --global (e2e)', () => {
  it('writes matcher-wrapper hooks into throwaway HOME and uninstall removes them cleanly', () => {
    if (!existsSync(kernelBin)) {
      console.warn(`skipping e2e: ${kernelBin} missing. Run: pnpm nx build execution-kernel`);
      return;
    }
    const fakeHome = makeTmpDir();
    const fakeAdapter = makeFakeAdapter(fakeHome);

    const installRes = run(
      ['install', '--surface', 'claude-code', '--global', '--adapter', fakeAdapter],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    if (skipIfWorkspaceBroken(installRes)) return;
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
    expect(s2.hooks).toBeUndefined();
  });

  it('preserves pre-existing non-chitin hooks across install and uninstall', () => {
    if (!existsSync(kernelBin)) {
      console.warn(`skipping e2e: ${kernelBin} missing.`);
      return;
    }
    const fakeHome = makeTmpDir();
    const fakeAdapter = makeFakeAdapter(fakeHome);
    const settingsDir = join(fakeHome, '.claude');
    const settingsPath = join(settingsDir, 'settings.json');
    mkdirSync(settingsDir, { recursive: true });
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
      ['install', '--surface', 'claude-code', '--global', '--adapter', fakeAdapter],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    if (skipIfWorkspaceBroken(installRes)) return;
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

  it('rejects --adapter with relative path', () => {
    const fakeHome = makeTmpDir();
    const res = run(
      ['install', '--surface', 'claude-code', '--global', '--adapter', './bin/adapter'],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    if (skipIfWorkspaceBroken(res)) return;
    expect(res.status).not.toBe(0);
    expect(res.stderr).toMatch(/absolute/i);
  });

  it('rejects --adapter with non-existent path', () => {
    const fakeHome = makeTmpDir();
    const res = run(
      ['install', '--surface', 'claude-code', '--global', '--adapter', '/nonexistent/binary'],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    if (skipIfWorkspaceBroken(res)) return;
    expect(res.status).not.toBe(0);
    expect(res.stderr).toMatch(/exist/i);
  });

  it('rejects --adapter with shell metacharacters', () => {
    const fakeHome = makeTmpDir();
    const res = run(
      ['install', '--surface', 'claude-code', '--global', '--adapter', '/bin/echo; rm -rf /'],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    if (skipIfWorkspaceBroken(res)) return;
    expect(res.status).not.toBe(0);
    expect(res.stderr).toMatch(/metacharacter|shell/i);
  });

  it('allows shell command via --adapter-shell with warning', () => {
    const fakeHome = makeTmpDir();
    const res = run(
      ['install', '--surface', 'claude-code', '--global', '--adapter-shell', '--adapter', '/bin/echo && true'],
      { HOME: fakeHome, CHITIN_KERNEL_BINARY: kernelBin },
    );
    if (skipIfWorkspaceBroken(res)) return;
    expect(res.stderr).toMatch(/WARNING.*adapter-shell/i);
  });
});