import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync, copyFileSync, existsSync, rmSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir, homedir } from 'node:os';
import { join, resolve } from 'node:path';
import type { ExecutionRequest, DriverId, Tier } from '@chitin/contracts';
import type { ActivityResult, HookEventSummary, WorktreeResult } from './activity-types.ts';

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

// Slice 6c: tier → model maps per driver. T0/T1 use the cheap fast
// models; T4 uses the strongest. Drivers that route via a per-agent
// configured model (the local-* tier through openclaw) ignore the tier
// — its model is set at agent-creation time, not per call. Override by
// setting CHITIN_MODEL_<DRIVER>_<TIER> for tactical experimentation.
const CLAUDE_TIER_MODEL: Record<Tier, string> = {
  T0: 'claude-haiku-4-5',
  T1: 'claude-haiku-4-5',
  T2: 'claude-sonnet-4-6',
  T3: 'claude-sonnet-4-6',
  T4: 'claude-opus-4-7',
};

// 2026-05-02: T2 bumped from haiku → sonnet on copilot. Pairs with the
// dispatcher.ts T2 reroute (claude-code-headless → copilot) so the model
// class is preserved (was claude-sonnet-4-6 via CCH, now claude-sonnet-4.6
// via copilot — same family). Without this bump the reroute would silently
// downgrade T2 reasoning to haiku. Cost is still $0 under the Copilot Pro
// plan.
const COPILOT_TIER_MODEL: Record<Tier, string> = {
  T0: 'gpt-4.1',
  T1: 'gpt-4.1',
  T2: 'claude-sonnet-4.6',           // (was claude-haiku-4.5; bumped to preserve T2 reasoning quality)
  T3: 'claude-sonnet-4.6',
  T4: 'claude-opus-4.7',
};

function envOverride(driverEnvKey: string, tier: Tier): string | undefined {
  const v = process.env[`CHITIN_MODEL_${driverEnvKey}_${tier}`];
  return v && v.trim() ? v.trim() : undefined;
}

function resolveClaudeModel(tier: Tier | undefined): string | null {
  if (!tier) return null;
  return envOverride('CLAUDE_CODE_HEADLESS', tier) ?? CLAUDE_TIER_MODEL[tier];
}

function resolveCopilotModel(tier: Tier | undefined): string | null {
  if (!tier) return null;
  return envOverride('COPILOT', tier) ?? COPILOT_TIER_MODEL[tier];
}

function planInvocation(req: ExecutionRequest): DriverInvocation {
  const driver: DriverId = req.allowed_drivers[0];
  switch (driver) {
    case 'copilot': {
      // Slice 6c: append --model when tier is set, so chitin-kernel drive
      // copilot threads it into the Copilot SDK SessionConfig.
      const args = ['drive', 'copilot', req.prompt];
      const model = resolveCopilotModel(req.tier);
      if (model) args.push('--model', model);
      return {
        command: 'chitin-kernel',
        args,
      };
    }
    case 'claude-code-headless': {
      // Anthropic publishes this as the supported pattern for unattended
      // runs (see code.claude.com/docs/en/headless). Spawned in the
      // worktree (when base_ref is set on the request) so edits land on
      // a real branch. The chitin claude-code PreToolUse hook gates every
      // tool call — but only fires when Claude Code's settings discovery
      // picks up a hooks-bearing settings.json, which is project-relative.
      // Slice 6a wires that via writeWorktreeSettings before this spawn.
      // --dangerously-skip-permissions bypasses Claude Code's own
      // interactive permission prompts; chitin's gate is the actual policy
      // boundary, and it still fires via the worktree's project settings.
      const args = [
        '-p', req.prompt,
        '--include-hook-events',
        '--dangerously-skip-permissions',
        '--allowedTools', process.env.CHITIN_CLAUDE_ALLOWED_TOOLS ?? DEFAULT_CLAUDE_ALLOWED_TOOLS,
        '--output-format', 'stream-json',
        '--verbose',
      ];
      // Slice 6c: tier-driven model. T0/T1 → haiku, T2/T3 → sonnet, T4 → opus.
      const model = resolveClaudeModel(req.tier);
      if (model) args.push('--model', model);
      return {
        command: 'claude',
        args,
      };
    }
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
          '--include-hook-events',
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

// Slice 6a: write a project-level .claude/settings.json into the worktree
// before spawning claude -p. This wires the chitin PreToolUse hook for
// claude-code-headless runs whose cwd is outside the user's primary
// workspace dir — without this, slice 5b's headless runs bypassed the
// chitin gate entirely (verified by zero claude-code chain entries on
// 2026-05-02 despite 1123+ hook captures going to the no-op global hook).
//
// Claude Code merges hooks across scopes (managed > local > project >
// user), so this project-level settings.json adds chitin gating ON TOP
// OF whatever the user's global settings define — it does not replace
// the global config.
function writeWorktreeClaudeSettings(worktreePath: string): void {
  const claudeDir = join(worktreePath, '.claude');
  mkdirSync(claudeDir, { recursive: true });
  const settingsPath = join(claudeDir, 'settings.json');
  // The hook command must produce a JSON decision the Claude Code hook
  // protocol recognizes. chitin-kernel's `gate evaluate --hook-stdin`
  // mode reads the hook payload from stdin, normalizes the tool call,
  // calls gov.Gate.Evaluate, and prints {decision} on stdout.
  const settings = {
    hooks: {
      PreToolUse: [
        {
          matcher: '',
          hooks: [
            {
              type: 'command' as const,
              command: 'chitin-kernel gate evaluate --hook-stdin --agent=claude-code',
            },
          ],
        },
      ],
    },
  };
  writeFileSync(settingsPath, JSON.stringify(settings, null, 2));
}

// Slice 6b: write a per-workflow openclaw config dir that points the
// requested local-* agent's workspace at the worktree. Spawned openclaw
// reads OPENCLAW_STATE_DIR for its config + state, so a transient
// per-workflow STATE_DIR avoids mutating the user's `~/.openclaw/openclaw.json`
// (which would race with concurrent workflows). Returns the env var to set
// on the spawn, or null when no remap is needed.
function provisionOpenclawState(
  req: ExecutionRequest,
  worktreePath: string,
  agentId: string,
): { stateDir: string; envOverride: Record<string, string> } | null {
  const sourceConfig = resolve(homedir(), '.openclaw/openclaw.json');
  if (!existsSync(sourceConfig)) return null;
  const sourceState = resolve(homedir(), '.openclaw');
  const stateDir = join(SWARM_WORKTREES_ROOT, `${req.workflow_id}-openclaw-state`);
  mkdirSync(stateDir, { recursive: true });
  // Symlink everything from the source state dir except openclaw.json,
  // which we rewrite below to redirect the requested agent's workspace.
  // This avoids copying gigabytes of session/transcript history while
  // still letting openclaw find its providers, plugins, agent dirs.
  for (const entry of execFileSync('ls', ['-1', sourceState], { encoding: 'utf8' }).trim().split('\n')) {
    if (!entry || entry === 'openclaw.json' || entry === 'openclaw.json.bak') continue;
    const target = join(stateDir, entry);
    if (existsSync(target)) continue;
    try {
      execFileSync('ln', ['-s', join(sourceState, entry), target]);
    } catch {
      // skip — usually a dangling symlink in source we can't follow
    }
  }
  // Read the user's openclaw.json, redirect the named agent's workspace
  // to the worktree, write to the per-workflow state dir.
  const cfg = JSON.parse(readFileSync(sourceConfig, 'utf8'));
  const list = cfg?.agents?.list;
  if (Array.isArray(list)) {
    for (const a of list) {
      if (a && typeof a === 'object' && a.id === agentId) {
        a.workspace = worktreePath;
      }
    }
  }
  writeFileSync(join(stateDir, 'openclaw.json'), JSON.stringify(cfg, null, 2));
  return {
    stateDir,
    envOverride: { OPENCLAW_STATE_DIR: stateDir },
  };
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
  let openclawState: { stateDir: string; envOverride: Record<string, string> } | null = null;
  if (useWorktree) {
    worktreeInfo = provisionWorktree(req, repoRoot);
    workDir = worktreeInfo.path;
    // Slice 6a: claude-code-headless needs project-level chitin hook
    // settings.json in the worktree so its tool calls actually gate.
    if (req.allowed_drivers.includes('claude-code-headless')) {
      writeWorktreeClaudeSettings(worktreeInfo.path);
    }
    // Slice 6b: openclaw-driven local-* drivers need the agent's workspace
    // pointed at the worktree so the agent's read/edit tools see the right
    // files. We provision a per-workflow OPENCLAW_STATE_DIR with a remapped
    // openclaw.json instead of mutating the user's global config.
    const localDriver = req.allowed_drivers.find((d) =>
      d === 'local-qwen' || d === 'local-glm' || d === 'local-deepseek',
    );
    if (localDriver) {
      const agentId = resolveAgent(localDriver as DriverId);
      openclawState = provisionOpenclawState(req, worktreeInfo.path, agentId);
    }
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
      // Slice 7a: spawn detached so the child becomes the leader of its own
      // process group. On wall_timeout SIGKILL we then kill the whole group
      // (process.kill(-pid)) instead of just the parent. Without this,
      // grandchildren (model runners under openclaw, or claude's worker
      // subprocesses) inherit the stdout pipe and keep it open after the
      // parent dies — Node's 'close' event waits for FDs to close, never
      // fires, and the activity hangs to Temporal's startToCloseTimeout
      // (15 min). With this fix, the timer-fired kill propagates and the
      // close event arrives within ~1s.
      const child = spawn(plan.command, plan.args, {
        cwd: workDir,
        env: {
          ...process.env,
          ...(plan.env ?? {}),
          ...(openclawState?.envOverride ?? {}),
          CHITIN_WORKFLOW_ID: req.workflow_id,
          CHITIN_RUN_ID: req.run_id,
        },
        stdio: ['ignore', 'pipe', 'pipe'],
        detached: true,
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

      const killTimer = setTimeout(() => {
        if (child.pid !== undefined) {
          try {
            // Negative pid = process group. Kills child + all its descendants
            // (openclaw + model runner + ollama + ...). Belt-and-suspenders:
            // also force-close stdout/stderr in case the descendant tree is
            // still holding pipes after SIGKILL (rare but real on overloaded
            // systems where SIGKILL takes a tick to propagate).
            process.kill(-child.pid, 'SIGKILL');
          } catch {
            // ESRCH = process already exited. No-op.
          }
        }
        child.stdout?.destroy();
        child.stderr?.destroy();
      }, req.bounds.wall_timeout_s * 1000);
      child.on('close', (code) => {
        clearTimeout(killTimer);
        const tailStdout = stdout.slice(-TAIL_BYTES);
        const tailStderr = stderr.slice(-TAIL_BYTES);
        const hookEvents = plan.args.includes('--include-hook-events')
          ? extractHookEvents(tailStdout)
          : undefined;
        const tool_summary = plan.command === 'openclaw' ? parseToolSummary(stdout) : undefined;
        resolvePromise({
          exit_code: code ?? -1,
          stdout_tail: tailStdout,
          stderr_tail: tailStderr,
          duration_ms: Date.now() - start,
          ...(hookEvents ? { hook_events: hookEvents } : {}),
          ...(tool_summary ? { tool_summary } : {}),
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
    // Slice 6b: per-workflow openclaw state dir is transient — clean it
    // up unconditionally regardless of activity success/failure. The user's
    // `~/.openclaw/openclaw.json` was never mutated, so there's nothing
    // to restore there.
    if (openclawState) {
      rmSync(openclawState.stateDir, { recursive: true, force: true });
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

/**
 * Project the agent's stream-json stdout tail into a typed
 * HookEventSummary[]. Best-effort — events older than TAIL_BYTES are
 * lost, and lines that don't parse as JSON are silently skipped.
 *
 * We only forward fields downstream consumers actually use; other
 * fields on the source event are intentionally dropped to keep the
 * audit-log payload small.
 */
function extractHookEvents(stdoutTail: string): HookEventSummary[] | undefined {
  const out: HookEventSummary[] = [];
  for (const line of stdoutTail.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    let parsed: unknown;
    try {
      parsed = JSON.parse(trimmed);
    } catch {
      continue;
    }
    if (!parsed || typeof parsed !== 'object') continue;
    const ev = parsed as Record<string, unknown>;
    if (ev.type !== 'hook_event') continue;
    const summary: HookEventSummary = {};
    if (typeof ev.hook_name === 'string') summary.hook_name = ev.hook_name;
    if (typeof ev.tool_name === 'string') summary.tool_name = ev.tool_name;
    if (ev.decision === 'allow' || ev.decision === 'deny' || ev.decision === 'error') {
      summary.decision = ev.decision;
    }
    if (typeof ev.reason === 'string') summary.reason = ev.reason;
    out.push(summary);
  }
  return out.length > 0 ? out : undefined;
}

/**
 * Extract the openclaw `toolSummary` payload from the agent's stdout.
 *
 * openclaw's `--json` mode emits NDJSON (one JSON object per line) but
 * older / non-streaming variants drop a final summary object containing
 * a top-level `toolSummary`. The previous regex-based extractor was
 * non-greedy on `{...}` which silently dropped any object containing
 * nested braces — including the toolSummary object itself — so the
 * field never populated in practice.
 *
 * Strategy: split into lines first (NDJSON path); for any line that
 * doesn't parse, fall back to a brace-balanced scan from the end of
 * stdout to recover the LAST complete top-level JSON object. Either
 * path returns the typed `tool_summary` shape if a `toolSummary` field
 * is found, otherwise undefined.
 */
function parseToolSummary(stdout: string): ActivityResult['tool_summary'] {
  // Path 1 — NDJSON. Try every line. The newest summary wins (last
  // line that has toolSummary).
  let found: ActivityResult['tool_summary'];
  for (const line of stdout.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed.startsWith('{')) continue;
    try {
      const obj = JSON.parse(trimmed) as { toolSummary?: ActivityResult['tool_summary'] };
      if (obj && typeof obj === 'object' && obj.toolSummary) {
        found = obj.toolSummary;
      }
    } catch {
      // Not a complete JSON line on its own — fall through.
    }
  }
  if (found) return found;

  // Path 2 — brace-balanced scan from the end. Walk backward from the
  // last `}`, count braces (outside string literals), and slice when
  // the count returns to zero. Try parsing that slice.
  for (let end = stdout.lastIndexOf('}'); end >= 0; end = stdout.lastIndexOf('}', end - 1)) {
    const start = findMatchingOpenBrace(stdout, end);
    if (start < 0) continue;
    const candidate = stdout.slice(start, end + 1);
    try {
      const obj = JSON.parse(candidate) as { toolSummary?: ActivityResult['tool_summary'] };
      if (obj && typeof obj === 'object' && obj.toolSummary) return obj.toolSummary;
    } catch {
      // Not balanced top-level JSON — keep walking.
    }
  }
  return undefined;
}

/**
 * Walk backward from `end` (a `}` index in `s`), counting brace depth
 * while ignoring braces inside double-quoted strings. Returns the index
 * of the matching `{`, or -1 if no balanced opener is found.
 */
function findMatchingOpenBrace(s: string, end: number): number {
  let depth = 0;
  let inString = false;
  for (let i = end; i >= 0; i--) {
    const ch = s[i];
    if (inString) {
      if (ch === '"' && s[i - 1] !== '\\') inString = false;
      continue;
    }
    if (ch === '"') {
      inString = true;
      continue;
    }
    if (ch === '}') depth++;
    else if (ch === '{') {
      depth--;
      if (depth === 0) return i;
    }
  }
  return -1;
}

export const __test__ = {
  parseToolSummary,
  planInvocation,
  resolvePolicySrc,
  resolveAgent,
  resolveRepoRoot,
  provisionWorktree,
  captureWorktreeState,
  resolveClaudeModel,
  resolveCopilotModel,
  writeWorktreeClaudeSettings,
  provisionOpenclawState,
  extractHookEvents,
  DRIVER_AGENT_MAP,
  SWARM_WORKTREES_ROOT,
  CLAUDE_TIER_MODEL,
  COPILOT_TIER_MODEL,
};
