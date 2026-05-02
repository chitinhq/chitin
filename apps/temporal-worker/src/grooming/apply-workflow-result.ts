// Slice 5: post-process for swarm-worktree workflow results.
//
// The activity returns an ActivityResult that may contain a `worktree`
// field with branch/head/diff state. This script consumes that result
// and decides what to do with the agent's work:
//
//   1. No worktree info: nothing to do (slice 1-4 behavior — tempdir mode).
//   2. Worktree clean (no commits, no uncommitted): agent didn't change
//      anything. Clean up worktree and exit. Often a sign of a failed
//      session or the agent declining.
//   3. Worktree has commits but no uncommitted: agent committed its work.
//      Push branch, open PR with commit-derived title/body.
//   4. Worktree has uncommitted changes (regardless of commits): we
//      auto-commit the leftovers with a generated message, then push + PR.
//
// MVP: --dry-run by default. --apply pushes and opens PR. No retries.

import { readFileSync } from 'node:fs';
import { execFileSync, execSync } from 'node:child_process';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { ActivityResult, WorktreeResult } from '../activity-types.ts';

interface ResultEnvelope {
  workflow_id: string;
  result: ActivityResult;
  // Optional human/agent-provided PR metadata. The grooming layer can pass
  // these through; for direct task dispatch, the operator can craft them.
  pr_title?: string;
  pr_body?: string;
}

async function main() {
  const reportPath = readFlag('--result');
  if (!reportPath) {
    console.error('usage: apply-workflow-result --result <path> [--apply] [--repo-root <path>]');
    process.exit(2);
  }
  const dryRun = !process.argv.includes('--apply');
  const repoRoot = resolve(readFlag('--repo-root') ?? process.cwd());

  const env: ResultEnvelope = JSON.parse(readFileSync(reportPath, 'utf8'));
  const wt = env.result.worktree;
  if (!wt) {
    console.log('[apply-result] no worktree in result — nothing to push (tempdir mode).');
    return;
  }
  console.log(`[apply-result] worktree=${wt.path}`);
  console.log(`  branch=${wt.branch}  head=${wt.head_sha.slice(0, 8)}`);
  console.log(`  commits_added=${wt.commits_added}  uncommitted=${wt.has_uncommitted_changes}`);
  console.log(`  shortstat: ${wt.diff_shortstat || '(no changes)'}`);

  if (wt.commits_added === 0 && !wt.has_uncommitted_changes) {
    console.log('[apply-result] worktree is clean — agent made no changes.');
    if (!dryRun) {
      cleanupWorktree(repoRoot, wt);
    }
    return;
  }

  if (dryRun) {
    console.log('\n[apply-result] DRY RUN — would:');
    if (wt.has_uncommitted_changes) {
      console.log(`  - git add -A && git commit -m "${defaultCommitMessage(env)}"  (in ${wt.path})`);
    }
    console.log(`  - git push -u origin ${wt.branch}  (from ${wt.path})`);
    console.log(`  - gh pr create --title "${env.pr_title ?? defaultPrTitle(env)}" --body <body>`);
    console.log(`  - git worktree remove ${wt.path}  (cleanup)`);
    console.log('\n[apply-result] re-run with --apply to perform.');
    return;
  }

  // Slice 7 finding: don't auto-commit just because porcelain shows
  // uncommitted state. Some agents (openclaw qwen via the slice-6b
  // workspace remap) write bootstrap/identity files into the worktree
  // at session start that aren't related to the assigned task — auto-
  // committing those produces a junk PR with no relationship to the
  // backlog entry. Heuristic: only auto-commit when there are TRACKED
  // file modifications (`diff --shortstat` non-empty). Untracked-only
  // changes are likely agent side effects, not the work product.
  //
  // 2026-05-02 finding (bucket-B contamination): claude-code-headless
  // worktrees ALWAYS show a tracked diff on `.claude/settings.json` —
  // the worker's writeWorktreeClaudeSettings overwrites whatever main
  // has there with the chitin gate hook config. That diff is byte-
  // identical across runs and is not the agent's task work. Reverting
  // it before the trackedDiff check makes the heuristic see only real
  // edits, so a CCH run where the agent declined to commit task work
  // produces no PR (instead of a "1 file changed, 12 insertions(+),
  // 10 deletions(-)" Bucket-B PR). See
  // docs/observations/2026-05-02-bucket-b-after-action.md for the
  // full root-cause analysis.
  if (wt.has_uncommitted_changes) {
    revertWorktreeSettingsArtifact(wt.path);
    const trackedDiff = git(wt.path, ['--no-pager', 'diff', '--shortstat']).length > 0;
    if (trackedDiff) {
      console.log('[apply-result] auto-committing tracked uncommitted changes…');
      git(wt.path, ['add', '-A']);
      git(wt.path, ['commit', '-m', defaultCommitMessage(env)]);
    } else {
      console.log('[apply-result] worktree has untracked-only changes (likely agent bootstrap files); skipping auto-commit.');
      // If there are no committed changes either, treat as no-work-done
      // and skip push entirely (don't pollute origin with empty branches).
      if (wt.commits_added === 0) {
        console.log('[apply-result] no committed work; skipping push and PR.');
        cleanupWorktree(repoRoot, wt);
        return;
      }
    }
  }

  console.log(`[apply-result] pushing ${wt.branch}…`);
  git(wt.path, ['push', '-u', 'origin', wt.branch]);

  console.log('[apply-result] opening PR…');
  const prUrl = openPR(env, wt);
  if (prUrl) console.log(`  PR → ${prUrl}`);

  console.log('[apply-result] cleaning up worktree…');
  cleanupWorktree(repoRoot, wt);
}

function git(cwd: string, args: string[]): string {
  return execFileSync('git', args, { cwd, encoding: 'utf8' }).trim();
}

// Revert any modification of `.claude/settings.json` (vs HEAD) in the
// worktree. The worker's `writeWorktreeClaudeSettings` always touches
// this file (to install the chitin gate hook for the spawned claude-
// code-headless session); the modification is *not* task work and
// shipping it produces bucket-B contamination.
//
// Uses `git diff HEAD --` (working tree + index vs HEAD) instead of bare
// `git diff` so the check sees modifications regardless of whether the
// agent / a future early `git add` already staged them. Plain `git diff`
// is index-vs-working-tree only and would silently no-op on staged
// modifications — bucket-B would silently return.
//
// CAVEAT: this revert ALWAYS fires when `.claude/settings.json` differs
// from HEAD. If a future backlog entry's `file:` field legitimately
// names `.claude/settings.json`, the agent's edits will be silently
// dropped. The function has no awareness of the entry — apply-step
// inputs are just the worktree state. Two mitigations available if it
// ever bites: (a) the entry can pre-commit its `.claude/settings.json`
// edit before declining (auto-commit fallback then sees only that), or
// (b) plumb the entry's file scope into apply-workflow-result and
// gate the revert on whether the entry claims this file.
//
// Errors are swallowed — failing the apply step over a bookkeeping
// revert would be worse than leaving the artifact in (and the
// trackedDiff check still bails on no-real-work via the "no committed
// work; skipping push and PR" branch).
export function revertWorktreeSettingsArtifact(worktreePath: string): void {
  try {
    const modified = git(worktreePath, ['--no-pager', 'diff', 'HEAD', '--name-only', '--', '.claude/settings.json']);
    if (!modified) return; // not a modification of a tracked file — nothing to revert
    // `checkout HEAD --` resets the file in BOTH the index and the working
    // tree, matching the diff scope above. Plain `checkout --` would only
    // reset the working tree, leaving a staged modification in place.
    git(worktreePath, ['checkout', 'HEAD', '--', '.claude/settings.json']);
    console.log('[apply-result] reverted writeWorktreeClaudeSettings artifact: .claude/settings.json');
  } catch (err) {
    console.warn(`[apply-result] revert of .claude/settings.json failed (ignored): ${err instanceof Error ? err.message : String(err)}`);
  }
}

function defaultCommitMessage(env: ResultEnvelope): string {
  const wfid = env.workflow_id;
  return `swarm: ${wfid}\n\nAuto-committed by apply-workflow-result. Workflow ${wfid} produced uncommitted changes; the dispatcher staged them so the branch could be pushed. Re-run with explicit commit guidance if you need a more descriptive message.`;
}

function defaultPrTitle(env: ResultEnvelope): string {
  return `swarm: ${env.workflow_id}`;
}

function openPR(env: ResultEnvelope, wt: WorktreeResult): string | null {
  const title = env.pr_title ?? defaultPrTitle(env);
  const body = (env.pr_body ?? '') +
    `\n\n---\n` +
    `Generated by the slice-5 swarm-worktree pipeline.\n` +
    `Workflow ID: \`${env.workflow_id}\`\n` +
    `Branch: \`${wt.branch}\`\n` +
    `Head: \`${wt.head_sha.slice(0, 8)}\`\n` +
    `Diff: \`${wt.diff_shortstat || 'no shortstat'}\`\n` +
    `Commits: ${wt.commits_added}${wt.has_uncommitted_changes ? ' (+ auto-committed uncommitted state)' : ''}`;
  try {
    const out = execSync(
      `gh pr create --title ${shellQuote(title)} --body ${shellQuote(body)} --head ${shellQuote(wt.branch)}`,
      { encoding: 'utf8' },
    );
    return out.trim().split('\n').pop() ?? null;
  } catch (err) {
    console.warn(`[apply-result] gh pr create failed: ${err instanceof Error ? err.message : String(err)}`);
    return null;
  }
}

function cleanupWorktree(repoRoot: string, wt: WorktreeResult) {
  try {
    execFileSync('git', ['worktree', 'remove', '--force', wt.path], { cwd: repoRoot });
  } catch (err) {
    console.warn(`[apply-result] worktree cleanup failed (manual rm may be needed): ${err instanceof Error ? err.message : String(err)}`);
  }
}

function shellQuote(s: string): string {
  return `'${s.replace(/'/g, "'\\''")}'`;
}

function readFlag(name: string): string | null {
  const idx = process.argv.indexOf(name);
  if (idx < 0 || idx + 1 >= process.argv.length) return null;
  return process.argv[idx + 1];
}

// Only run main() when invoked as a script. Importing
// revertWorktreeSettingsArtifact (or any other helper) from tests
// must not exit the process or read process.argv as a CLI.
const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  main().catch((err) => {
    console.error('[apply-result] fatal:', err);
    process.exit(1);
  });
}
