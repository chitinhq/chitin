import { mkdtempSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, describe, expect, it } from 'vitest';
import { getSurfaceStatus, installSurface, SURFACES } from '../src/installer';

const tmpDirs: string[] = [];

function withFakeHome(): string {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-boundaries-'));
  tmpDirs.push(dir);
  process.env.HOME = dir;
  return dir;
}

afterAll(() => {
  for (const dir of tmpDirs) rmSync(dir, { recursive: true, force: true });
});

describe('CLI install boundary coverage', () => {
  it('empty boundary: a fresh home reports every surface missing without side effects', () => {
    withFakeHome();
    for (const surface of SURFACES) {
      expect(getSurfaceStatus(surface).installed).toBe(false);
    }
  });

  it('max boundary: repeated installs stay idempotent (no accumulating wrappers/blocks)', () => {
    const home = withFakeHome();
    for (let i = 0; i < 5; i++) {
      installSurface('claude-code', '/tmp/chitin-kernel');
      installSurface('codex', '/tmp/chitin-kernel');
    }

    // claude-code: exactly one chitin-tagged PreToolUse wrapper.
    const claudeSettings = JSON.parse(
      readFileSync(join(home, '.claude', 'settings.json'), 'utf8'),
    ) as { hooks: { PreToolUse: Array<{ _tag?: string }> } };
    const chitinWrappers = claudeSettings.hooks.PreToolUse.filter((w) => w._tag === 'chitin');
    expect(chitinWrappers).toHaveLength(1);

    // codex: exactly one managed block, exactly one codex_hooks key.
    const codexConfig = readFileSync(join(home, '.codex', 'config.toml'), 'utf8');
    expect(codexConfig.match(/# >>> chitin guard \(managed\) >>>/g)).toHaveLength(1);
    expect(codexConfig.match(/codex_hooks\s*=/g)).toHaveLength(1);
  });

  it('error boundary: kernel paths with spaces and quotes are shell-quoted, not broken', () => {
    const home = withFakeHome();
    const nastyPath = "/Users/Jane Doe/.chitin/bin/chitin's-kernel";
    installSurface('claude-code', nastyPath);
    installSurface('copilot', nastyPath);

    const claudeCmd = JSON.parse(readFileSync(join(home, '.claude', 'settings.json'), 'utf8'))
      .hooks.PreToolUse[0].hooks[0].command as string;
    // POSIX single-quoting: the literal path is wrapped and embedded `'`
    // become the `'\''` escape — the raw path never sits unquoted.
    expect(claudeCmd.startsWith("'/Users/Jane Doe/.chitin/bin/chitin")).toBe(true);
    expect(claudeCmd).toContain(`'\\''`);
    expect(claudeCmd).toContain('--agent=claude-code');

    const wrapper = join(home, '.local', 'bin', 'chitin-copilot');
    expect(readFileSync(wrapper, 'utf8')).toContain(`'/Users/Jane Doe/.chitin/bin/chitin'\\''s-kernel'`);
    // The copilot wrapper is always made executable, even on rewrite.
    expect(statSync(wrapper).mode & 0o111).not.toBe(0);
  });

  it('error boundary: an explicit codex_hooks = false is flipped, not duplicated', () => {
    const home = withFakeHome();
    mkdirSync(join(home, '.codex'), { recursive: true });
    writeFileSync(join(home, '.codex', 'config.toml'), '[features]\ncodex_hooks = false\n', 'utf8');
    installSurface('codex', '/tmp/chitin-kernel');
    const config = readFileSync(join(home, '.codex', 'config.toml'), 'utf8');
    expect(config).toContain('codex_hooks = true');
    expect(config).not.toContain('codex_hooks = false');
    expect(config.match(/codex_hooks\s*=/g)).toHaveLength(1);
  });
});
