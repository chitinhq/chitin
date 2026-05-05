import { spawn, execFileSync } from 'node:child_process';
import { mkdtempSync, copyFileSync, existsSync, readdirSync, rmSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir, homedir } from 'node:os';
import { join, resolve, relative, isAbsolute } from 'node:path';
import type { ExecutionRequest, DriverId, Tier } from '@chitin/contracts';
import type { ActivityResult, HookEventSummary, WorktreeResult } from './activity-types.ts';

// Bytes of stdout/stderr returned to Temporal in ActivityResult. Buffers
// during the run are bounded at 2x this value (see runAgentTurn), so
// chatty drivers can't OOM the 24/7 worker.
//
// Sized so that the reviewer agent's `<<<REVIEW>>>{...json...}` marker
// (emitted at the end of the final text message) fits in the captured
// tail. parseReviewerOutput in reviewer-prompts.ts looks for the
// marker in the tail; if it's pushed out by trailing stream-json
// events (Claude Code's result event with usage stats is ~500 bytes
// of JSON, plus message_stop + a final newline) the parser returns
// "no <<<REVIEW>>> marker in stdout" and the review-graph escalates
// one tier — eventually falling through R3 → operator with the
// "review chain parse-failure cascade" digest.
//
// Pre-2026-05-04 default: 2000. That blew past the marker on every
// reviewer run for PR #274 (260 LOC docs sweep) — the review chain
// fired R2 → R3 → operator-escalation, both tiers parse-failed.
// Bumped to 16384 (16 KB) — comfortably absorbs the trailing events.
//
// Override via CHITIN_STDOUT_TAIL_BYTES for future tuning.
const TAIL_BYTES = (() => {
  const raw = process.env.CHITIN_STDOUT_TAIL_BYTES;
  if (!raw) return 16384;
  const n = parseInt(raw.trim(), 10);
  return Number.isFinite(n) && n > 0 ? n : 16384;
})();

// Slice 5: where worktrees live when base_ref is set on the request. XDG
// cache (transient, safe to delete). One sub-dir per workflow_id.
const SWARM_WORKTREES_ROOT = resolve(homedir(), '.cache/chitin/swarm-worktrees');

/**
 * Returns true if the given path is unambiguously owned by the worker
 * and therefore safe to `rmSync`-cleanup:
 * - Strictly under `os.tmpdir()` (tempdir-mode worker dirs), OR
 * - Strictly under `SWARM_WORKTREES_ROOT` (per-workflow worktrees).
 *
 * Strict containment matters: `startsWith(tmpdir())` raw would also
 * match a sibling like `${tmpdir()}-backup`. We use `relative()` to
 * verify the path is genuinely under the base, not just a prefix
 * match.
 *
 * Edge case: when repoRoot itself sits under tmpdir() (test rigs,
 * CI), the allowlist would match it. Reviewer mode runs in repoRoot
 * directly (#280); the explicit `abs === repoRoot` short-circuit
 * preserves the invariant that we never rm the repo checkout.
 *
 * Exported so tests can pin the contract without standing up a full
 * activity context. See review-graph-dispatch.ts pattern.
 */
export function isWorkerOwnedPath(p: string, repoRoot: string): boolean {
  const abs = resolve(p);
  if (abs === resolve(repoRoot)) return false;
  return isStrictlyUnder(abs, resolve(tmpdir())) || isStrictlyUnder(abs, SWARM_WORKTREES_ROOT);
}

function isStrictlyUnder(abs: string, base: string): boolean {
  const rel = relative(base, abs);
  // '' = same path; '..' / starts with '..' = outside base.
  // Anything else = strictly under base.
  return rel !== '' && !rel.startsWith('..') && !isAbsolute(rel);
}

interface DriverInvocation {
  command: string;
  args: string[];
  env?: Record<string, string>;
}

// Per-driver openclaw agent mapping. Each openclaw-* driver routes to a
// distinct openclaw agent in ~/.openclaw/openclaw.json so each tier can
// run its own model.
//
// 2026-05-04 rename: local-* → openclaw-*. The `local` prefix was
// misleading — `local-glm` actually pointed at glm-5.1:cloud (Ollama
// Cloud sub, not on-box). The new prefix is accurate: every entry here
// dispatches through openclaw, model-residency is in the suffix
// (-flash = 3090 on-box, -cloud = Ollama Cloud sub).
//
// Override per driver via env var, e.g. CHITIN_AGENT_OPENCLAW_GLM_FLASH=my-agent.
// Falls back to `main` if neither env var nor the default mapping
// resolves the driver — `main` always exists.
const DRIVER_AGENT_MAP: Record<string, string> = {
  'openclaw-glm-flash': 'glm-flash-agent',  // 3090: glm-4.7-flash:latest (~30B)
  'openclaw-glm-cloud': 'glm-agent',        // Ollama Cloud sub: glm-5.1:cloud (opus-light)
  'openclaw-deepseek': 'deepseek-agent',    // 3090: deepseek (kept; not in defaults)
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

// Tier → model maps per driver. Drivers that route via a per-agent
// configured model (openclaw-* through openclaw) ignore the tier — its
// model is set at agent-creation time, not per call. Override by
// setting CHITIN_MODEL_<DRIVER>_<TIER> for tactical experimentation.
//
// 2026-05-04 rebalance, driven by Copilot Pro premium-request multipliers
// the operator measured directly:
//
//   gpt-5-mini       0×    free, unlimited
//   gpt-5.4-mini     0.33×
//   claude-haiku-4-5 0.33×
//   claude-sonnet-4-6 1×
//   gpt-5.4          1×
//   claude-opus-4-6  3×
//   gpt-5.5          7.5×
//
// Rule on Copilot: never premium-tier-and-up by default (no sonnet, no
// opus, no gpt-5.5). They burn the Pro premium-request budget too fast.
// Default to gpt-5-mini for free/unlimited tiers; haiku-4-5 (0.33×) for
// any tier that needs more reasoning. Anthropic models go via
// claude-code-headless (Max plan) — never through Copilot.
const CLAUDE_TIER_MODEL: Record<Tier, string> = {
  T0: 'claude-haiku-4-5',
  T1: 'claude-haiku-4-5',
  T2: 'claude-sonnet-4-6',
  T3: 'claude-sonnet-4-6',
  T4: 'claude-opus-4-7',
};

const COPILOT_TIER_MODEL: Record<Tier, string> = {
  T0: 'gpt-5-mini',                  // 0× free unlimited
  T1: 'gpt-5-mini',                  // 0× free unlimited
  T2: 'claude-haiku-4-5',            // 0.33× — bulk programmer (Copilot routes to haiku via Anthropic provider)
  T3: 'claude-haiku-4-5',            // 0.33× — escalation cascade still cheap if T3 falls back to copilot
  T4: 'claude-haiku-4-5',            // 0.33× — never opus (3×) or gpt-5.5 (7.5×) on Copilot
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

/**
 * Resolve the model the dispatching agent will use, regardless of which
 * driver was picked. Returned value is what gets stamped onto chain
 * Decisions via CHITIN_MODEL env (P2 routing-as-learning-system).
 *
 * Returns the same model that planInvocation passes via --model/-m flags
 * to the CLI, so chain attribution and CLI invocation can never disagree.
 *
 * Drivers without per-tier defaults (codex, gemini, openclaw-*) return
 * the env-override when set, else null. Activity layer threads null
 * through to env as empty string, which the kernel's omitempty drops
 * from the JSON row.
 */
function resolveDispatchModel(driver: DriverId, tier: Tier | undefined): string | null {
  switch (driver) {
    case 'copilot':
      return resolveCopilotModel(tier);
    case 'claude-code-headless':
      return resolveClaudeModel(tier);
    case 'codex':
      return (process.env.CHITIN_MODEL_CODEX ?? '').trim() || null;
    case 'gemini':
      return (process.env.CHITIN_MODEL_GEMINI ?? '').trim() || null;
    case 'openclaw-glm-flash':
    case 'openclaw-glm-cloud':
    case 'openclaw-deepseek':
      // openclaw-* models live in ~/.openclaw/openclaw.json per-agent;
      // we don't know the model from this side without reading that file.
      // Future P2.5 follow-up: parse openclaw.json at activity-start and
      // surface the agent's model into the env. For now, leave empty so
      // chain rows from openclaw dispatches get role/workflow_id but no
      // model — better than guessing wrong.
      return null;
  }
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
    case 'codex': {
      // OpenAI Codex CLI in non-interactive mode. Real-time
      // governance is wired via codex's PreToolUse hook (codex
      // 0.128.0+ — verified 2026-05-04, see project memory
      // `Codex DOES have PreToolUse hooks`). The hook is
      // installed via scripts/install-codex-hook.sh, which writes
      // a [[hooks.PreToolUse]] block into ~/.codex/config.toml
      // pointing at chitin-router-hook --agent=codex. codex_mine
      // remains useful for retrospective audits + the universal
      // usage feed.
      //
      // --json emits structured event lines so downstream can
      // parse tool calls + completions. cwd is the spawn cwd
      // (set by runAgentTurn from req.base_ref's worktree path);
      // we deliberately DO NOT pass --cd here because req.repo is
      // a slug ("chitinhq/chitin"), not a filesystem path —
      // passing it would fail. -m sets model; ChatGPT Plus
      // default is gpt-5.4.
      const args = ['exec', '--json', '--skip-git-repo-check'];
      const model = (process.env.CHITIN_MODEL_CODEX ?? '').trim();
      if (model) args.push('-m', model);
      args.push(req.prompt);
      return {
        command: 'codex',
        args,
      };
    }
    case 'gemini': {
      // Gemini CLI on Google AI Pro plan. PreToolUse hook is wired via
      // ~/.gemini/settings.json BeforeTool — same wire shape as Claude
      // Code's PreToolUse, just a renamed event. Model selection via -m;
      // CHITIN_MODEL_GEMINI env override picks the per-call model
      // (defaults to whatever ~/.gemini/settings.json picks if unset).
      const args = ['-p', req.prompt];
      const model = (process.env.CHITIN_MODEL_GEMINI ?? '').trim();
      if (model) {
        args.push('-m', model);
      }
      return {
        command: 'gemini',
        args,
      };
    }
    case 'openclaw-glm-flash':
    case 'openclaw-glm-cloud':
    case 'openclaw-deepseek':
      // Dispatch through openclaw + chitin-governance plugin. The plugin
      // is loaded at openclaw startup (~/.openclaw/openclaw.json plugins.allow
      // includes "chitin-governance"); every tool call the local agent
      // dispatches passes through before_tool_call → chitin gate. The per-
      // driver agent mapping routes to distinct openclaw agents so each
      // tier runs its own model.
      //
      // No `--include-hook-events`: openclaw 2026.4.x doesn't support that
      // flag (it's claude-code-only). Passing it caused `openclaw agent` to
      // exit 1 immediately on every T0/T1 dispatch from 2026-05-04 17:00
      // UTC (shift-left routing reshuffle, when T0/T1 first started routing
      // to openclaw-glm-flash) until 2026-05-05 02:09 UTC (this fix), with
      // every run silently producing `commits_added: 0` and "agent made no
      // changes." Hook-event capture for openclaw runs happens via the
      // chitin-governance plugin's chain emission instead — the flag is
      // unnecessary on this path.
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
  // The hook command points at the chitin-router-hook wrapper, which
  // (a) calls chitin-kernel for the deterministic verdict, then
  // (b) runs heuristic plugins (blast-radius, floundering, drift),
  // (c) optionally consults an advisor LLM via `claude -p`,
  // (d) returns a composed allow/deny decision with optional nudge.
  //
  // The router is policy-controlled (chitin.yaml `router:` section)
  // — when policy.enabled=false the wrapper is a transparent
  // pass-through to the kernel, identical to direct kernel-hook
  // behavior.
  //
  // Hardcoded absolute path so the hook works from worktrees
  // without relying on PATH propagation. The bin script itself
  // resolves the TS module relative to its location.
  const routerHookBin =
    process.env.CHITIN_ROUTER_HOOK_BIN ?? '/home/red/workspace/chitin/bin/chitin-router-hook';
  const settings = {
    hooks: {
      PreToolUse: [
        {
          matcher: '',
          hooks: [
            {
              type: 'command' as const,
              command: `${routerHookBin} --agent=claude-code`,
            },
          ],
        },
      ],
    },
  };
  writeFileSync(settingsPath, JSON.stringify(settings, null, 2));
}

// Eliminates the swarm-worktree-orientation probe pattern observed in
// the 2026-05-03 skill-mining report (~/.chitin/events-*.jsonl over
// 87 sessions): every dispatched agent ran 5-10 ls/find/cat probes
// against the worktree to learn its layout. WORKTREE_INDEX.md gives
// the answer in one read.
//
// Contents are intentionally cheap to compute: top-level entries,
// nx plugins present, project.json count, pointers to docs the
// agent will need (CLAUDE.md, chitin.yaml, swarm-backlog.md).
//
// Cost: ~2 readdirs + 1 fs scan, run once at worktree provisioning.
// Benefit: ~10 fewer round-trips per agent turn.
function writeWorktreeIndex(
  worktreePath: string,
  workflowId: string,
  branch: string,
  baseRef: string,
): void {
  const lines: string[] = [];
  lines.push(`# Worktree index — ${workflowId}`);
  lines.push('');
  lines.push(`Generated by chitin temporal-worker at provisioning time. This file`);
  lines.push(`exists so dispatched agents do not need to probe the worktree layout`);
  lines.push(`with ls/find/cat. Read once instead of probing 5-10 times.`);
  lines.push('');
  lines.push('## Coordinates');
  lines.push('');
  lines.push(`- workflow_id: \`${workflowId}\``);
  lines.push(`- branch:      \`${branch}\``);
  lines.push(`- base_ref:    \`${baseRef}\``);
  lines.push(`- path:        \`${worktreePath}\``);
  lines.push('');

  // Top-level entries — the cheap "what's here" probe agents do via ls -la.
  lines.push('## Top-level entries');
  lines.push('');
  try {
    // Hide only the .git metadata dir — NOT .github / .gitignore / .gitattributes,
    // which are real top-level entries the agent needs to know exist.
    const entries = readdirSync(worktreePath, { withFileTypes: true })
      .filter((e) => e.name !== '.git')
      .sort((a, b) => a.name.localeCompare(b.name));
    for (const e of entries) {
      lines.push(`- ${e.isDirectory() ? '📁' : '📄'} \`${e.name}\``);
    }
  } catch {
    lines.push('- (readdir failed — worktree may have been pruned)');
  }
  lines.push('');

  // Nx plugin inventory — agents grep node_modules/@nx/ to learn this.
  const nxDir = join(worktreePath, 'node_modules', '@nx');
  lines.push('## Nx plugins (node_modules/@nx)');
  lines.push('');
  if (existsSync(nxDir)) {
    try {
      const plugins = readdirSync(nxDir, { withFileTypes: true })
        .filter((e) => e.isDirectory())
        .map((e) => e.name)
        .sort();
      if (plugins.length === 0) {
        lines.push('- (none)');
      } else {
        for (const p of plugins) lines.push(`- \`@nx/${p}\``);
      }
    } catch {
      lines.push('- (readdir failed)');
    }
  } else {
    lines.push('- (no node_modules/@nx — run pnpm install if you need nx)');
  }
  lines.push('');

  // Pointers to canonical docs the agent will need.
  lines.push('## Where to look');
  lines.push('');
  const docPointers: Array<{ path: string; what: string }> = [
    { path: 'CLAUDE.md', what: 'project conventions for Claude agents' },
    { path: 'chitin.yaml', what: 'governance policy active in this worktree' },
    { path: 'docs/swarm-backlog.md', what: 'the entry catalog this task came from' },
    { path: 'docs/architecture.md', what: 'three-plane architecture overview' },
    { path: 'docs/runbooks/', what: 'operator runbooks for chitin subsystems' },
    { path: 'package.json', what: 'monorepo scripts + workspaces' },
    { path: 'pnpm-workspace.yaml', what: 'package layout' },
    { path: 'apps/temporal-worker/', what: 'the worker that dispatched this task' },
    { path: 'libs/contracts/', what: 'ExecutionRequest + ActivityResult schemas' },
  ];
  for (const { path: p, what } of docPointers) {
    const exists = existsSync(join(worktreePath, p)) ? '✓' : '·';
    lines.push(`- ${exists} \`${p}\` — ${what}`);
  }
  lines.push('');

  // The "find . -name project.json" pattern was in 6 of the top n-grams.
  // Resolve it once here.
  lines.push('## Project manifests (project.json count)');
  lines.push('');
  try {
    const out = execFileSync(
      'find',
      ['.', '-name', 'project.json', '-not', '-path', './node_modules/*', '-not', '-path', './.git/*'],
      { cwd: worktreePath, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] },
    );
    const paths = out.trim().split('\n').filter(Boolean).sort();
    lines.push(`- count: ${paths.length}`);
    for (const p of paths.slice(0, 30)) lines.push(`- \`${p}\``);
    if (paths.length > 30) lines.push(`- … and ${paths.length - 30} more`);
  } catch {
    lines.push('- (find failed)');
  }
  lines.push('');

  lines.push('---');
  lines.push('Auto-generated. Re-running provisionWorktree regenerates this file.');

  writeFileSync(join(worktreePath, 'WORKTREE_INDEX.md'), lines.join('\n') + '\n');

  // Mark the index as ignored so the apply-step's `git add -A`
  // doesn't stage it into PRs. Git only consults `info/exclude` in
  // the COMMON git dir (worktrees share it via --git-common-dir),
  // so writing there once covers every worktree of the parent repo.
  // Side-effect: WORKTREE_INDEX.md in the operator's main checkout
  // would also be hidden from status — acceptable since there's no
  // reason to have one there.
  //
  // Idempotent: append the line only if not already present.
  try {
    const commonDir = execFileSync(
      'git',
      ['rev-parse', '--git-common-dir'],
      { cwd: worktreePath, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] },
    ).trim();
    // git rev-parse may return a relative path — resolve relative
    // to the worktree to get an absolute one.
    const commonDirAbs = commonDir.startsWith('/') ? commonDir : join(worktreePath, commonDir);
    const excludePath = join(commonDirAbs, 'info', 'exclude');
    mkdirSync(join(commonDirAbs, 'info'), { recursive: true });
    let prior = '';
    if (existsSync(excludePath)) prior = readFileSync(excludePath, 'utf8');
    if (!prior.split('\n').some((l) => l.trim() === 'WORKTREE_INDEX.md')) {
      const next = (prior.endsWith('\n') || prior === '' ? prior : prior + '\n') + 'WORKTREE_INDEX.md\n';
      writeFileSync(excludePath, next);
    }
  } catch {
    // Best-effort: the index file is still useful even if exclude
    // wiring fails. The apply step has its own checks for what to
    // commit — this is defense-in-depth, not the only line.
  }
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
    // Eliminates the swarm-orientation probe pattern (12% of all
    // chain decisions per the 2026-05-03 skill-mining report).
    // Best-effort: index generation failures must not block the
    // agent turn, so swallow + log.
    try {
      writeWorktreeIndex(worktreeInfo.path, req.workflow_id, worktreeInfo.branch, req.base_ref!);
    } catch (err) {
      console.warn(JSON.stringify({
        warning: 'worktree_index_write_failed',
        workflow_id: req.workflow_id,
        err: err instanceof Error ? err.message : String(err),
      }));
    }
    // Slice 6a: claude-code-headless needs project-level chitin hook
    // settings.json in the worktree so its tool calls actually gate.
    if (req.allowed_drivers.includes('claude-code-headless')) {
      writeWorktreeClaudeSettings(worktreeInfo.path);
    }
    // openclaw-driven drivers need the agent's workspace pointed at
    // the worktree so the agent's read/edit tools see the right files.
    // We provision a per-workflow OPENCLAW_STATE_DIR with a remapped
    // openclaw.json instead of mutating the user's global config.
    const openclawDriver = req.allowed_drivers.find((d) =>
      d === 'openclaw-glm-flash' || d === 'openclaw-glm-cloud' || d === 'openclaw-deepseek',
    );
    if (openclawDriver) {
      const agentId = resolveAgent(openclawDriver as DriverId);
      openclawState = provisionOpenclawState(req, worktreeInfo.path, agentId);
    }
  } else if (req.role === 'reviewer') {
    // Reviewers don't write code (write_policy:'none') but DO need
    // to read the actual repo: `gh pr diff <n>` requires a git
    // checkout, the prompt instructs them to read the changed files,
    // etc. Empty-tempdir mode (the slice-1..4 default below)
    // produced reviewer runs that found `not a git repository` on
    // the first tool call, escalated with low confidence, and the
    // gatekeeper cascaded to operator. Run the reviewer in repoRoot
    // directly — read-only access is governed by the kernel's
    // policy at the per-tool-call layer, not by isolating the cwd.
    workDir = repoRoot;
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
      // P2 routing-as-learning-system: surface the dispatch's identity
      // via env so the spawned agent's PreToolUse hook (chitin-kernel)
      // can stamp these onto every gov-decisions row. Empty strings are
      // dropped on the kernel side via omitempty.
      const dispatchModel = resolveDispatchModel(plan.command === 'chitin-kernel' ? 'copilot' :
        plan.command === 'claude' ? 'claude-code-headless' :
        plan.command === 'codex' ? 'codex' :
        plan.command === 'gemini' ? 'gemini' :
        (req.allowed_drivers[0] as DriverId), req.tier) ?? '';
      const child = spawn(plan.command, plan.args, {
        cwd: workDir,
        env: {
          ...process.env,
          ...(plan.env ?? {}),
          ...(openclawState?.envOverride ?? {}),
          CHITIN_WORKFLOW_ID: req.workflow_id,
          CHITIN_RUN_ID: req.run_id,
          CHITIN_MODEL: dispatchModel,
          CHITIN_ROLE: req.role ?? '',
          // CHITIN_FINGERPRINT will be populated once libs/contracts/
          // src/fingerprint.ts (PR #287) merges and the dispatcher
          // computes it from the resolved (driver, model, role, ...)
          // tuple. Until then, the kernel sees an empty fingerprint
          // and chain rows omit the field via omitempty.
          CHITIN_FINGERPRINT: process.env.CHITIN_FINGERPRINT ?? '',
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
    // Tempdir-only cleanup. Reviewer mode runs in repoRoot directly
    // (write_policy:'none' guarantees read-only); rm-ing it nukes the
    // monorepo checkout. Worktree mode is owned by the apply-step.
    if (!useWorktree && isWorkerOwnedPath(workDir, repoRoot)) {
      rmSync(workDir, { recursive: true, force: true });
    } else if (!useWorktree && workDir !== repoRoot) {
      console.warn(`[cleanup] Skipped rmSync on non-owned path: ${workDir}`);
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

  // Ollama Cloud usage capture: if this was an ollama-cloud fallback, capture quota headers and emit usage feed
  try {
    await captureOllamaCloudUsage(req, result);
  } catch (err) {
    // Non-fatal: log to stderr_tail for operator visibility
    const msg = err instanceof Error ? err.message : String(err);
    result.stderr_tail = `${result.stderr_tail}\n[ollama-cloud-usage-capture] ${msg}`.slice(-TAIL_BYTES);
  }
  return result;
}

// Capture Ollama Cloud quota headers and emit usage feed if applicable
async function captureOllamaCloudUsage(req: ExecutionRequest, result: ActivityResult): Promise<void> {
  // Only run for openclaw-glm-cloud driver (Ollama Cloud)
  if (!req.allowed_drivers.includes('openclaw-glm-cloud')) return;
  // Look for quota headers in tool_summary or stdout_tail
  const summary = result.tool_summary || parseToolSummary(result.stdout_tail || '');
  if (!summary || typeof summary !== 'object') return;
  // Expect quota headers in summary.headers or summary.quota or similar
  const headers = summary.headers || summary.quota || {};
  const rpm = Number(headers['X-RateLimit-Remaining'] ?? headers['x-ratelimit-remaining']);
  const tpm = Number(headers['X-RateLimit-Reset'] ?? headers['x-ratelimit-reset']);
  if (!Number.isFinite(rpm) && !Number.isFinite(tpm)) return;
  // Write usage feed
  const usageFeedPath = join(homedir(), '.cache/chitin/usage/ollama-cloud.json');
  const usage = {
    axis: 'rpm_tpm',
    observed_at: new Date().toISOString(),
    rpm: Number.isFinite(rpm) ? rpm : null,
    tpm: Number.isFinite(tpm) ? tpm : null,
    workflow_id: req.workflow_id,
    run_id: req.run_id,
  };
  mkdirSync(join(homedir(), '.cache/chitin/usage'), { recursive: true });
  writeFileSync(usageFeedPath, JSON.stringify(usage, null, 2));
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

// Re-export runGatekeeperNotify so the worker registers it as an
// activity. Workflows can't make HTTP calls; reviewGraphWorkflow
// proxies this for the terminal-state Slack digest.
export { runGatekeeperNotify } from './gatekeeper.ts';

// Re-export runCommentResponderEnqueue so the worker registers it
// as an activity. reviewGraphWorkflow calls this after a
// 'request-changes' verdict to spawn a comment-responder workflow
// that addresses the findings. Workflows can't spawn arbitrary
// top-level workflows directly (executeChild creates a parent-
// child); the activity bridges via a normal Temporal client.
export { runCommentResponderEnqueue } from './comment-responder/enqueue-activity.ts';

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
  writeWorktreeIndex,
  provisionOpenclawState,
  extractHookEvents,
  DRIVER_AGENT_MAP,
  SWARM_WORKTREES_ROOT,
  CLAUDE_TIER_MODEL,
  COPILOT_TIER_MODEL,
};
