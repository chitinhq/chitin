import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { getSurfaceStatus, installSurface, SURFACES } from './src/installer';

const originalHome = process.env.HOME;
let fakeHome: string | null = null;

function useFakeHome(): string {
  fakeHome = mkdtempSync(join(tmpdir(), 'chitin-cli-boundary-'));
  process.env.HOME = fakeHome;
  return fakeHome;
}

afterEach(() => {
  if (fakeHome) rmSync(fakeHome, { recursive: true, force: true });
  fakeHome = null;
  if (originalHome === undefined) {
    delete process.env.HOME;
  } else {
    process.env.HOME = originalHome;
  }
});

describe('PR boundary coverage for guard/status', () => {
  it('empty boundary: fresh home reports all surfaces missing', () => {
    useFakeHome();

    expect(SURFACES.map((surface) => getSurfaceStatus(surface).installed)).toEqual([
      false,
      false,
      false,
      false,
    ]);
  });

  it('max boundary: repeated guard installs replace managed entries', () => {
    const home = useFakeHome();

    for (let i = 0; i < 5; i++) {
      installSurface('claude-code', `/tmp/chitin-kernel-${i}`);
      installSurface('codex', `/tmp/chitin-kernel-${i}`);
    }

    const claudeSettings = JSON.parse(
      readFileSync(join(home, '.claude', 'settings.json'), 'utf8'),
    ) as { hooks: { PreToolUse: Array<{ _tag?: string }> } };
    expect(claudeSettings.hooks.PreToolUse.filter((entry) => entry._tag === 'chitin'))
      .toHaveLength(1);

    const codexConfig = readFileSync(join(home, '.codex', 'config.toml'), 'utf8');
    expect(codexConfig.match(/# >>> chitin guard \(managed\) >>>/g)).toHaveLength(1);
    expect(codexConfig).toContain('/tmp/chitin-kernel-4');
    expect(codexConfig).not.toContain('/tmp/chitin-kernel-0');
  });

  it('error boundary: explicit disabled codex hook is corrected in place', () => {
    const home = useFakeHome();
    mkdirSync(join(home, '.codex'), { recursive: true });
    writeFileSync(join(home, '.codex', 'config.toml'), '[features]\ncodex_hooks = false\n');

    installSurface('codex', '/tmp/chitin-kernel');

    const codexConfig = readFileSync(join(home, '.codex', 'config.toml'), 'utf8');
    expect(codexConfig).toContain('codex_hooks = true');
    expect(codexConfig).not.toContain('codex_hooks = false');
    expect(codexConfig.match(/codex_hooks\s*=/g)).toHaveLength(1);
  });
});
