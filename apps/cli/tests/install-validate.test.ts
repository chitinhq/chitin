import { mkdtempSync, writeFileSync, chmodSync, unlinkSync, rmdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, it, expect, afterAll } from 'vitest';
import { validateAdapterPath, hasShellMetacharacters } from '../src/commands/install';

const tmpDirs: string[] = [];

function makeTmpDir(): string {
  const d = mkdtempSync(join(tmpdir(), 'chitin-validate-'));
  tmpDirs.push(d);
  return d;
}

afterAll(() => {
  for (const d of tmpDirs) {
    try {
      rmdirSync(d, { recursive: true });
    } catch {
      /* best effort */
    }
  }
});

describe('validateAdapterPath', () => {
  it('passes for absolute path to existing executable', () => {
    const dir = makeTmpDir();
    const file = join(dir, 'adapter');
    writeFileSync(file, '#!/bin/sh\n');
    chmodSync(file, 0o755);
    // Should not throw
    expect(() => validateAdapterPath(file)).not.toThrow();
  });

  it('rejects relative path', () => {
    expect(() => validateAdapterPath('./bin/adapter')).toThrow(/absolute/i);
    expect(() => validateAdapterPath('bin/adapter')).toThrow(/absolute/i);
  });

  it('rejects non-existent path', () => {
    expect(() => validateAdapterPath('/nonexistent/path/to/adapter')).toThrow(
      /exist/i,
    );
  });

  it('rejects non-executable file', () => {
    const dir = makeTmpDir();
    const file = join(dir, 'not-exec');
    writeFileSync(file, 'data');
    chmodSync(file, 0o644);
    expect(() => validateAdapterPath(file)).toThrow(/executable/i);
  });

  it('rejects empty string', () => {
    expect(() => validateAdapterPath('')).toThrow(/empty/i);
  });

  it('rejects shell metacharacters', () => {
    expect(() => validateAdapterPath('/bin/echo; rm -rf /')).toThrow(
      /metacharacter/i,
    );
    expect(() => validateAdapterPath('/bin/echo && true')).toThrow(
      /metacharacter/i,
    );
    expect(() => validateAdapterPath('/bin/echo | tee log')).toThrow(
      /metacharacter/i,
    );
    expect(() => validateAdapterPath('/bin/echo `whoami`')).toThrow(
      /metacharacter/i,
    );
    expect(() => validateAdapterPath('/bin/echo $(whoami)')).toThrow(
      /metacharacter/i,
    );
    expect(() => validateAdapterPath('/bin/echo\nmalicious')).toThrow(
      /metacharacter/i,
    );
  });

  it('allows paths with spaces but no metacharacters', () => {
    // We can't easily create a file with spaces in a temp path for the exists check,
    // so we test the metacharacter check in isolation via hasShellMetacharacters
    expect(hasShellMetacharacters('/path with spaces/bin/adapter')).toBe(false);
  });
});

describe('hasShellMetacharacters', () => {
  it('detects semicolon', () => {
    expect(hasShellMetacharacters('cmd; other')).toBe(true);
  });

  it('detects ampersand', () => {
    expect(hasShellMetacharacters('cmd && other')).toBe(true);
  });

  it('detects pipe', () => {
    expect(hasShellMetacharacters('cmd | other')).toBe(true);
  });

  it('detects backtick', () => {
    expect(hasShellMetacharacters('cmd `whoami`')).toBe(true);
  });

  it('detects command substitution', () => {
    expect(hasShellMetacharacters('cmd $(whoami)')).toBe(true);
  });

  it('detects newline', () => {
    expect(hasShellMetacharacters('cmd\nother')).toBe(true);
  });

  it('detects carriage return', () => {
    expect(hasShellMetacharacters('cmd\rother')).toBe(true);
  });

  it('passes clean paths', () => {
    expect(hasShellMetacharacters('/usr/local/bin/adapter')).toBe(false);
    expect(hasShellMetacharacters('/path with spaces/bin/adapter')).toBe(false);
    expect(hasShellMetacharacters('/home/user.d/bin-adapter')).toBe(false);
  });
});