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
import { mkdirSync, writeFileSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';
import { homedir } from 'node:os';
import type { ActivityResult } from './activity-types.ts';
import type { executeRequestWorkflow } from './workflow.ts';
import { parseBacklog, type BacklogEntry } from './grooming/parse-backlog.ts';

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

// Tier → driver routing. The cheapest driver capable of the work.
const TIER_DRIVER: Record<Tier, DriverId> = {
  T0: 'local-qwen',                  // free, mechanical
  T1: 'copilot',                     // Copilot GPT-4.1 free
  T2: 'claude-code-headless',        // claude-haiku-4-5
  T3: 'claude-code-headless',        // claude-sonnet-4-6
  T4: 'claude-code-headless',        // claude-opus-4-7
};

// Per-entry wall_timeout — short enough that a stuck workflow doesn't
// hold the queue long, generous enough that real work has room. The
// wall_timeout is enforced by activity SIGKILL (slice 7a). Activities
// that finish naturally before this don't pay the cost.
const TIER_WALL_TIMEOUT_S: Record<Tier, number> = {
  T0: 180,
  T1: 240,
  T2: 360,
  T3: 600,
  T4: 600,
};

const TIER_MAX_TOOL_CALLS: Record<Tier, number> = {
  T0: 10,
  T1: 15,
  T2: 25,
  T3: 50,
  T4: 50,
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
    return entry;
  }
  return null;
}

function buildPrompt(entry: BacklogEntry): string {
  // The entry's description (post-grooming) already contains the
  // implementation steps. Wrap it with a "do this end-to-end" frame
  // so the agent commits the change rather than just describing it.
  return `You are picking up a backlog entry. Read the description carefully, make the change in the current working directory, run any tests the entry requires, and commit your work with a clear message. If anything is ambiguous, do the conservative thing — do not invent scope. Do not modify chitin.yaml or any file under .chitin/ (governance is human-only).

ENTRY ID: ${entry.id}

ENTRY:
${entry.description}

When done, your worktree should have either commits or staged-but-uncommitted changes that the apply pipeline will push as a PR. Output a one-line summary of what you did, then stop.`;
}

async function main() {
  const dryRun = process.argv.includes('--dry-run');
  log('info', 'dispatcher start', { dryRun });

  const conn = await Connection.connect({ address: '127.0.0.1:7233' });
  const client = new Client({ connection: conn, namespace: 'default' });

  try {
    const running = await findRunningSwarmWorkflow(client);
    if (running) {
      log('info', 'swarm workflow already in flight; exiting', { running });
      return;
    }

    const entries = parseBacklog(BACKLOG_PATH);
    const entry = pickEntryToDispatch(entries);
    if (!entry) {
      log('info', 'no ready entry to dispatch this tick');
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
      prompt: buildPrompt(entry),
      base_ref: 'main',
      tier,
    });

    // Write the marker BEFORE submit. If submit fails, marker still
    // present → operator's intent is "this entry was attempted; don't
    // auto-retry." Manual rm to retry. This is intentionally pessimistic:
    // the swarm doesn't quietly retry without operator review.
    writeDispatchMarker(entry.id, workflowId, tier, driver);

    const handle = await client.workflow.start<typeof executeRequestWorkflow>(WORKFLOW_NAME, {
      args: [req],
      taskQueue: TASK_QUEUE,
      workflowId,
    });
    log('info', 'workflow started', { workflow_id: workflowId });

    const result = (await handle.result()) as ActivityResult;
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
    try {
      const applyOutput = execSync(
        `pnpm exec tsx apps/temporal-worker/src/grooming/apply-workflow-result.ts --result ${envelopePath} --apply`,
        { encoding: 'utf8' },
      );
      log('info', 'apply step output', { output: applyOutput.slice(-2000) });
    } catch (err) {
      log('warn', 'apply step failed (run manually)', {
        envelope: envelopePath,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  } finally {
    await conn.close();
  }
}

main().catch((err) => {
  log('error', 'dispatcher fatal', { error: err instanceof Error ? err.message : String(err) });
  process.exit(1);
});
