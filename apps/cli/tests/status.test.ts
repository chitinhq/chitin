import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { afterAll, describe, expect, it } from 'vitest';
import { getSurfaceStatus, installSurface } from '../src/installer';

const tmpDirs: string[] = [];

function withFakeHome(): string {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-status-'));
  tmpDirs.push(dir);
  process.env.HOME = dir;
  return dir;
}

afterAll(() => {
  for (const dir of tmpDirs) rmSync(dir, { recursive: true, force: true });
});

describe('installSurface/getSurfaceStatus', () => {
  it('installs Claude Code hook config', () => {
    const home = withFakeHome();
    installSurface('claude-code', '/tmp/chitin-kernel');
    const target = join(home, '.claude', 'settings.json');
    expect(JSON.parse(readFileSync(target, 'utf8')).hooks.PreToolUse[0].hooks[0].command)
      .toContain('--agent=claude-code');
    expect(getSurfaceStatus('claude-code').installed).toBe(true);
  });

  it('installs Gemini hook config', () => {
    const home = withFakeHome();
    installSurface('gemini', '/tmp/chitin-kernel');
    const target = join(home, '.gemini', 'settings.json');
    expect(JSON.parse(readFileSync(target, 'utf8')).hooks.BeforeTool[0].hooks[0].command)
      .toContain('--agent=gemini');
    expect(getSurfaceStatus('gemini').installed).toBe(true);
  });

  it('installs Codex hook config', () => {
    const home = withFakeHome();
    mkdirSync(join(home, '.codex'), { recursive: true });
    writeFileSync(join(home, '.codex', 'config.toml'), '', 'utf8');
    installSurface('codex', '/tmp/chitin-kernel');
    expect(readFileSync(join(home, '.codex', 'config.toml'), 'utf8')).toContain('--agent=codex');
    expect(getSurfaceStatus('codex').installed).toBe(true);
  });

  it('installs Copilot wrapper', () => {
    const home = withFakeHome();
    installSurface('copilot', '/tmp/chitin-kernel');
    const target = join(home, '.local', 'bin', 'chitin-copilot');
    expect(readFileSync(target, 'utf8')).toContain('drive copilot');
    expect(getSurfaceStatus('copilot').installed).toBe(true);
  });
});
