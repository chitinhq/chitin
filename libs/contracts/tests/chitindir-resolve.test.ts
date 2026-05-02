import { mkdtempSync, mkdirSync, rmSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { describe, it, expect, afterEach } from 'vitest';
import { resolveChitinDir } from '../src/chitindir-resolve';

describe('resolveChitinDir', () => {
  const originalHome = process.env.HOME;

  // #8: track temp dirs and rm them in afterEach so /tmp doesn't
  // accumulate across CI runs.
  const tempDirs: string[] = [];
  function tempDir(prefix: string): string {
    const d = mkdtempSync(join(tmpdir(), prefix));
    tempDirs.push(d);
    return d;
  }

  afterEach(() => {
    process.env.HOME = originalHome;
    for (const d of tempDirs) rmSync(d, { recursive: true, force: true });
    tempDirs.length = 0;
  });

  it('finds an existing .chitin dir in a parent', () => {
    const root = tempDir('cd-test-');
    const chitin = join(root, '.chitin');
    mkdirSync(chitin);
    const nested = join(root, 'a', 'b');
    mkdirSync(nested, { recursive: true });

    const got = resolveChitinDir(nested, root);
    expect(got).toBe(chitin);
  });

  it('falls back to $HOME/.chitin when none found, creating on-demand', () => {
    // Sandbox the walk-up inside the temp dir so a stray /tmp/.chitin
    // on the host cannot be found. Passing boundary=sandbox stops the
    // walk at the sandbox, exercising the orphan-fallback path.
    const sandbox = tempDir('cd-cwd-');
    const fakeHome = tempDir('cd-home-');
    process.env.HOME = fakeHome;

    const got = resolveChitinDir(sandbox, sandbox);
    const want = join(fakeHome, '.chitin');
    expect(got).toBe(want);
    expect(statSync(want).isDirectory()).toBe(true);
  });

  it('stops at workspace boundary', () => {
    const boundary = tempDir('cd-bound-');
    const outside = dirname(boundary);
    mkdirSync(join(outside, '.chitin'), { recursive: true });
    const nested = join(boundary, 'sub');
    mkdirSync(nested);
    const fakeHome = tempDir('cd-home2-');
    process.env.HOME = fakeHome;

    const got = resolveChitinDir(nested, boundary);
    expect(got).toBe(join(fakeHome, '.chitin'));
  });
});
