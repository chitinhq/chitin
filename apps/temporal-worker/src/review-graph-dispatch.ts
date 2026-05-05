// Phase 2 step 3b: dispatcher → review-graph integration. After the
// programmer dispatcher's apply step opens a PR, this enqueues the
// review-graph so the §5 escalation chain runs in parallel with
// whatever the dispatcher does next.
//
// Pre-cut-over (Temporal): client.workflow.start(reviewGraphWorkflow).
// Now: detached spawn of `lobster run --file review-graph.lobster
// --args-json '...'`. The lobster runtime cascades R1 → R2 → R3 with
// approval gate at R3 → operator-pickup. Same fire-and-forget contract
// as before (the dispatcher's tick doesn't wait on the review).
//
// Failure mode: if the lobster spawn fails, log and move on. The PR
// still exists; operator can manually start the review or merge based
// on Copilot's R0. We never want a review-graph failure to leave the
// dispatcher stuck (the implementor's work has shipped).

import { spawn as nodeSpawn } from 'node:child_process';
import { mkdirSync, openSync, appendFileSync, readdirSync, statSync } from 'node:fs';
import { resolve } from 'node:path';
import type { BacklogEntry } from './grooming/parse-backlog.ts';
import type { WorktreeResult } from './activity-types.ts';
import { resolveReviewTierDriver, type PrMeta, type ReviewTier } from './review-graph.ts';

const LOBSTER_BIN =
  process.env.LOBSTER_BIN ?? `${process.env.HOME ?? ''}/.local/bin/lobster`;
const DEFAULT_LOBSTER_FILE = resolve(
  process.cwd(),
  'apps/temporal-worker/workflows/review-graph.lobster',
);
const DEFAULT_LOG_DIR = resolve(
  process.env.HOME ?? '/tmp',
  '.cache/chitin/review-graph-logs',
);

/**
 * The log file at `${LOG_DIR}/<reviewGraphId>.log` is also our dedup
 * oracle: if it exists with mtime within RUNNING_WINDOW_MS, treat the
 * graph as in-flight. Pre-cut-over the dedup came from Temporal's
 * visibility query; lobster doesn't expose an equivalent API for
 * active runs (only paused workflows in LOBSTER_STATE_DIR), so we
 * stand up our own mtime-based oracle.
 *
 * Window default 60 min: longest realistic lobster review-graph
 * runtime (R3/Opus is the slow tier; observed ~5-15 min). Anything
 * older is stale — likely crashed or the operator never approved
 * the gate — so let the next ingester tick re-spawn.
 */
const RUNNING_WINDOW_MS = parseInt(
  process.env.LOBSTER_REVIEW_GRAPH_RUNNING_WINDOW_MS ?? '3600000', 10,
);

function logDir(): string {
  return process.env.LOBSTER_REVIEW_GRAPH_LOG_DIR ?? DEFAULT_LOG_DIR;
}

function logPathFor(reviewGraphId: string): string {
  return resolve(logDir(), `${reviewGraphId}.log`);
}

/**
 * True iff `<reviewGraphId>.log` exists and was touched within the
 * dedup window. Caller of enqueueReviewGraph uses this to skip the
 * spawn when an in-flight graph already exists.
 */
function isReviewGraphRunning(reviewGraphId: string, now: number = Date.now()): boolean {
  try {
    const st = statSync(logPathFor(reviewGraphId));
    return now - st.mtimeMs < RUNNING_WINDOW_MS;
  } catch {
    return false;  // no log file → not running
  }
}

/**
 * List all currently in-flight review-graph workflow ids, by
 * scanning the log dir for files whose mtime is within the dedup
 * window. Replaces the pre-cut-over Temporal visibility query.
 */
export function listRunningReviewGraphWorkflowsFromDisk(now: number = Date.now()): Set<string> {
  const ids = new Set<string>();
  let entries: string[];
  try {
    entries = readdirSync(logDir());
  } catch {
    return ids;  // log dir doesn't exist yet → no in-flight graphs
  }
  for (const name of entries) {
    if (!name.endsWith('.log')) continue;
    const fullPath = resolve(logDir(), name);
    try {
      const st = statSync(fullPath);
      if (now - st.mtimeMs < RUNNING_WINDOW_MS) {
        ids.add(name.slice(0, -'.log'.length));
      }
    } catch {
      // file disappeared between readdir and stat — skip
    }
  }
  return ids;
}

export interface LobsterSpawnInput {
  /** Stable id used for the per-run log file name + telemetry correlation. */
  reviewGraphId: string;
  /** Pre-serialized args object passed to `lobster run --args-json`. */
  argsJson: string;
}

/**
 * Default lobster spawn — detached + log-redirected. Throws synchronously
 * if the binary can't be located (caller's try/catch surfaces it as a
 * warn log, same shape as the old Temporal-submit-failed branch).
 */
async function defaultSpawnLobster(input: LobsterSpawnInput): Promise<void> {
  const lobsterFile = process.env.LOBSTER_REVIEW_GRAPH_FILE ?? DEFAULT_LOBSTER_FILE;
  mkdirSync(logDir(), { recursive: true });
  const logPath = logPathFor(input.reviewGraphId);
  const out = openSync(logPath, 'a');

  const child = nodeSpawn(
    LOBSTER_BIN,
    ['run', '--file', lobsterFile, '--mode', 'tool', '--args-json', input.argsJson],
    { detached: true, stdio: ['ignore', out, out] },
  );
  child.on('error', (err) => {
    try {
      appendFileSync(logPath, `\n[spawn-error] ${err.message}\n`);
    } catch {
      // best-effort
    }
  });
  if (!child.pid) {
    throw new Error(`lobster spawn returned no pid (binary not found at ${LOBSTER_BIN}?)`);
  }
  child.unref();
}

export interface EnqueueReviewGraphInput {
  /** The programmer workflow_id that just finished. Becomes the
   *  parent_workflow_id on every reviewer dispatch the graph fires. */
  parent_workflow_id: string;
  /** PR URL the apply step opened. Required — without it the graph
   *  has no PR to review and we silently no-op. */
  pr_url: string | undefined;
  /** Worktree result from the programmer's activity — source of
   *  truth for diff stats. May be undefined if the activity didn't
   *  produce a worktree (rare; review-graph still runs but with
   *  diff_loc=0, files_changed=0). */
  worktree: WorktreeResult | undefined;
  /** Backlog entry the programmer worked off. Drives tier rules
   *  (e.g., T3 implementor → start review at R2). */
  entry: BacklogEntry;
  /** Repo slug, e.g. 'chitinhq/chitin'. Used by reviewer agents to
   *  call gh CLI. */
  repo: string;
  /** Optional pre-built PrMeta. When set, the dispatcher skips its
   *  worktree-derived rebuild and threads this object through to
   *  the lobster review-graph as-is. Callers like `pr-event-ingester`
   *  that don't have a worktree but DO have authoritative diff
   *  stats from `gh pr view` should pass this so the §5 trigger
   *  matrix evaluates against real metadata, not zeros. */
  pr_meta?: PrMeta;
  /** Optional log sink (defaults to console.log). Tests inject. */
  log?: (line: string) => void;
  /** Injectable for tests. Default spawns lobster detached. */
  spawnLobster?: (input: LobsterSpawnInput) => Promise<void>;
}

export interface EnqueueReviewGraphResult {
  /** Whether the review-graph spawn was actually fired. False when
   *  pr_url was absent (PR didn't open), spawn failed, or the dedup
   *  oracle says one is already in flight. */
  enqueued: boolean;
  /** The review-graph workflow_id, when enqueued (or when skipped
   *  because one is already running — caller can correlate via id). */
  workflow_id?: string;
  /** Failure message when enqueued=false because of a spawn error. */
  error?: string;
  /** When set, enqueued=false because the dedup oracle saw a recent
   *  log-file mtime for this workflow_id. Caller should log as info,
   *  not warn — the in-flight graph will continue without disturbance. */
  skipped_already_running?: boolean;
}

/**
 * Build the review-graph input from the dispatcher's post-apply state.
 * Pure — exported so tests can pin the field-mapping invariant
 * without standing up a Temporal client.
 */
export function buildReviewGraphInput(
  parent_workflow_id: string,
  pr_url: string,
  worktree: WorktreeResult | undefined,
  entry: BacklogEntry,
  repo: string,
): { parent_workflow_id: string; pr_meta: PrMeta; entry: BacklogEntry; repo: string } {
  const pr_number = extractPrNumber(pr_url);
  const { diff_loc, files_changed } = parseDiffShortstat(worktree?.diff_shortstat ?? '');
  return {
    parent_workflow_id,
    pr_meta: {
      diff_loc,
      files_changed,
      // We don't enumerate the file list here — git would have to be
      // re-invoked from a worktree that may be cleaned up. Reviewers
      // can `gh pr diff` if they need it. Empty list = "unknown,
      // skip path-scope-based bumps" per PrMeta docstring.
      files: [],
      pr_url,
      pr_number,
      // copilot_comment_count is undefined on first dispatch — Copilot
      // is racing us. Reviewer prompt handles that branch.
    },
    entry,
    repo,
  };
}

/**
 * Parse a `git diff --shortstat`-style line into (LOC, files_changed).
 * Defensive for the empty-diff case (worktree had no change to commit
 * but a PR opened on whatever was already pushed — rare but real).
 */
export function parseDiffShortstat(s: string): { diff_loc: number; files_changed: number } {
  if (!s) return { diff_loc: 0, files_changed: 0 };
  // Format: " 4 files changed, 123 insertions(+), 45 deletions(-)"
  const filesMatch = s.match(/(\d+)\s+files?\s+changed/);
  const insMatch = s.match(/(\d+)\s+insertions?/);
  const delMatch = s.match(/(\d+)\s+deletions?/);
  const ins = insMatch ? Number(insMatch[1]) : 0;
  const del = delMatch ? Number(delMatch[1]) : 0;
  return {
    diff_loc: ins + del,
    files_changed: filesMatch ? Number(filesMatch[1]) : 0,
  };
}

/**
 * Pull the numeric PR number off a github.com/<o>/<r>/pull/N URL.
 * Returns undefined when the URL doesn't match the expected shape
 * (defensive — caller should still enqueue with pr_number=undefined
 * and let the reviewer-prompt builder throw the schema rejection).
 */
export function extractPrNumber(pr_url: string): number | undefined {
  const m = pr_url.match(/\/pull\/(\d+)/);
  return m ? Number(m[1]) : undefined;
}

/**
 * Spawn the review-graph as a detached lobster run. Failure is logged
 * but never propagates — the implementor's work has already shipped
 * (PR exists); a missing review is not worse than the pre-Phase-2
 * baseline where Copilot was the only reviewer.
 */
export async function enqueueReviewGraph(
  input: EnqueueReviewGraphInput,
): Promise<EnqueueReviewGraphResult> {
  const log = input.log ?? ((l: string) => console.log(l));
  const spawnLobster = input.spawnLobster ?? defaultSpawnLobster;

  if (!input.pr_url) {
    return { enqueued: false };
  }

  const reviewGraphIdEarly = `${input.parent_workflow_id}-review-graph`;
  if (isReviewGraphRunning(reviewGraphIdEarly)) {
    log(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'info',
        component: 'review-graph-dispatch',
        msg: 'review-graph already in flight; skipping spawn',
        parent_workflow_id: input.parent_workflow_id,
        review_graph_id: reviewGraphIdEarly,
        pr_url: input.pr_url,
      }),
    );
    return { enqueued: false, workflow_id: reviewGraphIdEarly, skipped_already_running: true };
  }

  // If a pre-built PrMeta was provided (pr-event-ingester path),
  // thread it through unchanged. Otherwise fall back to the
  // worktree-derived rebuild (programmer-success path).
  const baseInput = input.pr_meta
    ? {
        parent_workflow_id: input.parent_workflow_id,
        pr_meta: input.pr_meta,
        entry: input.entry,
        repo: input.repo,
      }
    : buildReviewGraphInput(
        input.parent_workflow_id,
        input.pr_url,
        input.worktree,
        input.entry,
        input.repo,
      );

  // Resolve per-tier driver+model in regular Node and embed in the
  // input. (Pre-cut-over the workflow isolate couldn't read process.env;
  // post-cut-over the lobster .lobster file just shells out to
  // chitin-agent-runner which CAN read env, so this is technically
  // redundant — but kept so the args envelope shape stays stable for
  // any downstream consumer that reads it.)
  const tier_config: Record<ReviewTier, { driver: string | null; model: string | null }> = {
    R0: { driver: null, model: null },
    R1: resolveReviewTierDriver('R1'),
    R2: resolveReviewTierDriver('R2'),
    R3: resolveReviewTierDriver('R3'),
    R4: { driver: null, model: null },
  };
  const reviewGraphInput = { ...baseInput, tier_config };

  // Greppable id: operator can correlate `swarm-<entry>-<ts>` to its
  // review-graph chain via `<parent>-review-graph` log file name.
  const reviewGraphId = `${input.parent_workflow_id}-review-graph`;

  // .lobster file's `args:` is a flat key→string map (lobster doesn't
  // type args). Translate the structured ReviewGraphInput accordingly.
  const lobsterArgs = {
    pr_meta_json: JSON.stringify(reviewGraphInput.pr_meta),
    entry_id: input.entry.id,
    entry_file_scope: input.entry.file ?? '',
    repo: input.repo,
    parent_workflow_id: input.parent_workflow_id,
    required_approver: process.env.LOBSTER_REQUIRED_APPROVER ?? '',
  };

  try {
    await spawnLobster({ reviewGraphId, argsJson: JSON.stringify(lobsterArgs) });
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    log(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'warn',
        component: 'review-graph-dispatch',
        msg: 'spawn failed; PR will rely on Copilot R0 only',
        parent_workflow_id: input.parent_workflow_id,
        pr_url: input.pr_url,
        error: msg,
      }),
    );
    return { enqueued: false, error: msg };
  }

  log(
    JSON.stringify({
      ts: new Date().toISOString(),
      level: 'info',
      component: 'review-graph-dispatch',
      msg: 'review-graph enqueued',
      parent_workflow_id: input.parent_workflow_id,
      review_graph_id: reviewGraphId,
      pr_url: input.pr_url,
      diff_loc: reviewGraphInput.pr_meta.diff_loc,
      files_changed: reviewGraphInput.pr_meta.files_changed,
    }),
  );

  return { enqueued: true, workflow_id: reviewGraphId };
}
