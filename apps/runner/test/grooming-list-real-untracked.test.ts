import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { listRealUntrackedFiles } from '../src/grooming/apply-workflow-result.ts';

// Build a tiny git repo, drop various untracked files, verify the
// helper distinguishes real-work paths from bootstrap-noise paths.
describe('listRealUntrackedFiles', () => {
  let scratch: string;

  function git(args: string[]): string {
    return execFileSync('git', args, { cwd: scratch, encoding: 'utf8' }).trim();
  }

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'chitin-untracked-test-'));
    git(['init', '--quiet']);
    git(['config', 'user.email', 'test@example.invalid']);
    git(['config', 'user.name', 'test']);
    git(['config', 'commit.gpgsign', 'false']);
    writeFileSync(join(scratch, 'README.md'), 'baseline\n');
    git(['add', 'README.md']);
    git(['commit', '-m', 'baseline', '--quiet']);
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('returns empty when there are no untracked files', () => {
    expect(listRealUntrackedFiles(scratch)).toEqual([]);
  });

  it('filters out .claude/ bootstrap files', () => {
    mkdirSync(join(scratch, '.claude'));
    writeFileSync(join(scratch, '.claude/settings.json'), '{}');
    writeFileSync(join(scratch, '.claude/notes.md'), 'agent notes');
    expect(listRealUntrackedFiles(scratch)).toEqual([]);
  });

  it('returns real-work paths even when bootstrap is also present', () => {
    mkdirSync(join(scratch, '.claude'));
    writeFileSync(join(scratch, '.claude/settings.json'), '{}');
    mkdirSync(join(scratch, 'tools/lint'), { recursive: true });
    writeFileSync(join(scratch, 'tools/lint/role-coverage.ts'), '// new lint rule\n');

    const real = listRealUntrackedFiles(scratch);
    expect(real).toContain('tools/lint/role-coverage.ts');
    expect(real.every((p) => !p.startsWith('.claude/'))).toBe(true);
  });

  it('reports nested new files individually (uses -uall)', () => {
    // The default `git status --porcelain` collapses an untracked
    // directory to a single trailing-slash entry; -uall expands it.
    // The prefix filter relies on path-level granularity, so this
    // test guards the -uall flag from being dropped.
    mkdirSync(join(scratch, 'libs/scheduler/src'), { recursive: true });
    writeFileSync(join(scratch, 'libs/scheduler/src/index.ts'), '');
    writeFileSync(join(scratch, 'libs/scheduler/src/schema.ts'), '');

    const real = listRealUntrackedFiles(scratch);
    expect(real.sort()).toEqual([
      'libs/scheduler/src/index.ts',
      'libs/scheduler/src/schema.ts',
    ]);
  });

  it('respects .git/info/exclude (mirrors WORKTREE_INDEX.md handling)', () => {
    // WORKTREE_INDEX.md is added to info/exclude by the worker; this
    // test confirms that pattern flows through — anything excluded
    // there never reaches the helper, so no prefix entry is needed.
    mkdirSync(join(scratch, '.git/info'), { recursive: true });
    writeFileSync(join(scratch, '.git/info/exclude'), 'WORKTREE_INDEX.md\n');
    writeFileSync(join(scratch, 'WORKTREE_INDEX.md'), 'auto-generated');
    mkdirSync(join(scratch, 'apps'), { recursive: true });
    writeFileSync(join(scratch, 'apps/foo.ts'), 'real');

    expect(listRealUntrackedFiles(scratch)).toEqual(['apps/foo.ts']);
  });

  it('returns empty array when worktree is invalid (no git failure escape)', () => {
    // Helper must fail-safe rather than throw — apply-step should fall
    // back to the trackedDiff + commits_added backstops on errors.
    expect(listRealUntrackedFiles('/nonexistent/path/abc123')).toEqual([]);
  });
});
