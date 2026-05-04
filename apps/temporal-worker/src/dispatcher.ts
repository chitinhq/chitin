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
import { mkdirSync, readdirSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
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

// Tier → driver routing. Goal: every flat-rate / free account in
// rotation, Anthropic Max plan reserved for actual escalation.
//
// 2026-05-04 reshuffle, driven by measured Copilot Pro premium-request
// multipliers + the realization that each subscription has its own
// independent rate-limit budget. Spreading T1-T3 across them prevents
// any single sub from blowing through its quota.
//
//   T0 → openclaw-glm-flash     3090, glm-4.7-flash (~30B), free unlimited
//   T1 → openclaw-glm-flash     3090, same model — exercises the GPU on
//                                bulk simple-write work; cascades to copilot
//                                free-tier (gpt-5-mini, 0×) on failure
//   T2 → copilot                Copilot Pro, model defaults to claude-haiku-4-5
//                                (0.33× — bulk, ~900/mo budget)
//   T3 → openclaw-glm-cloud     Ollama Cloud sub, glm-5.1:cloud (opus-light);
//                                heavy reasoning that doesn't need Anthropic
//   T4 → claude-code-headless   Anthropic Max, opus-4-7 — escalation only
//
// Operator can override per-tier via CHITIN_TIER_DRIVER_T<N> env at
// runtime without a code change. The autonomous swarm picks up the
// new mapping on next dispatcher tick.
//
// Pre-2026-05-04 mapping had T2 and T3 on claude-code-headless directly,
// which made the swarm's bulk programmer tier dependent on the metered
// Anthropic plan. The reshuffle moves bulk to Copilot's haiku-4-5 (cheap-
// multiplier) and shifts the GPU-resident glm-flash from T0-only to
// T0+T1 so the 3090 actually does work.
//
// Historical bucket-B fix (2026-05-02): T2 routed to copilot to sidestep
// the writeWorktreeClaudeSettings auto-commit issue. apply-workflow-result
// .revertWorktreeSettingsArtifact closed that path; T2 remains on copilot
// for the cost-multiplier reasons above. See docs/observations/
// 2026-05-02-bucket-b-after-action.md.
const TIER_DRIVER_DEFAULTS: Record<Tier, DriverId> = {
  T0: 'openclaw-glm-flash',          // 3090: glm-4.7-flash:latest (~30B)
  T1: 'openclaw-glm-flash',          // 3090: same model — bulk simple write on the GPU
  T2: 'copilot',                     // Copilot Pro: claude-haiku-4-5 (0.33×) — bulk programmer
  T3: 'openclaw-glm-cloud',          // Ollama Cloud sub: glm-5.1:cloud (opus-light) — heavy reasoning
  T4: 'claude-code-headless',        // Anthropic Max: claude-opus-4-7 — escalation only
};

// Operator can override per-tier driver routing via env:
// CHITIN_TIER_DRIVER_T0=copilot pulls T0 back to copilot at runtime
// without a code change. Useful for hotfixes when a local model
// regresses or is offline.
function envDriverOverride(tier: Tier): DriverId | undefined {
  const v = process.env[`CHITIN_TIER_DRIVER_${tier}`];
  if (!v || !v.trim()) return undefined;
  return v.trim() as DriverId;
}

const TIER_DRIVER: Record<Tier, DriverId> = (Object.keys(TIER_DRIVER_DEFAULTS) as Tier[]).reduce(
  (acc, tier) => {
    acc[tier] = envDriverOverride(tier) ?? TIER_DRIVER_DEFAULTS[tier];
    return acc;
  },
  {} as Record<Tier, DriverId>,
);

// Per-entry wall_timeout — short enough that a stuck workflow doesn't
// hold the queue long, generous enough that real work has room. The
// wall_timeout is enforced by activity SIGKILL (slice 7a) — stuck
// processes get killed at exactly this+1s, regardless of how long the
// budget is. So generous budgets cost nothing for healthy runs and
// only cost time-to-fail-detection for stuck ones.
//
// Slice 7-tuning history:
//   180s (slice 7) — too short, even healthy runs hit it
//   480s (first tuning) — productive for copilot but the small 30B local still
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

// Refresh local view of origin's swarm/* refs. Called once per
// pickEntryToDispatch tick so subsequent entryHasOriginBranch checks
// (entry-level + blocks-level) hit cached refs rather than re-fetching
// per call. Auto-prunes deleted remote branches so a merged-then-
// deleted swarm/<id>-* doesn't keep the entry skipped.
function gitFetchOriginRefs(): void {
  try {
    git(['fetch', '--prune', 'origin']);
  } catch (err) {
    log('warn', 'git fetch failed; falling back to cached refs', {
      error: err instanceof Error ? err.message : String(err),
    });
  }
}

// True if origin has any branch matching swarm/swarm-<entry-id>-* — i.e.,
// the entry has been dispatched to completion (branch persists past PR
// merge unless deleted, and stays through PR open). Caller is
// responsible for calling gitFetchOriginRefs() once per tick before the
// loop — this helper deliberately does not fetch, so a single tick
// scanning N entries × K blockers does at most one network round trip.
function entryHasOriginBranch(entryId: string): boolean {
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

// Implementor-side escalation: tier ladder mirrors review-graph's
// R1→R2→R3→operator escalation. A failed dispatch (commits=0) gets
// re-attempted at one tier higher; after exhausting T4, the marker
// stays stuck and the operator picks up.
//
// The "junior dev" intuition: if a junior can't ship the work +
// tests pass cleanly, escalate to senior. Don't throw away the
// effort — but DO escalate before another junior tries the same
// thing again.
const TIER_LADDER: Tier[] = ['T0', 'T1', 'T2', 'T3', 'T4'];

interface MarkerOutcome {
  exit_code: number;
  commits_added: number;
  completed_at: string;
}

interface DispatchMarker {
  entry_id: string;
  workflow_id: string;
  tier: Tier;
  driver: DriverId;
  dispatched_at: string;
  /** Last completed result. Written by `recordDispatchOutcome` after
   *  the workflow returns. Absent until then — caller must treat
   *  marker-without-outcome as "still in flight, do not re-dispatch". */
  last_result?: MarkerOutcome;
  /** Tier ladder visited so far. The junior-dev escalation rule:
   *  re-dispatch at the next-up tier, log every attempt. */
  tier_attempts?: Tier[];
}

function readDispatchMarker(entryId: string): DispatchMarker | null {
  const path = resolve(STATE_DIR, `${entryId}.json`);
  if (!existsSync(path)) return null;
  try {
    return JSON.parse(readFileSync(path, 'utf8')) as DispatchMarker;
  } catch {
    return null;
  }
}

function writeDispatchMarker(
  entryId: string,
  workflowId: string,
  tier: Tier,
  driver: DriverId,
  prior?: DispatchMarker | null,
) {
  mkdirSync(STATE_DIR, { recursive: true });
  const path = resolve(STATE_DIR, `${entryId}.json`);
  const tier_attempts: Tier[] = prior?.tier_attempts ?? (prior ? [prior.tier] : []);
  if (!tier_attempts.includes(tier)) tier_attempts.push(tier);
  const marker: DispatchMarker = {
    entry_id: entryId,
    workflow_id: workflowId,
    tier,
    driver,
    dispatched_at: new Date().toISOString(),
    tier_attempts,
  };
  writeFileSync(path, JSON.stringify(marker, null, 2));
}

/**
 * Update the marker with the workflow's terminal outcome. Called
 * after `handle.result()` returns. Idempotent re-writes are safe.
 */
function recordDispatchOutcome(
  entryId: string,
  result: { exit_code: number; commits_added: number },
) {
  const marker = readDispatchMarker(entryId);
  if (!marker) return; // marker missing — don't synthesize one
  marker.last_result = {
    exit_code: result.exit_code,
    commits_added: result.commits_added,
    completed_at: new Date().toISOString(),
  };
  const path = resolve(STATE_DIR, `${entryId}.json`);
  writeFileSync(path, JSON.stringify(marker, null, 2));
}

/**
 * Implementor-side escalation rule. Reads the marker; decides
 * whether the entry is re-dispatchable at a higher tier.
 *
 * Returns:
 *   { kind: 'no-marker' }              — first dispatch, no escalation
 *   { kind: 'in-flight' }              — workflow still running; skip
 *   { kind: 'shipped' }                — commits made; chain handles it
 *   { kind: 'exhausted' }              — last tier was T4; operator-only
 *   { kind: 'escalate', nextTier: T }  — re-dispatch at nextTier
 *
 * The "shipped" case covers the user's "junior dev" point: if the
 * agent committed work — even if tests then failed — the work is
 * REAL, the reviewer chain catches the rest. Don't re-dispatch on
 * top of an already-pushed branch.
 */
/**
 * Check whether a backlog entry's id appears literally in the
 * recent-merged-PR-subjects scan. Pure string match — backlog ids
 * can contain regex metacharacters (e.g., `pr-event-ingester-1.0`
 * has `.`), so we deliberately skip --grep and do an in-memory
 * `includes` to avoid false matches AND `fatal: invalid regexp`
 * errors that would silently disable the suppression.
 *
 * Exported for unit tests; not part of the public dispatcher API.
 */
export function entryIdInRecentSubjects(entryId: string, recentSubjects: string): boolean {
  if (!entryId || !recentSubjects) return false;
  // Per-line check rather than whole-blob includes — defends
  // against an entry id that happens to appear as a substring of
  // a longer entry id in a different subject (e.g., entry `foo`
  // matching subject "swarm: foo-bar"). We require the id to
  // appear as a discrete word: surrounded by start-of-line, end-
  // of-line, or non-id characters ([^a-zA-Z0-9_-]).
  const lines = recentSubjects.split('\n');
  for (const line of lines) {
    if (!line) continue;
    let idx = 0;
    while ((idx = line.indexOf(entryId, idx)) !== -1) {
      const before = idx === 0 ? '' : line[idx - 1];
      const afterIdx = idx + entryId.length;
      const after = afterIdx >= line.length ? '' : line[afterIdx];
      const beforeOk = before === '' || !/[a-zA-Z0-9_-]/.test(before);
      const afterOk = after === '' || !/[a-zA-Z0-9_-]/.test(after);
      if (beforeOk && afterOk) return true;
      idx += 1;
    }
  }
  return false;
}

export function classifyDispatchEscalation(marker: DispatchMarker | null): {
  kind: 'no-marker' | 'in-flight' | 'shipped' | 'exhausted' | 'escalate';
  nextTier?: Tier;
} {
  if (!marker) return { kind: 'no-marker' };
  if (!marker.last_result) return { kind: 'in-flight' };
  if (marker.last_result.commits_added > 0) return { kind: 'shipped' };
  // commits=0 → the work was lost. Escalate to the next-up tier.
  const visited = new Set(marker.tier_attempts ?? [marker.tier]);
  const lastTier = marker.tier_attempts?.[marker.tier_attempts.length - 1] ?? marker.tier;
  const lastIdx = TIER_LADDER.indexOf(lastTier);
  // Find the next tier we haven't tried; cap at T4.
  for (let i = lastIdx + 1; i < TIER_LADDER.length; i++) {
    const candidate = TIER_LADDER[i];
    if (!visited.has(candidate)) return { kind: 'escalate', nextTier: candidate };
  }
  return { kind: 'exhausted' };
}

interface PickedEntry {
  entry: BacklogEntry;
  /** Tier to dispatch at. Equals entry.tier on first dispatch; one
   *  step UP the ladder when re-dispatching after a commits=0 outcome
   *  (junior-dev escalation rule). */
  effectiveTier: Tier;
  /** Prior marker, if escalating. Caller passes to writeDispatchMarker
   *  so tier_attempts accumulates. */
  priorMarker: DispatchMarker | null;
}

function pickEntryToDispatch(entries: BacklogEntry[]): PickedEntry | null {
  // One fetch per tick — entryHasOriginBranch reads cached refs after
  // this. Without the hoist, an N-entry × K-blocker scan would do N×K
  // network round trips per tick.
  gitFetchOriginRefs();

  // Hoist the recent-PR-subjects scan to once-per-tick. Without this,
  // every ready entry would spawn its own `git log` subprocess (N
  // spawns per tick); on backlogs with dozens of ready entries that's
  // expensive. Read once into memory, then in-memory `includes` per
  // entry.
  //
  // Critical correctness notes (per Copilot review on #265):
  //   - DROP `--merges`. Chitin uses squash-merge as the default; a
  //     squash-merge produces a regular commit on main (no merge
  //     parent), so `--merges` would miss every shipped entry whose
  //     merge style is squash. Plain `git log` catches both.
  //   - Use `origin/main` not local HEAD. `git fetch origin` updates
  //     remote refs but doesn't touch HEAD; scanning HEAD would miss
  //     any merged PRs since the last local pull.
  //   - DROP the per-entry `--grep`. `--grep` treats the id as a
  //     regex; backlog ids can contain regex metacharacters (dots,
  //     parens) which silently mis-match. Read all subjects once,
  //     then in-memory substring match per entry.
  let recentSubjects = '';
  try {
    recentSubjects = git([
      'log',
      'origin/main',
      '--since=14.days',
      '--pretty=%s',
    ]);
  } catch (err) {
    log('warn', 'failed recent-PR-subjects scan; shipped-entry dispatch suppression disabled this tick', {
      error: err instanceof Error ? err.message : String(err),
    });
  }

  for (const entry of entries) {
    if (entry.status !== 'ready') continue;

    // Skip if a recent merged PR title literally contains the entry
    // id (hand-merged/shipped). Defense-in-depth — the
    // chitin-shipped-entry-flipper.timer is the primary mechanism
    // for flipping `ready → partial` after a hand-merge, but it
    // runs on a cadence; this dispatcher-side guard catches the
    // window between hand-merge and the next flipper tick.
    if (entryIdInRecentSubjects(entry.id, recentSubjects)) {
      log('info', 'skip entry: recent merged PR title contains entry id (already shipped)', { entry_id: entry.id });
      continue;
    }

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

    // Implementor-side escalation: read the marker, decide whether
    // a prior failed run is re-dispatchable at a higher tier.
    const marker = readDispatchMarker(entry.id);
    const escalation = classifyDispatchEscalation(marker);
    if (escalation.kind === 'in-flight') {
      log('info', 'skip entry: dispatch marker present, last run still in flight', {
        entry_id: entry.id,
      });
      continue;
    }
    if (escalation.kind === 'shipped') {
      log('info', 'skip entry: prior run committed work — review chain handles the rest', {
        entry_id: entry.id,
        commits: marker?.last_result?.commits_added,
      });
      continue;
    }
    if (escalation.kind === 'exhausted') {
      log('info', 'skip entry: tier ladder exhausted (T4 attempted) — operator-only now', {
        entry_id: entry.id,
        tier_attempts: marker?.tier_attempts,
      });
      continue;
    }

    const unmetBlocker = findUnmetBlocker(entry, entryHasOriginBranch);
    if (unmetBlocker !== undefined) {
      log('info', 'skip entry: blocked-by', {
        entry_id: entry.id,
        blocked_by: unmetBlocker,
      });
      continue;
    }

    let effectiveTier = entry.tier as Tier;
    if (escalation.kind === 'escalate' && escalation.nextTier) {
      effectiveTier = escalation.nextTier;
      log('info', 'escalating entry to next tier (prior run produced no commits)', {
        entry_id: entry.id,
        prior_tier: marker?.tier,
        next_tier: effectiveTier,
        tier_attempts: marker?.tier_attempts,
      });
    }

    return { entry, effectiveTier, priorMarker: marker };
  }
  return null;
}

// Invariant: an entry is blocked iff any id in entry.blocks does not
// yet have a swarm/swarm-<id>-* branch on origin. The branch is the
// load-bearing signal — it exists only after the apply step has
// pushed, so its presence proves the blocker has shipped its work
// (PR open or merged). The dispatch-marker file deliberately is NOT
// consulted here: markers persist past completion (operator must
// rm to retry), so a blocker that shipped would have BOTH a marker
// and a branch — using the marker as a "still in flight" signal
// would falsely block descendants forever.
//
// Returns the first unmet blocker id, or undefined when all blockers
// are met (or there are no blockers). Pure of side effects so tests
// can inject a fake `isShipped` predicate.
export function findUnmetBlocker(
  entry: BacklogEntry,
  isShipped: (entryId: string) => boolean,
): string | undefined {
  if (!Array.isArray(entry.blocks) || entry.blocks.length === 0) return undefined;
  return entry.blocks.find((blockerId) => !isShipped(blockerId));
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
    const picked = pickEntryToDispatch(entries);
    if (!picked) {
      log('info', 'no ready entry to dispatch this tick');
      await notifyTickIdle('no ready entry to dispatch');
      return;
    }
    const { entry, effectiveTier, priorMarker } = picked;
    const tier = effectiveTier;
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
    // present → next tick reads it via the escalation classifier.
    // The new escalation logic supersedes the previous "operator must
    // rm to retry" rule: a failed dispatch (commits=0) auto-escalates
    // one tier up; only T4-exhausted entries stay stuck for operator
    // pickup. priorMarker carries the tier_attempts ladder forward.
    writeDispatchMarker(entry.id, workflowId, tier, driver, priorMarker);

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
      // Record outcome so the next tick's escalation classifier knows
      // the run terminated. exit_code=-1 + commits=0 is the SIGKILL
      // signature; commits=0 alone signals re-dispatchable.
      recordDispatchOutcome(entry.id, { exit_code: -1, commits_added: 0 });
      throw err;
    }
    // Record terminal outcome on the marker so the next dispatcher
    // tick's escalation classifier sees commits=N and decides shipped/
    // re-dispatch correctly.
    recordDispatchOutcome(entry.id, {
      exit_code: result.exit_code,
      commits_added: result.worktree?.commits_added ?? 0,
    });
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

    // Phase 2 step 3b: kick off the review-graph for the freshly-
    // opened PR. Fire-and-forget — the next dispatcher tick is free
    // to pick a new entry while reviewers proceed in parallel. A
    // submit failure here is logged but never propagates: the
    // implementor's work has shipped, and Copilot R0 still reviews
    // server-side regardless.
    if (prUrl) {
      try {
        const { enqueueReviewGraph } = await import('./review-graph-dispatch.ts');
        await enqueueReviewGraph({
          client,
          taskQueue: TASK_QUEUE,
          parent_workflow_id: workflowId,
          pr_url: prUrl,
          worktree: result.worktree,
          entry,
          repo: 'chitinhq/chitin',
          log: (line) => console.log(line),
        });
      } catch (err) {
        log('warn', 'review-graph enqueue failed', {
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }
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
