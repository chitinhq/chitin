import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { revertWorktreeSettingsArtifact } from '../src/grooming/apply-workflow-result.ts';

// Build a tiny git repo with .claude/settings.json committed, then mutate
// it (simulating writeWorktreeClaudeSettings) and verify the revert puts
// the file back to its original tracked content.
describe('revertWorktreeSettingsArtifact', () => {
  let scratch: string;

  function git(args: string[]): string {
    return execFileSync('git', args, { cwd: scratch, encoding: 'utf8' }).trim();
  }

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'chitin-revert-test-'));
    git(['init', '--quiet']);
    git(['config', 'user.email', 'test@example.invalid']);
    git(['config', 'user.name', 'test']);
    git(['config', 'commit.gpgsign', 'false']);
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
  });

  it('reverts a tracked .claude/settings.json modification', () => {
    mkdirSync(join(scratch, '.claude'));
    const original = JSON.stringify({ extraKnownMarketplaces: { nx: { source: 'github' } } }, null, 2);
    writeFileSync(join(scratch, '.claude/settings.json'), original);
    git(['add', '.claude/settings.json']);
    git(['commit', '-m', 'baseline', '--quiet']);

    // Simulate the worker overwriting the file.
    const overwritten = JSON.stringify({ hooks: { PreToolUse: [{ matcher: '', hooks: [] }] } }, null, 2);
    writeFileSync(join(scratch, '.claude/settings.json'), overwritten);

    revertWorktreeSettingsArtifact(scratch);

    expect(readFileSync(join(scratch, '.claude/settings.json'), 'utf8')).toBe(original);
  });

  it('is a no-op when .claude/settings.json is not modified', () => {
    mkdirSync(join(scratch, '.claude'));
    const original = JSON.stringify({ keep: true });
    writeFileSync(join(scratch, '.claude/settings.json'), original);
    git(['add', '.claude/settings.json']);
    git(['commit', '-m', 'baseline', '--quiet']);

    revertWorktreeSettingsArtifact(scratch);

    expect(readFileSync(join(scratch, '.claude/settings.json'), 'utf8')).toBe(original);
  });

  it('is a no-op when there is no .claude/settings.json at all', () => {
    // Worker may not have run writeWorktreeClaudeSettings (e.g., copilot driver).
    writeFileSync(join(scratch, 'other.txt'), 'hello');
    git(['add', 'other.txt']);
    git(['commit', '-m', 'baseline', '--quiet']);

    // Should not throw.
    expect(() => revertWorktreeSettingsArtifact(scratch)).not.toThrow();
  });

  it('does not touch other modified files', () => {
    mkdirSync(join(scratch, '.claude'));
    const settings = JSON.stringify({ extra: 'main' });
    writeFileSync(join(scratch, '.claude/settings.json'), settings);
    writeFileSync(join(scratch, 'task.ts'), '// original');
    git(['add', '.']);
    git(['commit', '-m', 'baseline', '--quiet']);

    // Worker overwrites both: .claude/settings.json (artifact) and the
    // entry's actual target (task work). We must revert ONLY the
    // settings.json, leaving the task work alone for staging.
    writeFileSync(join(scratch, '.claude/settings.json'), '{"hooks":[]}');
    writeFileSync(join(scratch, 'task.ts'), '// task work edit');

    revertWorktreeSettingsArtifact(scratch);

    expect(readFileSync(join(scratch, '.claude/settings.json'), 'utf8')).toBe(settings);
    expect(readFileSync(join(scratch, 'task.ts'), 'utf8')).toBe('// task work edit');
  });
});
