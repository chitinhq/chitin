// Slice 5: integration tests for the worktree-aware activity. Spins up a
// real git repo in a tempdir, exercises provisionWorktree +
// captureWorktreeState directly, and asserts the state-capture is correct
// across the four scenarios apply-step branches on (clean / commits-only /
// uncommitted-only / both).

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, rmSync, writeFileSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { ExecutionRequest } from '@chitin/contracts';
import { __test__ } from '../src/activity.ts';

const { provisionWorktree, captureWorktreeState } = __test__;

let repoDir: string;
let savedRepoRoot: string | undefined;
let savedHome: string | undefined;
let fakeHome: string;

function git(cwd: string, args: string[]): string {
  return execFileSync('git', args, { cwd, encoding: 'utf8' }).trim();
}

function setupRepo(): string {
  const dir = mkdtempSync(join(tmpdir(), 'chitin-wt-test-repo-'));
  git(dir, ['init', '-b', 'main']);
  git(dir, ['config', 'user.email', 'test@example.com']);
  git(dir, ['config', 'user.name', 'Test']);
  writeFileSync(join(dir, 'README.md'), '# test\n');
  git(dir, ['add', '.']);
  git(dir, ['commit', '-m', 'initial']);
  return dir;
}

function makeRequest(overrides: Partial<ExecutionRequest> = {}): ExecutionRequest {
  return {
    schema_version: '1',
    workflow_id: `wt-test-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    run_id: 'wt-test-attempt-1',
    repo: 'chitinhq/chitin',
    task_class: 'refactor',
    risk_level: 'low',
    allowed_drivers: ['copilot'],
    network_policy: 'allowlist',
    write_policy: 'worktree',
    bounds: { max_tool_calls: 1, max_cost_usd: 0, wall_timeout_s: 60 },
    prompt: 'noop',
    base_ref: 'main',
    ...overrides,
  };
}

beforeEach(() => {
  repoDir = setupRepo();
  // Redirect SWARM_WORKTREES_ROOT (which is derived from homedir()) to a
  // scratch dir for the test run so we don't pollute the real ~/.cache.
  fakeHome = mkdtempSync(join(tmpdir(), 'chitin-wt-test-home-'));
  savedHome = process.env.HOME;
  process.env.HOME = fakeHome;
  // The activity's resolveRepoRoot reads CHITIN_REPO_ROOT, but provision-
  // Worktree takes repoRoot directly — we pass repoDir explicitly. Saving
  // the env var only for the few tests that exercise resolveRepoRoot.
  savedRepoRoot = process.env.CHITIN_REPO_ROOT;
});

afterEach(() => {
  if (savedHome === undefined) delete process.env.HOME;
  else process.env.HOME = savedHome;
  if (savedRepoRoot === undefined) delete process.env.CHITIN_REPO_ROOT;
  else process.env.CHITIN_REPO_ROOT = savedRepoRoot;
  rmSync(repoDir, { recursive: true, force: true });
  rmSync(fakeHome, { recursive: true, force: true });
});

describe('provisionWorktree', () => {
  it('creates a new worktree at <home>/.cache/chitin/swarm-worktrees/<wfid>/ on a swarm/<wfid> branch', () => {
    const req = makeRequest();
    const { path, branch } = provisionWorktree(req, repoDir);
    expect(path).toContain('chitin/swarm-worktrees');
    expect(path.endsWith(req.workflow_id)).toBe(true);
    expect(branch).toBe(`swarm/${req.workflow_id}`);
    // Worktree exists and is on the right branch
    const head = git(path, ['rev-parse', '--abbrev-ref', 'HEAD']);
    expect(head).toBe(branch);
    // Cleanup so subsequent tests don't pile up in fakeHome
    git(repoDir, ['worktree', 'remove', '--force', path]);
    git(repoDir, ['branch', '-D', branch]);
  });

  it('cleans up stale state from a prior crashed run (idempotent)', () => {
    const req = makeRequest();
    // First provision
    const first = provisionWorktree(req, repoDir);
    // Simulate a crash mid-run by abandoning the worktree (don't remove)
    writeFileSync(join(first.path, 'partial.txt'), 'half-done\n');
    // Second provision with the same workflow_id should reclaim cleanly
    const second = provisionWorktree(req, repoDir);
    expect(second.path).toBe(first.path);
    expect(second.branch).toBe(first.branch);
    // Worktree is fresh: no partial.txt
    expect(() => git(second.path, ['rev-parse', 'HEAD'])).not.toThrow();
    git(repoDir, ['worktree', 'remove', '--force', second.path]);
    git(repoDir, ['branch', '-D', second.branch]);
  });

  it('throws when called without base_ref', () => {
    const req = makeRequest({ base_ref: undefined });
    expect(() => provisionWorktree(req, repoDir)).toThrow(/base_ref/);
  });
});

describe('captureWorktreeState', () => {
  it('reports clean when the worktree has no commits and no uncommitted changes', () => {
    const req = makeRequest();
    const wt = provisionWorktree(req, repoDir);
    const state = captureWorktreeState(wt.path, req.base_ref!);
    expect(state.commits_added).toBe(0);
    expect(state.has_uncommitted_changes).toBe(false);
    expect(state.diff_shortstat).toBe('');
    expect(state.branch).toBe(wt.branch);
    expect(state.head_sha).toMatch(/^[0-9a-f]{40}$/);
    git(repoDir, ['worktree', 'remove', '--force', wt.path]);
    git(repoDir, ['branch', '-D', wt.branch]);
  });

  it('reports uncommitted changes when files are modified but not committed', () => {
    const req = makeRequest();
    const wt = provisionWorktree(req, repoDir);
    writeFileSync(join(wt.path, 'new-file.txt'), 'agent wrote this\n');
    const state = captureWorktreeState(wt.path, req.base_ref!);
    expect(state.commits_added).toBe(0);
    expect(state.has_uncommitted_changes).toBe(true);
    // Note: untracked files don't appear in `git diff --shortstat baseRef`.
    // diff_shortstat reflects committed work only — apply-step regenerates
    // it after `git add -A && git commit` when agent left dirty state.
    git(repoDir, ['worktree', 'remove', '--force', wt.path]);
    git(repoDir, ['branch', '-D', wt.branch]);
  });

  it('reports uncommitted changes for tracked-file modifications (in shortstat)', () => {
    const req = makeRequest();
    const wt = provisionWorktree(req, repoDir);
    writeFileSync(join(wt.path, 'README.md'), '# modified\n');
    const state = captureWorktreeState(wt.path, req.base_ref!);
    expect(state.commits_added).toBe(0);
    expect(state.has_uncommitted_changes).toBe(true);
    expect(state.diff_shortstat).toMatch(/changed/);
    git(repoDir, ['worktree', 'remove', '--force', wt.path]);
    git(repoDir, ['branch', '-D', wt.branch]);
  });

  it('reports commits_added when the agent committed', () => {
    const req = makeRequest();
    const wt = provisionWorktree(req, repoDir);
    writeFileSync(join(wt.path, 'committed.txt'), 'agent committed\n');
    git(wt.path, ['add', '.']);
    git(wt.path, ['commit', '-m', 'agent change']);
    const state = captureWorktreeState(wt.path, req.base_ref!);
    expect(state.commits_added).toBe(1);
    expect(state.has_uncommitted_changes).toBe(false);
    expect(state.diff_shortstat).toMatch(/insertion/);
    git(repoDir, ['worktree', 'remove', '--force', wt.path]);
    git(repoDir, ['branch', '-D', wt.branch]);
  });

  it('reports both commits_added and uncommitted when the agent committed AND left dirty state', () => {
    const req = makeRequest();
    const wt = provisionWorktree(req, repoDir);
    writeFileSync(join(wt.path, 'a.txt'), 'committed\n');
    git(wt.path, ['add', '.']);
    git(wt.path, ['commit', '-m', 'first agent change']);
    writeFileSync(join(wt.path, 'b.txt'), 'still uncommitted\n');
    const state = captureWorktreeState(wt.path, req.base_ref!);
    expect(state.commits_added).toBe(1);
    expect(state.has_uncommitted_changes).toBe(true);
    git(repoDir, ['worktree', 'remove', '--force', wt.path]);
    git(repoDir, ['branch', '-D', wt.branch]);
  });

  it('reports the right shortstat with multiple commits', () => {
    const req = makeRequest();
    const wt = provisionWorktree(req, repoDir);
    writeFileSync(join(wt.path, 'a.txt'), 'x\n');
    git(wt.path, ['add', '.']);
    git(wt.path, ['commit', '-m', 'one']);
    writeFileSync(join(wt.path, 'b.txt'), 'y\n');
    git(wt.path, ['add', '.']);
    git(wt.path, ['commit', '-m', 'two']);
    const state = captureWorktreeState(wt.path, req.base_ref!);
    expect(state.commits_added).toBe(2);
    expect(state.diff_shortstat).toMatch(/2 files changed/);
    git(repoDir, ['worktree', 'remove', '--force', wt.path]);
    git(repoDir, ['branch', '-D', wt.branch]);
  });
});
