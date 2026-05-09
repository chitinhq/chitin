import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { initClaudeCodeCommand } from '../src/commands/init-claude-code';
import {
  readFileSync,
  writeFileSync,
  existsSync,
  mkdirSync,
} from 'node:fs';
import { join, resolve } from 'node:path';
import { homedir } from 'node:os';

vi.mock('node:fs');
vi.mock('node:os', () => ({ homedir: () => '/fake/home' }));

describe('initClaudeCodeCommand', () => {
  let files: Map<string, string>;

  beforeEach(() => {
    files = new Map();

    vi.mocked(existsSync).mockImplementation((p) => {
      const path = String(p);
      // Kernel binary always exists in tests
      if (path.endsWith('chitin-kernel')) return true;
      return files.has(path);
    });

    vi.mocked(readFileSync).mockImplementation((p) => {
      const path = String(p);
      const content = files.get(path);
      if (content === undefined) throw new Error(`ENOENT: ${path}`);
      return content;
    });

    vi.mocked(writeFileSync).mockImplementation((p, data) => {
      const path = String(p);
      files.set(path, typeof data === 'string' ? data : String(data));
    });

    vi.mocked(mkdirSync).mockImplementation(() => '');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('creates settings.json with PreToolUse hook when none exists', () => {
    const workspace = '/fake/workspace';

    initClaudeCodeCommand({ workspace });

    // Find the settings.json write
    const settingsPath = join('/fake/home', '.claude', 'settings.json');
    const settingsWrite = Array.from(files.keys()).find((k) =>
      k.endsWith('settings.json'),
    );
    expect(settingsWrite).toBeTruthy();

    const settings = JSON.parse(files.get(settingsWrite!)!);
    expect(settings.hooks.PreToolUse).toHaveLength(1);
    expect(settings.hooks.PreToolUse[0]._tag).toBe('chitin-v2');
  });

  it('replaces existing chitin-v2 entry idempotently', () => {
    const workspace = '/fake/workspace';
    const settingsPath = join('/fake/home', '.claude', 'settings.json');

    // Pre-populate settings with an old chitin-v2 entry
    files.set(
      settingsPath,
      JSON.stringify({
        hooks: {
          PreToolUse: [{ _tag: 'chitin-v2', matcher: 'old', hooks: [{ type: 'command', command: '/old/bin' }] }],
        },
      }),
    );

    initClaudeCodeCommand({ workspace });

    const settings = JSON.parse(files.get(settingsPath)!);
    expect(settings.hooks.PreToolUse).toHaveLength(1);
    expect(settings.hooks.PreToolUse[0]._tag).toBe('chitin-v2');
    // Command should point to the new workspace
    expect(settings.hooks.PreToolUse[0].hooks[0].command).toContain('fake');
  });

  it('appends chitin-v2 to existing PreToolUse hooks', () => {
    const workspace = '/fake/workspace';
    const settingsPath = join('/fake/home', '.claude', 'settings.json');

    // Pre-populate with an unrelated hook
    files.set(
      settingsPath,
      JSON.stringify({
        hooks: {
          PreToolUse: [{ _tag: 'other-hook', hooks: [{ type: 'command', command: '/other' }] }],
        },
      }),
    );

    initClaudeCodeCommand({ workspace });

    const settings = JSON.parse(files.get(settingsPath)!);
    expect(settings.hooks.PreToolUse).toHaveLength(2);
    expect(settings.hooks.PreToolUse[1]._tag).toBe('chitin-v2');
  });

  it('throws if settings.json is invalid JSON', () => {
    const settingsPath = join('/fake/home', '.claude', 'settings.json');
    files.set(settingsPath, 'not-json{{{');

    expect(() =>
      initClaudeCodeCommand({ workspace: '/fake/workspace' }),
    ).toThrow(/not valid JSON/);
  });

  it('throws if kernel binary is missing', () => {
    vi.mocked(existsSync).mockReturnValue(false);

    expect(() =>
      initClaudeCodeCommand({ workspace: '/nonexistent' }),
    ).toThrow(/kernel binary not built/);
  });

  it('writes init.json in .chitin directory', () => {
    const workspace = '/fake/workspace';
    initClaudeCodeCommand({ workspace });

    const initJsonPath = Array.from(files.keys()).find((k) =>
      k.endsWith('init.json'),
    );
    expect(initJsonPath).toBeTruthy();

    const initJson = JSON.parse(files.get(initJsonPath!)!);
    expect(initJson.surface).toBe('claude-code');
    expect(initJson.kernelBin).toBeTruthy();
  });
});