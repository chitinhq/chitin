import { mkdtempSync, mkdirSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, dirname } from 'node:path';
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { resolveChitinDir } from './chitindir-resolve';

describe('resolveChitinDir', () => {
  const originalHome = process.env.HOME;

  afterEach(() => {
    process.env.HOME = originalHome;
  });

  it('finds an existing .chitin dir in a parent', () => {
    const root = mkdtempSync(join(tmpdir(), 'cd-test-'));
    const chitin = join(root, '.chitin');
    mkdirSync(chitin);
    const nested = join(root, 'a', 'b');
    mkdirSync(nested, { recursive: true });

    const got = resolveChitinDir(nested, root);
    expect(got).toBe(chitin);
  });

  it('falls back to $HOME/.chitin when none found, creating on-demand', () => {
    // Sandbox the walk-up inside a fresh temp dir so stray /tmp/.chitin on the
    // host (from earlier runs) cannot be found. Passing boundary=sandbox makes
    // the walk stop at the sandbox, exercising the orphan-fallback path.
    const sandbox = mkdtempSync(join(tmpdir(), 'cd-cwd-'));
    const fakeHome = mkdtempSync(join(tmpdir(), 'cd-home-'));
    process.env.HOME = fakeHome;

    const got = resolveChitinDir(sandbox, sandbox);
    const want = join(fakeHome, '.chitin');
    expect(got).toBe(want);
    expect(statSync(want).isDirectory()).toBe(true);
  });

  it('stops at workspace boundary', () => {
    const boundary = mkdtempSync(join(tmpdir(), 'cd-bound-'));
    const outside = dirname(boundary);
    mkdirSync(join(outside, '.chitin'), { recursive: true });
    const nested = join(boundary, 'sub');
    mkdirSync(nested);
    const fakeHome = mkdtempSync(join(tmpdir(), 'cd-home2-'));
    process.env.HOME = fakeHome;

    const got = resolveChitinDir(nested, boundary);
    expect(got).toBe(join(fakeHome, '.chitin'));
  });
});
