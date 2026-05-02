// Slice 7b: autonomous dispatcher. Runs on a schedule (systemd timer or
// equivalent), picks the next ready backlog entry, submits one workflow.
//
// Invariants:
//   - At most one swarm workflow in flight at a time. If any
//     swarm-<entry-id>-* workflow is currently RUNNING in Temporal, this
//     tick exits without dispatching.
//   - Each backlog entry is dispatched at most once per origin. If a
//     branch swarm/swarm-<entry-id>-* exists on origin (open or merged
//     PR), the entry is considered handled and skipped on subsequent
//     ticks.
//   - T5 entries are never dispatched (human-only escalation).
//
// Failure modes:
//   - Workflow submit fails: log to stderr, exit non-zero so systemd
//     surfaces the failure. Next tick retries.
//   - Workflow times out (wall_timeout SIGKILL fires per slice 7a fix):
//     activity returns exit_code=-1, apply step skips PR, branch may
//     still exist (push happened before close). Operator sees in
//     gov-decisions chain + Temporal UI.
//   - Apply step fails: branch is pushed but no PR. Operator runs
//     apply manually with the result file.
//
// Usage:
//   pnpm exec tsx apps/temporal-worker/src/dispatcher.ts [--dry-run]
//
//   --dry-run: print the entry that would be dispatched, exit 0
//   without submitting.

import { Connection, Client } from '@temporalio/client';
import { ExecutionRequestSchema } from '@chitin/contracts';
import type { DriverId, Tier } from '@chitin/contracts';
import { execFileSync, execSync } from 'node:child_process';
import { mkdirSync, readdirSync, writeFileSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { homedir } from 'node:os';
import { fileURLToPath } from 'node:url';
import type { ActivityResult } from './activity-types.ts';
import type { executeRequestWorkflow } from './workflow.ts';
import { parseBacklog, type BacklogEntry } from './grooming/parse-backlog.ts';
import { buildPromptForEntry, resolveEntryRole } from './role-prompts.ts';
import {
  notifyDispatchStart,
  notifyDispatchComplete,
  notifyDispatchError,
  notifyTickIdle,
  extractPrUrl,
} from './notify.ts';

const WORKFLOW_NAME = 'executeRequestWorkflow';
const TASK_QUEUE = 'chitin-worker-q';
const BACKLOG_PATH = resolve(process.cwd(), 'docs/swarm-backlog.md');
const TMP_DIR = resolve(process.cwd(), 'tmp');
// Slice 7b: persistent markers for dispatched entries. Prevents the
// infinite-loop case where the agent is denied on every tool call
// (e.g., the entry references chitin.yaml, which no-governance-self-
// modification blocks): worktree stays clean, no branch is pushed,
// next tick re-picks the same entry forever. Operator deletes the
// marker to allow re-dispatch.
const STATE_DIR = resolve(homedir(), '.cache/chitin/swarm-state/dispatched');

// Tier → driver routing. The cheapest reliable driver capable of the
// work. local-qwen is architecturally the right T0 driver (free, on-
// the-3090, mechanical) but qwen3-coder:30b on this rig is currently
// unstable (ollama stream crashes mid-generation; agent
// misinterprets relative paths as absolute; scope drift onto files
// outside the entry). Slice 7-tuning's first live run uncovered all
// three. Routing T0 → copilot temporarily until the qwen layer is
// fixed (those fixes are backlog entries; the swarm itself produces
// PRs for them via this same dispatcher). Cost: still $0 under
// Jared's Copilot plan. One-line revert to local-qwen once the local
// model is reliable.
// 2026-05-02: T2 temporarily routed to copilot. The overnight 2026-05-02
// run produced 4/4 bucket-B contaminated PRs on T2/T3 claude-code-
// headless (and 1/5 successes total). Root cause: the worker's
// writeWorktreeClaudeSettings overwrites the worktree's
// .claude/settings.json, and the apply step's "tracked diff" heuristic
// auto-commits + ships the modification as task work whenever the
// agent declined to commit real edits. The apply-step revert in
// apply-workflow-result.revertWorktreeSettingsArtifact closes the
// auto-commit path; once it's been live for a swarm cycle and the
// rate stays at 0, flip T2 (and T3) back to claude-code-headless.
// See docs/observations/2026-05-02-bucket-b-after-action.md.
const TIER_DRIVER: Record<Tier, DriverId> = {
  T0: 'copilot',                     // Copilot GPT-4.1 free (was local-qwen — see comment)
  T1: 'copilot',                     // Copilot GPT-4.1 free
  T2: 'copilot',                     // (was claude-code-headless — see comment above; flip back after CCH bucket-B rate stays at 0)
  T3: 'claude-code-headless',        // claude-sonnet-4-6
  T4: 'claude-code-headless',        // claude-opus-4-7
};

// Per-entry wall_timeout — short enough that a stuck workflow doesn't
// hold the queue long, generous enough that real work has room. The
// wall_timeout is enforced by activity SIGKILL (slice 7a) — stuck
// processes get killed at exactly this+1s, regardless of how long the
// budget is. So generous budgets cost nothing for healthy runs and
// only cost time-to-fail-detection for stuck ones.
//
// Slice 7-tuning history:
//   180s (slice 7) — too short, even healthy runs hit it
//   480s (first tuning) — productive for copilot but local-qwen still
//                         couldn't finish stable runs
//   1200s/1800s (this tuning) — give complex T2+ work real room. The
//                         SIGKILL fix means stuck = killed-at-budget,
//                         not infinite hang, so 30min ceiling is safe.
//
// Operator override: if a specific entry needs even more (slice 3-style
// rewrites that span dozens of files), override per workflow via
// the request's bounds.wall_timeout_s — the dispatcher's tier value
// is just the default.
const TIER_WALL_TIMEOUT_S: Record<Tier, number> = {
  T0: 1200,  // 20 min — mechanical work has room to read, edit, test, commit
  T1: 1200,  // 20 min
  T2: 1800,  // 30 min — specialized reasoning + multi-file
  T3: 1800,  // 30 min
  T4: 1800,  // 30 min — even Opus gets a fair budget
};

const TIER_MAX_TOOL_CALLS: Record<Tier, number> = {
  T0: 15,
  T1: 20,
  T2: 30,
  T3: 60,
  T4: 60,
};

function git(args: string[]): string {
  return execFileSync('git', args, { encoding: 'utf8' }).trim();
}

function log(level: 'info' | 'warn' | 'error', msg: string, data?: Record<string, unknown>) {
  const line = JSON.stringify({
    ts: new Date().toISOString(),
    level,
    component: 'dispatcher',
    msg,
    ...(data ?? {}),
  });
  // info → stdout, warn/error → stderr (systemd journal separates them).
  if (level === 'info') console.log(line);
  else console.error(line);
}

function sanitize(s: string): string {
  return s.replace(/[^a-zA-Z0-9_\-:.]/g, '-');
}

// True if origin has any branch matching swarm/swarm-<entry-id>-* — i.e.,
// the entry has been dispatched to completion (branch persists past PR
// merge unless deleted, and stays through PR open). We fetch first so the
// remote ref state is current; auto-prune deleted remote branches.
function entryHasOriginBranch(entryId: string): boolean {
  try {
    git(['fetch', '--prune', 'origin']);
  } catch (err) {
    log('warn', 'git fetch failed; falling back to cached refs', {
      error: err instanceof Error ? err.message : String(err),
    });
  }
  try {
    const out = git([
      'for-each-ref',
      '--format=%(refname:short)',
      `refs/remotes/origin/swarm/swarm-${entryId}-*`,
    ]);
    return out.length > 0;
  } catch {
    return false;
  }
}

// Find any currently-running swarm workflow via the Temporal client.
// We use the visibility list-workflows endpoint with a simple status
// filter. Returns the workflow_id of the first running one, or null.
async function findRunningSwarmWorkflow(client: Client): Promise<string | null> {
  // The query language Temporal supports varies by server config; fall
  // back to a broad list + manual filter. Limit to a small number for
  // cheap polling.
  for await (const wf of client.workflow.list({ query: "ExecutionStatus='Running'" })) {
    if (wf.workflowId.startsWith('swarm-')) {
      return wf.workflowId;
    }
  }
  return null;
}

function entryHasDispatchMarker(entryId: string): boolean {
  const path = resolve(STATE_DIR, `${entryId}.json`);
  return existsSync(path);
}

function writeDispatchMarker(entryId: string, workflowId: string, tier: Tier, driver: DriverId) {
  mkdirSync(STATE_DIR, { recursive: true });
  const path = resolve(STATE_DIR, `${entryId}.json`);
  writeFileSync(
    path,
    JSON.stringify(
      {
        entry_id: entryId,
        workflow_id: workflowId,
        tier,
        driver,
        dispatched_at: new Date().toISOString(),
      },
      null,
      2,
    ),
  );
}

function pickEntryToDispatch(entries: BacklogEntry[]): BacklogEntry | null {
  for (const entry of entries) {
    if (entry.status !== 'ready') continue;
    // T5 is human-only — skip even if the schema permits it (it doesn't,
    // but the tier field on the file might).
    if (entry.tier === 'T5') {
      log('info', 'skip T5 entry (human-only)', { entry_id: entry.id });
      continue;
    }
    if (!entry.tier || !(entry.tier in TIER_DRIVER)) {
      log('warn', 'skip entry without recognized tier', {
        entry_id: entry.id,
        tier: entry.tier,
      });
      continue;
    }
    if (entryHasOriginBranch(entry.id)) {
      log('info', 'skip entry: swarm/<id> branch exists on origin', { entry_id: entry.id });
      continue;
    }
    if (entryHasDispatchMarker(entry.id)) {
      log('info', 'skip entry: dispatch marker present (operator must rm to retry)', {
        entry_id: entry.id,
        marker: resolve(STATE_DIR, `${entry.id}.json`),
      });
      continue;
    }
    // --- BLOCKS FIELD HANDLING ---
    if (Array.isArray(entry.blocks) && entry.blocks.length > 0) {
      for (const blockerId of entry.blocks) {
        // Unknown blockers are treated as not-yet-dispatched (skip)
        // Blocked if: blocker has a dispatch marker (in-flight, not complete), or no PR yet
        if (entryHasDispatchMarker(blockerId)) {
          log('info', 'skip entry: blocked-by', {
            entry_id: entry.id,
            blocked_by: blockerId,
            blocker_state: 'dispatch_marker_present',
          });
          continue;
        }
        // If blocker has an origin branch, it's shipped (PR open or merged) — do not skip
        if (!entryHasOriginBranch(blockerId)) {
          log('info', 'skip entry: blocked-by', {
            entry_id: entry.id,
            blocked_by: blockerId,
            blocker_state: 'not_yet_dispatched',
          });
          continue;
        }
      }
      // If any blocker caused a skip, continue to next entry
      // (Handled by continue in the loop above)
    }
    return entry;
  }
  return null;
}

export function buildPrompt(entry: BacklogEntry): string {
  // Slice 7-tuning: rewritten to be directive about tool use and
  // shut off chat-style replies. The pre-tuning prompt let qwen3-
  // coder:30b answer with a markdown plan instead of dispatching
  // tools; both end-of-slice-7 runs hit wall_timeout with no work.
  // This version gives a concrete first action, names the tools the
  // agent must call, and explicitly forbids chat-only output.
  //
  // The entry's description (post-grooming) contains implementation
  // steps. We DO NOT echo the steps inside the prompt — we point the
  // agent at the file and tell it to use the read tool. The
  // verbose-step echo was likely tempting the model into "summarize
  // the steps" mode rather than executing them.
  const rawFile = entry.file?.split(',')[0]?.trim();
  let targetFile: string;
  if (rawFile) {
    targetFile = rawFile.startsWith('./') || rawFile.startsWith('/')
      ? rawFile
      : `./${rawFile}`;
  } else {
    targetFile = 'the file named in the entry';
  }
  return `You are a swarm worker executing one backlog entry. Output text is ignored — only TOOL DISPATCHES count. If you finish without dispatching tools, the work is lost.

ENTRY ID: ${entry.id}
TARGET FILE: ${targetFile}

YOUR FIRST ACTION: dispatch the \`read\` tool on ${targetFile}. Do not respond with text first. Read the file, understand the change required, then dispatch \`edit\` or \`write\` to make the change. Run \`exec\` if tests are needed. Finally dispatch \`exec\` with a git command to commit your work (e.g., git add -A && git commit -m "..."), so the apply pipeline can push the branch.

ENTRY DETAIL (frontmatter + description):
${entry.description}

CONSTRAINTS:
- Do not modify chitin.yaml or anything under .chitin/ — governance is human-only and chitin's gate will deny those writes anyway.
- Only edit files referenced in the entry. Do not invent scope.
- Forbid editing files not named in the entry's \`file\` field, and instruct the agent to \`read\` ONLY the target file before editing.
- If you decide the entry is misclassified or requires human judgment, exit without committing — empty worktrees are not pushed.

REMEMBER: chat replies do nothing. Tool calls are the only thing that produces work. Start by reading ${targetFile} now.`;
}

// Returns true if the dispatcher should skip this tick. Looks for any
// `.claude/settings.json.chitin-backup-*` artifact in the given cwd's
// `.claude/` directory — that file gets swept into worktree diffs by
// `git add -A` in the apply step and produces bucket-B contaminated
// PRs (see docs/observations/2026-05-02-bucket-b-after-action.md).
//
// Exported for tests via `__test__`. Pure modulo `existsSync`/
// `readdirSync` calls — operator-facing logging + idle-notify happen
// in the caller.
function findClaudeBackupArtifact(cwd: string): string | null {
  const claudeDir = resolve(cwd, '.claude');
  if (!existsSync(claudeDir)) return null;
  const re = /^settings\.json\.chitin-backup-.+$/;
  for (const file of readdirSync(claudeDir)) {
    if (re.test(file)) return resolve(claudeDir, file);
  }
  return null;
}

async function preflightRefuseOnClaudeBackup(): Promise<boolean> {
  const artifact = findClaudeBackupArtifact(process.cwd());
  if (!artifact) return false;
  log('warn', 'preflight: claude-settings-backup artifact present', { artifact });
  await notifyTickIdle(`preflight: claude-settings-backup artifact present (${artifact})`);
  return true;
}

async function main() {
  // Pre-flight: refuse to dispatch if any .claude/settings.json.chitin-backup-*
  // artifact is present in the cwd's .claude/ dir. Background: a leftover
  // backup file got swept into worktree diffs by `git add -A` in the apply
  // step on 2026-05-02, producing four bucket-B contaminated PRs whose diff
  // didn't match their title. See docs/observations/2026-05-02-bucket-b-
  // after-action.md. Default policy is option-1 from the backlog entry —
  // hard refuse, operator deletes the artifact manually before next tick.
  if (await preflightRefuseOnClaudeBackup()) return;

  const dryRun = process.argv.includes('--dry-run');
  log('info', 'dispatcher start', { dryRun });

  const conn = await Connection.connect({ address: '127.0.0.1:7233' });
  const client = new Client({ connection: conn, namespace: 'default' });

  try {
    const running = await findRunningSwarmWorkflow(client);
    if (running) {
      log('info', 'swarm workflow already in flight; exiting', { running });
      await notifyTickIdle(`workflow already in flight (${running})`);
      return;
    }

    const entries = parseBacklog(BACKLOG_PATH);
    const entry = pickEntryToDispatch(entries);
    if (!entry) {
      log('info', 'no ready entry to dispatch this tick');
      await notifyTickIdle('no ready entry to dispatch');
      return;
    }
    const tier = entry.tier as Tier;
    const driver = TIER_DRIVER[tier];
    const wallTimeout = TIER_WALL_TIMEOUT_S[tier];
    const maxToolCalls = TIER_MAX_TOOL_CALLS[tier];
    const workflowId = `swarm-${sanitize(entry.id)}-${Date.now()}`;

    log('info', 'selected entry', {
      entry_id: entry.id,
      tier,
      driver,
      wall_timeout_s: wallTimeout,
      workflow_id: workflowId,
    });

    if (dryRun) {
      log('info', 'dry-run; would submit', { workflow_id: workflowId });
      return;
    }

    // Phase 1 (factory design §3-4): pick the prompt template based
    // on the entry's role. Unknown / absent role → programmer (the
    // pre-Phase-1 default).
    const { role: resolvedRole, warning: roleWarning } = resolveEntryRole(entry);
    if (roleWarning) log('warn', 'role-resolution', { entry_id: entry.id, warning: roleWarning });

    const req = ExecutionRequestSchema.parse({
      schema_version: '1',
      workflow_id: workflowId,
      run_id: `${workflowId}-attempt-1`,
      repo: 'chitinhq/chitin',
      task_class: 'refactor',
      risk_level: 'low',
      allowed_drivers: [driver],
      network_policy: 'allowlist',
      write_policy: 'worktree',
      bounds: { max_tool_calls: maxToolCalls, max_cost_usd: 0, wall_timeout_s: wallTimeout },
      prompt: buildPromptForEntry(entry),
      base_ref: 'main',
      tier,
      role: resolvedRole,
      // Phase 1 surfaces the parent_workflow_id + step_index fields
      // on the schema; the multi-step flow that uses them lands with
      // the review-graph-executor in Phase 2. Top-level dispatches
      // leave these undefined.
    });

    // Write the marker BEFORE submit. If submit fails, marker still
    // present → operator's intent is "this entry was attempted; don't
    // auto-retry." Manual rm to retry. This is intentionally pessimistic:
    // the swarm doesn't quietly retry without operator review.
    writeDispatchMarker(entry.id, workflowId, tier, driver);

    let handle;
    try {
      handle = await client.workflow.start<typeof executeRequestWorkflow>(WORKFLOW_NAME, {
        args: [req],
        taskQueue: TASK_QUEUE,
        workflowId,
      });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      await notifyDispatchError({ entry_id: entry.id, workflow_id: workflowId, stage: 'submit', error: msg });
      throw err;
    }
    // notifyDispatchStart fires AFTER successful submit so a failed
    // workflow.start() doesn't claim a workflow exists in Slack that
    // never actually got created.
    await notifyDispatchStart({ entry_id: entry.id, tier, driver, workflow_id: workflowId });
    log('info', 'workflow started', { workflow_id: workflowId });

    let result: ActivityResult;
    try {
      result = (await handle.result()) as ActivityResult;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      await notifyDispatchError({ entry_id: entry.id, workflow_id: workflowId, stage: 'workflow', error: msg });
      throw err;
    }
    mkdirSync(TMP_DIR, { recursive: true });
    const envelopePath = resolve(TMP_DIR, `result-${workflowId}.json`);
    writeFileSync(
      envelopePath,
      JSON.stringify(
        {
          workflow_id: workflowId,
          result,
          pr_title: `swarm: ${entry.id}`,
          pr_body:
            `Picked up by the autonomous dispatcher (slice 7).\n\n` +
            `Backlog entry: \`${entry.id}\`\n` +
            `Tier: \`${tier}\`  •  Driver: \`${driver}\`\n` +
            `Workflow: \`${workflowId}\`\n\n` +
            `## Entry context\n\n${entry.description.slice(0, 4000)}\n`,
        },
        null,
        2,
      ),
    );
    log('info', 'workflow complete', {
      workflow_id: workflowId,
      exit_code: result.exit_code,
      duration_ms: result.duration_ms,
      worktree_present: !!result.worktree,
      commits_added: result.worktree?.commits_added ?? 0,
      uncommitted: result.worktree?.has_uncommitted_changes ?? false,
    });

    // Run apply step inline. apply-workflow-result.ts handles push + PR
    // when the worktree has work, no-ops otherwise. Best-effort: failures
    // are logged but don't propagate (operator can run manually).
    let applyOutput = '';
    let applyFailed: Error | null = null;
    try {
      applyOutput = execSync(
        `pnpm exec tsx apps/temporal-worker/src/grooming/apply-workflow-result.ts --result ${envelopePath} --apply`,
        { encoding: 'utf8' },
      );
      log('info', 'apply step output', { output: applyOutput.slice(-2000) });
    } catch (err) {
      applyFailed = err instanceof Error ? err : new Error(String(err));
      log('warn', 'apply step failed (run manually)', {
        envelope: envelopePath,
        error: applyFailed.message,
      });
    }

    const prUrl = extractPrUrl(applyOutput);
    // apply-workflow-result.ts catches `gh pr create` failures and returns
    // null instead of throwing, so applyFailed is null but no PR landed.
    // Detect the warning it emits so the operator still sees a Slack alert.
    const prCreateSilentlyFailed =
      !applyFailed &&
      !prUrl &&
      /\[apply-result\] gh pr create failed/.test(applyOutput);
    // The push step ran iff we got past the apply step (no exception) AND
    // apply didn't bail on "no committed work; skipping push and PR." If
    // apply ran the auto-commit branch + pushed, commits_added (captured
    // before apply ran) understates reality.
    const applyAutoCommitted = /\[apply-result\] auto-committing tracked uncommitted changes/.test(applyOutput);
    const pushed = !applyFailed && /\[apply-result\] pushing /.test(applyOutput);

    if (applyFailed) {
      await notifyDispatchError({
        entry_id: entry.id,
        workflow_id: workflowId,
        stage: 'apply',
        error: applyFailed.message,
      });
    } else if (prCreateSilentlyFailed) {
      await notifyDispatchError({
        entry_id: entry.id,
        workflow_id: workflowId,
        stage: 'apply',
        error:
          `gh pr create failed (branch ${result.worktree?.branch ?? '?'} pushed but no PR opened — open manually: ` +
          `gh pr create --head ${result.worktree?.branch ?? '<branch>'} --title "swarm: ${entry.id}")`,
      });
    }
    await notifyDispatchComplete({
      entry_id: entry.id,
      workflow_id: workflowId,
      exit_code: result.exit_code,
      duration_ms: result.duration_ms,
      commits_added: result.worktree?.commits_added ?? 0,
      uncommitted: result.worktree?.has_uncommitted_changes ?? false,
      pr_url: prUrl,
      apply_failed: !!applyFailed || prCreateSilentlyFailed,
      pushed,
      auto_committed: applyAutoCommitted,
    });
  } finally {
    await conn.close();
  }
}

export const __test__ = { findClaudeBackupArtifact };

// Only auto-run when invoked as a script. Importing buildPrompt or other
// helpers from tests must not open a Temporal connection.
const isMain = process.argv[1] === fileURLToPath(import.meta.url);
if (isMain) {
  main().catch((err) => {
    log('error', 'dispatcher fatal', { error: err instanceof Error ? err.message : String(err) });
    process.exit(1);
  });
}
