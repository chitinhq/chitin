import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync, copyFileSync, existsSync, rmSync, mkdirSync } from 'node:fs';
import { tmpdir, homedir } from 'node:os';
import { join, resolve } from 'node:path';
import type { ExecutionRequest, DriverId } from '@chitin/contracts';
import type { ActivityResult, WorktreeResult } from './activity-types.ts';

// Bytes of stdout/stderr returned to Temporal in ActivityResult. Buffers
// during the run are bounded at 2x this value (see runAgentTurn), so
// chatty drivers can't OOM the 24/7 worker.
const TAIL_BYTES = 2000;

// Slice 5: where worktrees live when base_ref is set on the request. XDG
// cache (transient, safe to delete). One sub-dir per workflow_id.
const SWARM_WORKTREES_ROOT = resolve(homedir(), '.cache/chitin/swarm-worktrees');

interface DriverInvocation {
  command: string;
  args: string[];
  env?: Record<string, string>;
}

// Per-driver openclaw agent mapping (slice 3). Each local-* driver routes to
// a distinct openclaw agent so reasoning and mechanical models can be
// configured independently — the spec calls for qwen3-coder for mechanical
// (local-qwen), glm-5.1:cloud for reasoning (local-glm), deepseek for code
// (local-deepseek). Override per driver via env var, e.g.
// CHITIN_AGENT_LOCAL_QWEN=my-agent. Falls back to `main` if neither env var
// nor the default mapping resolves the driver — `main` always exists.
const DRIVER_AGENT_MAP: Record<string, string> = {
  'local-qwen': 'qwen-agent',
  'local-glm': 'glm-agent',
  'local-deepseek': 'deepseek-agent',
};

function resolveAgent(driver: DriverId): string {
  const envVar = `CHITIN_AGENT_${driver.toUpperCase().replace(/-/g, '_')}`;
  const override = process.env[envVar];
  if (override && override.trim()) return override.trim();
  return DRIVER_AGENT_MAP[driver] ?? 'main';
}

// Tools the headless Claude Code session is allowed to dispatch. Mirrors
// the chat-domain coverage chitin's normalizer recognizes (read/edit/write/
// bash) so every tool call still hits a policy-meaningful action_type.
// Override per request via CHITIN_CLAUDE_ALLOWED_TOOLS env var if you need
// a tighter scope (e.g., 'Read,Edit' only).
const DEFAULT_CLAUDE_ALLOWED_TOOLS = 'Read,Edit,Write,Bash,Glob,Grep';

function planInvocation(req: ExecutionRequest): DriverInvocation {
  const driver: DriverId = req.allowed_drivers[0];
  switch (driver) {
    case 'copilot':
      return {
        command: 'chitin-kernel',
        args: ['drive', 'copilot', req.prompt],
      };
    case 'claude-code-headless':
      // Anthropic publishes this as the supported pattern for unattended
      // runs (see code.claude.com/docs/en/headless). Spawned in the
      // worktree (when base_ref is set on the request) so edits land on
      // a real branch. The existing claude-code adapter PreToolUse hook
      // (PR #66) gates every tool call — same enforcement plane as the
      // interactive surface, just no human in the loop. The
      // --dangerously-skip-permissions flag bypasses Claude Code's own
      // interactive permission prompts; chitin's gate is the actual
      // policy boundary.
      return {
        command: 'claude',
        args: [
          '-p', req.prompt,
          '--dangerously-skip-permissions',
          '--allowedTools', process.env.CHITIN_CLAUDE_ALLOWED_TOOLS ?? DEFAULT_CLAUDE_ALLOWED_TOOLS,
          '--output-format', 'stream-json',
          '--verbose',
        ],
      };
    case 'local-qwen':
    case 'local-glm':
    case 'local-deepseek':
      // Dispatch through openclaw + chitin-governance plugin. The plugin
      // is loaded at openclaw startup (~/.openclaw/openclaw.json plugins.allow
      // includes "chitin-governance"); every tool call the local agent
      // dispatches passes through before_tool_call → chitin gate. The per-
      // driver agent mapping (slice 3) routes to distinct openclaw agents so
      // each local tier runs its own model.
      return {
        command: 'openclaw',
        args: [
          'agent',
          '--local',
          '--agent', resolveAgent(driver),
          '--json',
          '--timeout', String(req.bounds.wall_timeout_s),
          '--message', req.prompt,
        ],
      };
    default: {
      const exhaustive: never = driver;
      throw new Error(`unknown driver: ${exhaustive as string}`);
    }
  }
}

// Policy file lookup order:
//   1. CHITIN_POLICY_FILE env var (absolute path) — explicit override.
//   2. <cwd>/chitin.yaml — repo-relative default. The worker is meant to be
//      launched from the repo root; this matches developer/CI ergonomics.
// If neither resolves to an existing file, the worker proceeds without
// seeding a policy file. The kernel's gate evaluate path will then fall
// back to its own default-deny semantics.
function resolvePolicySrc(): string {
  const explicit = process.env.CHITIN_POLICY_FILE;
  if (explicit) return resolve(explicit);
  return resolve(process.cwd(), 'chitin.yaml');
}

// Where the source repo lives — the one we create worktrees from. Worker
// is conventionally launched from the repo root, but allow CHITIN_REPO_ROOT
// to override (e.g., when the worker process runs from a different dir).
function resolveRepoRoot(): string {
  const explicit = process.env.CHITIN_REPO_ROOT;
  if (explicit) return resolve(explicit);
  return process.cwd();
}

function git(repoCwd: string, args: string[]): string {
  return execFileSync('git', args, { cwd: repoCwd, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
}

// Slice 5: provision a worktree at SWARM_WORKTREES_ROOT/<workflow_id>/
// based on req.base_ref. Branch is `swarm/<workflow_id>`. Idempotent:
// if the path or branch already exists (from a prior crash), tries to
// clean up first.
function provisionWorktree(req: ExecutionRequest, repoRoot: string): { path: string; branch: string } {
  if (!req.base_ref) {
    throw new Error('provisionWorktree called without base_ref');
  }
  mkdirSync(SWARM_WORKTREES_ROOT, { recursive: true });
  const path = join(SWARM_WORKTREES_ROOT, req.workflow_id);
  const branch = `swarm/${req.workflow_id}`;
  // Best-effort cleanup of stale state from a prior crashed run.
  try {
    git(repoRoot, ['worktree', 'remove', '--force', path]);
  } catch {
    // not an existing worktree — fine
  }
  if (existsSync(path)) rmSync(path, { recursive: true, force: true });
  try {
    git(repoRoot, ['branch', '-D', branch]);
  } catch {
    // not an existing branch — fine
  }
  git(repoRoot, ['worktree', 'add', '-b', branch, path, req.base_ref]);
  return { path, branch };
}

// After agent exits, capture worktree state for the apply step. Does not
// modify anything — purely observational.
function captureWorktreeState(worktreePath: string, baseRef: string): WorktreeResult {
  const headSha = git(worktreePath, ['rev-parse', 'HEAD']);
  const commitsAdded = parseInt(
    git(worktreePath, ['rev-list', '--count', `${baseRef}..HEAD`]) || '0',
    10,
  );
  // status --porcelain returns one line per dirty path; empty string = clean.
  const status = git(worktreePath, ['status', '--porcelain']);
  const hasUncommitted = status.length > 0;
  // shortstat between base and current tree (covers both committed and
  // uncommitted by diffing tree-vs-tree-of-base — actually for that we want
  // a full diff including working tree, so:
  const diffShortstat = (() => {
    try {
      // Full diff including working tree changes:
      // diff base...HEAD covers committed; we want both, so do diff base
      // against the working tree:
      return git(worktreePath, ['--no-pager', 'diff', '--shortstat', baseRef]);
    } catch {
      return '';
    }
  })();
  // resolve branch name from HEAD ref
  let branch = '';
  try {
    branch = git(worktreePath, ['rev-parse', '--abbrev-ref', 'HEAD']);
  } catch {
    // detached
  }
  return {
    path: worktreePath,
    branch,
    head_sha: headSha,
    commits_added: Number.isFinite(commitsAdded) ? commitsAdded : 0,
    has_uncommitted_changes: hasUncommitted,
    diff_shortstat: diffShortstat,
  };
}

export async function runAgentTurn(req: ExecutionRequest): Promise<ActivityResult> {
  // Slice 5: when base_ref is set, run inside a real git worktree so the
  // agent's edits are durable and a follow-up apply-step can push + PR.
  // When base_ref is absent (slice 1-4 behavior), use a tempdir — the
  // agent's edits are discarded on exit.
  const useWorktree = !!req.base_ref;
  const repoRoot = resolveRepoRoot();
  let workDir: string;
  let worktreeInfo: { path: string; branch: string } | null = null;
  if (useWorktree) {
    worktreeInfo = provisionWorktree(req, repoRoot);
    workDir = worktreeInfo.path;
  } else {
    workDir = mkdtempSync(join(tmpdir(), `chitin-worker-${req.workflow_id}-`));
    const policySrc = resolvePolicySrc();
    if (existsSync(policySrc)) {
      copyFileSync(policySrc, join(workDir, 'chitin.yaml'));
    }
  }

  const plan = planInvocation(req);

  const start = Date.now();
  let result: ActivityResult;
  try {
    result = await new Promise<ActivityResult>((resolvePromise, reject) => {
      const child = spawn(plan.command, plan.args, {
        cwd: workDir,
        env: {
          ...process.env,
          ...(plan.env ?? {}),
          CHITIN_WORKFLOW_ID: req.workflow_id,
          CHITIN_RUN_ID: req.run_id,
        },
        stdio: ['ignore', 'pipe', 'pipe'],
      });

      // Bounded ring buffers — only the tail is reported, so growing strings
      // unboundedly would just burn memory in a 24/7 worker that hits chatty
      // drivers. Cap at 2x the reported tail to absorb boundary chunks.
      const tail = (cur: string, chunk: string, cap: number) =>
        (cur + chunk).slice(-cap);
      const TAIL_CAP = TAIL_BYTES * 2;
      let stdout = '';
      let stderr = '';
      child.stdout.on('data', (b) => (stdout = tail(stdout, b.toString(), TAIL_CAP)));
      child.stderr.on('data', (b) => (stderr = tail(stderr, b.toString(), TAIL_CAP)));

      const killTimer = setTimeout(() => child.kill('SIGKILL'), req.bounds.wall_timeout_s * 1000);
      child.on('close', (code) => {
        clearTimeout(killTimer);
        resolvePromise({
          exit_code: code ?? -1,
          stdout_tail: stdout.slice(-TAIL_BYTES),
          stderr_tail: stderr.slice(-TAIL_BYTES),
          duration_ms: Date.now() - start,
        });
      });
      child.on('error', reject);
    });
  } finally {
    // Tempdir mode: rm is fine, edits discarded by design.
    // Worktree mode: do NOT rm — apply-step needs the worktree to capture
    // state and push. Apply-step is responsible for cleanup. If the activity
    // crashes mid-way, the next provisionWorktree() call on the same
    // workflow_id will reclaim the path via best-effort cleanup.
    if (!useWorktree) {
      rmSync(workDir, { recursive: true, force: true });
    }
  }

  if (useWorktree && worktreeInfo) {
    try {
      result.worktree = captureWorktreeState(worktreeInfo.path, req.base_ref!);
    } catch (err) {
      // Don't fail the activity over capture errors — the apply step will
      // surface the issue. Log to stderr_tail so the operator sees it.
      const msg = err instanceof Error ? err.message : String(err);
      result.stderr_tail = `${result.stderr_tail}\n[capture-worktree-state] ${msg}`.slice(-TAIL_BYTES);
    }
  }

  return result;
}

export const __test__ = {
  planInvocation,
  resolvePolicySrc,
  resolveAgent,
  resolveRepoRoot,
  provisionWorktree,
  captureWorktreeState,
  DRIVER_AGENT_MAP,
  SWARM_WORKTREES_ROOT,
};
