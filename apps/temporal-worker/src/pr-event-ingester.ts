// PR event ingester. Closes the gap that left the 2026-05-03 cohort
// of human/interactive-opened PRs (#196-#199) un-reviewed by the
// swarm: enqueueReviewGraph was only called from the dispatcher's
// programmer-success path, so PRs from any other source bypassed the
// §5 trigger matrix entirely.
//
// What this module does:
//   1. Polls open PRs on the configured repo (`gh pr list`).
//   2. For each PR NOT authored by the dispatcher (i.e. no `swarm/`
//      branch prefix), checks whether a review-graph workflow with
//      a stable id (`pr-ingest-<pr_number>-review-graph`) is already
//      running. Skip if so.
//   3. Reads PR metadata (diff size, file list, Copilot comment
//      count) and runs `computeStartingTier` from review-graph.ts.
//   4. Synthesizes a BacklogEntry-shaped object and calls
//      enqueueReviewGraph(...). The graph runs as it does today;
//      the PR gets the same R1 → R2 → R3 escalation chain.
//
// Pure logic is exported so the test suite can pin every branch of
// the dedup + trigger logic without standing up Temporal.
//
// Usage:
//   pnpm exec tsx apps/temporal-worker/src/pr-event-ingester.ts [--repo <owner/repo>] [--dry-run]
//
//   --repo:    repo slug (default: read from `gh repo view --json nameWithOwner`)
//   --dry-run: print the PRs that would be ingested, exit 0 without enqueueing.

import { Connection, Client } from '@temporalio/client';
import { execFileSync } from 'node:child_process';
import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';
import type { BacklogEntry } from './grooming/parse-backlog.ts';
import { computeStartingTier, type PrMeta } from './review-graph.ts';
import {
  enqueueReviewGraph,
  REVIEW_GRAPH_WORKFLOW_NAME,
} from './review-graph-dispatch.ts';
import {
  enqueuePeerReviewer,
  peerReviewerWorkflowIdForPr,
} from './peer-reviewer/dispatch.ts';
import {
  enqueueCommentResponder,
  commentResponderWorkflowIdForPr,
} from './comment-responder/dispatch.ts';

// Threshold above which the ingester dispatches a comment-responder.
// Mirrors the §5 review-graph trigger ("Copilot bot leaves > 2 inline
// comments → escalate to R1") so the comment-responder fires on
// roughly the same PRs that get an R1 reviewer.
const COMMENT_RESPONDER_THRESHOLD = 2;

const EXECUTE_REQUEST_WORKFLOW_NAME = 'executeRequestWorkflow';

const TASK_QUEUE = 'chitin-worker-q';

// ── Per-PR dispatch markers ─────────────────────────────────────────
//
// listRunningAgentWorkflows only returns Running workflows. Once a
// peer-reviewer run for PR #N completes, the next ingester tick (5 min
// later) sees no running workflow with id `peer-review-pr-N` and
// re-dispatches against the same unchanged PR — leaving duplicate
// review comments on every tick.
//
// Fix: dedup against PR state, not workflow state. Each peer-reviewer
// run is identified by (pr_number, head_sha). On dispatch the ingester
// writes a marker file recording the head_sha it dispatched against.
// On the next tick, if the marker's head_sha matches the PR's current
// HEAD, skip — there's nothing new to review. A new commit pushed to
// the PR rotates head_sha and the marker no longer matches, so the
// next tick dispatches.
//
// Comment-responder uses a composite key (head_sha, comment_count): a
// new commit OR new comments since last dispatch warrants another run.
// Strictly this should be the unresolved-comment-set hash (see backlog
// entry pr-event-ingester-comment-count-is-unresolved); comment_count
// is a coarser proxy that still solves the every-5-minutes regression.
//
// Markers live under ~/.cache/chitin/, mirroring the dispatcher's
// existing dispatched/<entry-id>.json convention.
const PEER_REVIEWER_STATE_DIR = path.join(
  os.homedir(),
  '.cache/chitin/peer-reviewer-state',
);
const COMMENT_RESPONDER_STATE_DIR = path.join(
  os.homedir(),
  '.cache/chitin/comment-responder-state',
);

export interface PeerReviewerMarker {
  head_sha: string;
  dispatched_at: string;
  workflow_id: string;
}

export interface CommentResponderMarker {
  head_sha: string;
  comment_count: number;
  dispatched_at: string;
  workflow_id: string;
}

export function readPeerReviewerMarker(
  prNumber: number,
  baseDir: string = PEER_REVIEWER_STATE_DIR,
): PeerReviewerMarker | undefined {
  return readMarker<PeerReviewerMarker>(baseDir, prNumber);
}

export function writePeerReviewerMarker(
  prNumber: number,
  marker: PeerReviewerMarker,
  baseDir: string = PEER_REVIEWER_STATE_DIR,
): void {
  writeMarker(baseDir, prNumber, marker);
}

export function readCommentResponderMarker(
  prNumber: number,
  baseDir: string = COMMENT_RESPONDER_STATE_DIR,
): CommentResponderMarker | undefined {
  return readMarker<CommentResponderMarker>(baseDir, prNumber);
}

export function writeCommentResponderMarker(
  prNumber: number,
  marker: CommentResponderMarker,
  baseDir: string = COMMENT_RESPONDER_STATE_DIR,
): void {
  writeMarker(baseDir, prNumber, marker);
}

function readMarker<T>(baseDir: string, prNumber: number): T | undefined {
  const p = path.join(baseDir, `pr-${prNumber}.json`);
  try {
    const raw = fs.readFileSync(p, 'utf8');
    return JSON.parse(raw) as T;
  } catch {
    // Missing file or malformed JSON → treat as no marker. Fail-open
    // means we MIGHT re-dispatch once; the running-workflow check is
    // the second layer of dedup so the worst case is bounded.
    return undefined;
  }
}

function writeMarker(baseDir: string, prNumber: number, marker: unknown): void {
  fs.mkdirSync(baseDir, { recursive: true });
  fs.writeFileSync(
    path.join(baseDir, `pr-${prNumber}.json`),
    JSON.stringify(marker),
  );
}

// ── Pure logic ──────────────────────────────────────────────────────

/**
 * Pure: decide which agents to dispatch for a PR, given running agents,
 * persisted dispatch markers, and options. Mirrors the per-PR agent
 * dispatch logic in runIngesterTick.
 *
 * Two agents may be dispatched per PR:
 *
 *   - peer-reviewer: a higher-tier reviewer that re-evaluates the PR
 *     beyond what GitHub's Copilot bot already produced. Skipped when:
 *       1. its workflow id is already in runningAgents (Temporal-side
 *          dedup against an in-flight run), OR
 *       2. peerReviewerMarker.head_sha === pr.headRefOid (state-side
 *          dedup against an already-completed run for this commit).
 *     A new commit on the PR rotates headRefOid; the marker no longer
 *     matches and the next tick dispatches.
 *
 *   - comment-responder: redrafts patches in response to inline
 *     review comments. Dispatched only when copilotCommentCount
 *     EXCEEDS the threshold (`>` not `>=`). Skipped when:
 *       1. its workflow id is already in runningAgents, OR
 *       2. commentResponderMarker matches both head_sha AND
 *          comment_count is not strictly greater — i.e. nothing has
 *          changed since the last dispatch.
 *     The composite (head_sha, comment_count) key approximates the
 *     ideal (PR#, unresolved-comment-set hash) until backlog entry
 *     pr-event-ingester-comment-count-is-unresolved lands.
 *
 * Decisions are returned as bools + a reasons map so callers can
 * distinguish "skipped because of dedup" from "skipped because below
 * threshold" in telemetry.
 */
export function decideAgentDispatches(
  pr: OpenPrSummary,
  runningAgents: ReadonlySet<string>,
  opts?: {
    commentResponderThreshold?: number;
    peerReviewerMarker?: PeerReviewerMarker;
    commentResponderMarker?: CommentResponderMarker;
  }
): {
  dispatchPeerReviewer: boolean;
  dispatchCommentResponder: boolean;
  reasons: {
    skip_peer_reviewer?: string;
    skip_comment_responder?: string;
  };
} {
  const peerWorkflowId = peerReviewerWorkflowIdForPr(pr.number);
  const responderWorkflowId = commentResponderWorkflowIdForPr(pr.number);
  const commentCount = pr.copilotCommentCount ?? 0;
  const threshold = opts?.commentResponderThreshold ?? COMMENT_RESPONDER_THRESHOLD;
  const reasons: { skip_peer_reviewer?: string; skip_comment_responder?: string } = {};

  let dispatchPeerReviewer = true;
  if (runningAgents.has(peerWorkflowId)) {
    dispatchPeerReviewer = false;
    reasons.skip_peer_reviewer = 'peer-reviewer already running';
  } else if (
    opts?.peerReviewerMarker &&
    pr.headRefOid &&
    opts.peerReviewerMarker.head_sha === pr.headRefOid
  ) {
    dispatchPeerReviewer = false;
    reasons.skip_peer_reviewer = `peer-reviewer already ran for head_sha ${pr.headRefOid.slice(0, 7)}`;
  }

  let dispatchCommentResponder = false;
  if (runningAgents.has(responderWorkflowId)) {
    dispatchCommentResponder = false;
    reasons.skip_comment_responder = 'comment-responder already running';
  } else if (commentCount > threshold) {
    const marker = opts?.commentResponderMarker;
    const headMatches =
      !!marker && !!pr.headRefOid && marker.head_sha === pr.headRefOid;
    const noNewComments = !!marker && commentCount <= marker.comment_count;
    if (headMatches && noNewComments) {
      dispatchCommentResponder = false;
      reasons.skip_comment_responder = `comment-responder already ran for (head_sha=${pr.headRefOid!.slice(0, 7)}, comment_count<=${marker!.comment_count})`;
    } else {
      dispatchCommentResponder = true;
    }
  } else {
    dispatchCommentResponder = false;
    reasons.skip_comment_responder = `comment count (${commentCount}) <= threshold (${threshold})`;
  }

  return {
    dispatchPeerReviewer,
    dispatchCommentResponder,
    reasons,
  };
}


/**
 * Stable parent_workflow_id for an ingester-spawned review-graph.
 * The review-graph workflow id derives from this as
 * `${parent_workflow_id}-review-graph` (matches review-graph-dispatch.ts).
 */
export function parentWorkflowIdForPr(prNumber: number): string {
  return `pr-ingest-${prNumber}`;
}

export function reviewGraphWorkflowIdForPr(prNumber: number): string {
  return `${parentWorkflowIdForPr(prNumber)}-review-graph`;
}

export interface OpenPrSummary {
  number: number;
  title: string;
  headRefName: string;
  /** Current HEAD commit sha of the PR's head branch. Idempotency key
   *  for per-PR agent dispatch (peer-reviewer, comment-responder): a
   *  new commit pushed to the PR rotates this and earns a fresh run.
   *  Optional because callers in tests synthesize summaries without
   *  faking a sha; production listOpenPrs always populates it. */
  headRefOid?: string;
  additions: number;
  deletions: number;
  changedFiles: number;
  isDraft: boolean;
  /** Repo-relative paths in the diff. Empty when caller hasn't fetched
   *  them (the path-scope bumps in computeStartingTier are then skipped). */
  files?: string[];
  /** Inline review comment count on the PR. Used by §5 trigger matrix. */
  copilotCommentCount?: number;
  /** PR url (for the synthesized BacklogEntry's pr_meta). */
  url: string;
}

/**
 * Decision the ingester makes per PR:
 *   - skip         — branch is dispatcher-owned, draft, already running, etc.
 *   - ingest       — qualifies; spawn a review-graph
 *   - ingest_R0    — §5 says "Copilot R0 is enough"; chitin doesn't dispatch
 *                    (kept distinct from skip so the chain event records
 *                    "we evaluated it and chose not to escalate")
 */
export type IngestDecision =
  | { kind: 'skip'; pr: OpenPrSummary; reason: string }
  | { kind: 'ingest_r0'; pr: OpenPrSummary; reasons: string[] }
  | {
      kind: 'ingest';
      pr: OpenPrSummary;
      pr_meta: PrMeta;
      starting_tier: 'R1' | 'R2' | 'R3';
      reasons: string[];
      t5_shape: boolean;
    };

/**
 * Pure: given (open PRs, running review-graph workflow ids), return a
 * decision per PR.
 *
 * Invariant (Knuth-style): every input PR appears in the output exactly
 * once. The decisions partition the input — no PR is silently dropped.
 */
export function pickPrsToIngest(
  prs: readonly OpenPrSummary[],
  runningReviewGraphIds: ReadonlySet<string>,
): IngestDecision[] {
  return prs.map((pr) => decideForPr(pr, runningReviewGraphIds));
}

function decideForPr(
  pr: OpenPrSummary,
  runningReviewGraphIds: ReadonlySet<string>,
): IngestDecision {
  // Skip 1: drafts. Reviewers shouldn't waste tokens on WIP work the
  // author hasn't asked for review on. The author marks the PR ready
  // when they want eyes on it.
  if (pr.isDraft) {
    return { kind: 'skip', pr, reason: 'draft PR' };
  }

  // Skip 2: dispatcher-owned PRs. The programmer-success path in
  // dispatcher.ts already calls enqueueReviewGraph for these.
  // Re-ingesting would create a duplicate review-graph workflow.
  if (pr.headRefName.startsWith('swarm/')) {
    return { kind: 'skip', pr, reason: 'dispatcher-owned (swarm/ branch)' };
  }

  // Skip 3: review-graph already running for this PR. Stable id-based
  // dedup means re-running this tick a minute from now is idempotent.
  const expectedWorkflowId = reviewGraphWorkflowIdForPr(pr.number);
  if (runningReviewGraphIds.has(expectedWorkflowId)) {
    return { kind: 'skip', pr, reason: 'review-graph already running' };
  }

  // Build the PrMeta shape that computeStartingTier expects.
  const pr_meta: PrMeta = {
    diff_loc: pr.additions + pr.deletions,
    files_changed: pr.changedFiles,
    files: pr.files ?? [],
    pr_url: pr.url,
    pr_number: pr.number,
    copilot_comment_count: pr.copilotCommentCount,
  };

  const decision = computeStartingTier(pr_meta, synthesizeBacklogEntry(pr));

  if (decision.tier === 'R0') {
    // §5 says R0 (Copilot) covers it. Don't dispatch chitin reviewers
    // — but record the decision so audit can confirm we evaluated it.
    return { kind: 'ingest_r0', pr, reasons: decision.reasons };
  }

  if (decision.tier === 'R4') {
    // R4 is "ping operator" — non-dispatchable. Treat as skip with
    // a clear reason so the chain event captures it.
    return { kind: 'skip', pr, reason: 'R4 (operator pickup) — not chitin-dispatchable' };
  }

  return {
    kind: 'ingest',
    pr,
    pr_meta,
    starting_tier: decision.tier,
    reasons: decision.reasons,
    t5_shape: decision.t5_shape,
  };
}

/**
 * Pure: turn a PR summary into a BacklogEntry-shaped object the
 * existing enqueueReviewGraph API can consume. Synthesized entries
 * carry an `id` of `pr-ingest-<pr_number>` so chain events tie back
 * to the parent_workflow_id. Fields match BacklogEntry's actual shape
 * (camelCase, with rawFrontmatter / rawSection placeholders since
 * this entry was synthesized, not parsed from swarm-backlog.md).
 */
export function synthesizeBacklogEntry(pr: OpenPrSummary): BacklogEntry {
  return {
    id: `pr-ingest-${pr.number}`,
    tier: 'T2',                          // reviewer driver tier; review tier is computed separately
    status: 'ready',
    estimatedLoc: String(pr.additions + pr.deletions),
    blocks: [],
    file: pr.files?.join(', ') ?? '',
    role: 'reviewer',
    rawFrontmatter: '',                  // synthesized; no source yaml
    description: `Synthesized from PR #${pr.number}: ${pr.title}`,
    rawSection: '',                      // synthesized; no source section
  };
}

// ── I/O wrappers ────────────────────────────────────────────────────

/**
 * Read open PRs via `gh pr list`. Includes only the fields needed for
 * the §5 trigger matrix; further fields (diff file list, comment
 * count) are fetched per-PR by `enrichPr`.
 */
export function listOpenPrs(repo: string): OpenPrSummary[] {
  const out = execFileSync(
    'gh',
    [
      'pr',
      'list',
      '--repo',
      repo,
      '--state',
      'open',
      // Cap high enough that no realistic chitin-shaped repo blows
      // past it. gh's hard cap on `pr list --limit` is 1000; setting
      // exactly that maxes the page size. If the repo ever grows
      // beyond 1000 open PRs, real pagination via `--paginate`
      // becomes necessary — until then this is fine.
      '--limit',
      '1000',
      '--json',
      'number,title,headRefName,headRefOid,additions,deletions,changedFiles,isDraft,url',
    ],
    { encoding: 'utf8' },
  );
  return JSON.parse(out) as OpenPrSummary[];
}

/**
 * Enrich a PR summary with the file list + Copilot comment count.
 * These require additional gh calls so we only fetch them for PRs
 * that look like they might cross a §5 threshold (always for the
 * non-trivial ones).
 */
export function enrichPr(repo: string, pr: OpenPrSummary): OpenPrSummary {
  let files: string[] = [];
  try {
    const filesOut = execFileSync('gh', ['pr', 'diff', String(pr.number), '--repo', repo, '--name-only'], {
      encoding: 'utf8',
    });
    files = filesOut.split('\n').map((l) => l.trim()).filter(Boolean);
  } catch {
    // PR has no diff yet (rare) — leave empty; computeStartingTier
    // tolerates this.
  }
  let copilotCommentCount: number | undefined;
  try {
    // /pulls/{n}/comments returns ALL inline review comments — humans,
    // bots, and Copilot. The §5 trigger matrix only escalates on
    // Copilot's R0 review (see review-graph.ts:computeStartingTier
    // "copilot_comment_count" branch), so we filter by author.login.
    // GitHub's Copilot review bot logs in as `copilot-pull-request-reviewer[bot]`;
    // be tolerant of the historical `Copilot` capitalization too.
    const commentsOut = execFileSync(
      'gh',
      [
        'api',
        `repos/${repo}/pulls/${pr.number}/comments`,
        '--jq',
        '[.[] | select(.user.login == "copilot-pull-request-reviewer[bot]" or .user.login == "Copilot")] | length',
      ],
      { encoding: 'utf8' },
    );
    copilotCommentCount = parseInt(commentsOut.trim(), 10);
    if (Number.isNaN(copilotCommentCount)) copilotCommentCount = undefined;
  } catch {
    // Permission / API issue — skip this signal.
  }
  return { ...pr, files, copilotCommentCount };
}

/**
 * Query Temporal for currently-running review-graph workflow ids.
 * The dedup check uses this to avoid spawning a duplicate.
 */
export async function listRunningReviewGraphWorkflows(client: Client): Promise<Set<string>> {
  const ids = new Set<string>();
  const iter = client.workflow.list({
    query: `WorkflowType="${REVIEW_GRAPH_WORKFLOW_NAME}" AND ExecutionStatus="Running"`,
  });
  for await (const wf of iter) {
    ids.add(wf.workflowId);
  }
  return ids;
}

/**
 * Query Temporal for currently-running peer-reviewer + comment-
 * responder workflow ids. Both share the executeRequestWorkflow type;
 * we filter by workflow_id prefix in-process (NOT in the visibility
 * query) because `STARTS_WITH` + boolean OR in visibility queries
 * isn't uniformly supported across Temporal visibility backends —
 * the default SQLite backend in particular rejects it. (Copilot
 * review #211 #3.)
 *
 * Failure mode: if the visibility query throws (backend down, schema
 * drift, etc.), this returns an empty set and logs a warn line
 * rather than letting the entire ingester tick fail. Treating "no
 * running agents" on query failure means the ingester might
 * dispatch a duplicate peer-reviewer for one tick — which Temporal
 * itself rejects via stable workflow id, since peer-review-pr-<n>
 * is a unique-by-PR id. Fail-open here is bounded.
 */
export async function listRunningAgentWorkflows(
  client: Client,
  log?: (line: string) => void,
): Promise<Set<string>> {
  const ids = new Set<string>();
  try {
    const iter = client.workflow.list({
      query: `WorkflowType="${EXECUTE_REQUEST_WORKFLOW_NAME}" AND ExecutionStatus="Running"`,
    });
    for await (const wf of iter) {
      const id = wf.workflowId;
      // Filter to the agent ids in-process (peer-review-pr-* and
      // comment-respond-pr-*) so the visibility query stays
      // backend-portable.
      if (id.startsWith('peer-review-pr-') || id.startsWith('comment-respond-pr-')) {
        ids.add(id);
      }
    }
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    const line = JSON.stringify({
      ts: new Date().toISOString(),
      level: 'warn',
      component: 'pr-event-ingester',
      msg: 'listRunningAgentWorkflows failed; treating as empty',
      error: msg,
    });
    if (log) log(line);
    else console.warn(line);
    return new Set();
  }
  return ids;
}

// ── main entrypoint ────────────────────────────────────────────────

export interface IngesterTickResult {
  evaluated: number;
  /** review-graph workflows enqueued */
  ingested: number;
  /** §5 said R0 (Copilot covers it) — recorded but not dispatched */
  ingested_r0: number;
  /** peer-reviewer workflows enqueued (per-PR, parallel to review-graph) */
  peer_reviewers_enqueued: number;
  /** comment-responder workflows enqueued */
  comment_responders_enqueued: number;
  skipped: number;
  errors: number;
}

export async function runIngesterTick(opts: {
  client: Client;
  taskQueue: string;
  repo: string;
  dryRun?: boolean;
  log?: (line: string) => void;
}): Promise<IngesterTickResult> {
  // Default logger to console.log to match the convention of other
  // timer-style runners in this repo (alarm-feeder.ts:164,
  // groomer.ts:174, lessons.ts:382, stale-doc-detector.ts:285).
  // Sending normal-tick output to stderr would have systemd treat
  // every successful run as an error and create monitor noise.
  const log = opts.log ?? ((line) => console.log(line));
  const result: IngesterTickResult = {
    evaluated: 0,
    ingested: 0,
    ingested_r0: 0,
    peer_reviewers_enqueued: 0,
    comment_responders_enqueued: 0,
    skipped: 0,
    errors: 0,
  };

  const summaries = listOpenPrs(opts.repo);
  result.evaluated = summaries.length;

  if (summaries.length === 0) {
    log(JSON.stringify({ component: 'pr-event-ingester', msg: 'no open PRs' }));
    return result;
  }

  // Enrich only the PRs we'd potentially ingest (skip drafts and
  // dispatcher-owned branches). Saves gh calls.
  const enriched = summaries.map((s) => {
    if (s.isDraft || s.headRefName.startsWith('swarm/')) return s;
    return enrichPr(opts.repo, s);
  });

  const running = await listRunningReviewGraphWorkflows(opts.client);
  const runningAgents = await listRunningAgentWorkflows(opts.client, log);
  const decisions = pickPrsToIngest(enriched, running);

  for (const decision of decisions) {
    switch (decision.kind) {
      case 'skip':
        result.skipped += 1;
        log(JSON.stringify({
          component: 'pr-event-ingester',
          msg: 'skip',
          pr: decision.pr.number,
          reason: decision.reason,
        }));
        break;
      case 'ingest_r0':
        result.ingested_r0 += 1;
        log(JSON.stringify({
          component: 'pr-event-ingester',
          msg: 'ingest_r0',
          pr: decision.pr.number,
          reasons: decision.reasons,
        }));
        break;
      case 'ingest':
        if (opts.dryRun) {
          log(JSON.stringify({
            component: 'pr-event-ingester',
            msg: 'would-ingest',
            pr: decision.pr.number,
            tier: decision.starting_tier,
            reasons: decision.reasons,
          }));
          result.ingested += 1;
          break;
        }
        try {
          const enqResult = await enqueueReviewGraph({
            client: opts.client,
            taskQueue: opts.taskQueue,
            parent_workflow_id: parentWorkflowIdForPr(decision.pr.number),
            pr_url: decision.pr.url,
            worktree: undefined,                  // ingester has no worktree
            // Pass through the PrMeta we already classified — without
            // this, enqueueReviewGraph rebuilds from the (empty)
            // worktree and the §5 trigger matrix re-runs against
            // diff_loc=0 / files_changed=0 / no Copilot count, so
            // every ingested PR collapses back to R0→R1. Threading
            // pr_meta keeps the R2/R3/T5 escalations the ingester
            // computed in pickPrsToIngest().
            pr_meta: decision.pr_meta,
            entry: synthesizeBacklogEntry(decision.pr),
            repo: opts.repo,
            log,
          });
          if (enqResult.enqueued) {
            result.ingested += 1;
            log(JSON.stringify({
              component: 'pr-event-ingester',
              msg: 'ingested',
              pr: decision.pr.number,
              workflow_id: enqResult.workflow_id,
              tier: decision.starting_tier,
            }));
          } else {
            result.errors += 1;
            log(JSON.stringify({
              component: 'pr-event-ingester',
              msg: 'enqueue-failed',
              pr: decision.pr.number,
              error: enqResult.error,
            }));
          }
        } catch (err) {
          result.errors += 1;
          log(JSON.stringify({
            component: 'pr-event-ingester',
            msg: 'enqueue-threw',
            pr: decision.pr.number,
            error: err instanceof Error ? err.message : String(err),
          }));
        }
        break;
    }
  }

  // Per-PR agent dispatches — peer-reviewer + comment-responder.
  // Iterate the same enriched set so the dispatch decision sees the
  // up-to-date Copilot comment count and PR shape. Non-skip decisions
  // already qualify (non-draft, non-swarm/, non-already-running review-
  // graph); we add per-agent dedup against runningAgents AND against
  // persisted markers so completed runs don't re-fire on every tick.
  for (const decision of decisions) {
    if (decision.kind === 'skip') continue;
    const pr = decision.pr;
    const peerMarker = readPeerReviewerMarker(pr.number);
    const responderMarker = readCommentResponderMarker(pr.number);
    const agentDecision = decideAgentDispatches(pr, runningAgents, {
      peerReviewerMarker: peerMarker,
      commentResponderMarker: responderMarker,
    });

    if (agentDecision.reasons.skip_peer_reviewer) {
      log(JSON.stringify({
        component: 'pr-event-ingester',
        msg: 'skip-peer-reviewer',
        pr: pr.number,
        reason: agentDecision.reasons.skip_peer_reviewer,
      }));
    }
    if (agentDecision.reasons.skip_comment_responder) {
      log(JSON.stringify({
        component: 'pr-event-ingester',
        msg: 'skip-comment-responder',
        pr: pr.number,
        reason: agentDecision.reasons.skip_comment_responder,
      }));
    }

    // Peer-reviewer
    if (agentDecision.dispatchPeerReviewer) {
      if (opts.dryRun) {
        log(JSON.stringify({
          component: 'pr-event-ingester',
          msg: 'would-enqueue-peer-reviewer',
          pr: pr.number,
        }));
        result.peer_reviewers_enqueued += 1;
      } else {
        const peerRes = await enqueuePeerReviewer({
          client: opts.client,
          taskQueue: opts.taskQueue,
          pr_url: pr.url,
          repo: opts.repo,
          log,
        });
        if (peerRes.enqueued) {
          result.peer_reviewers_enqueued += 1;
          // Record the dispatch so subsequent ticks skip until either
          // a new commit (head_sha rotates) or operator-initiated
          // marker removal. Skipped if headRefOid isn't available —
          // we'd rather re-dispatch than write a marker keyed on an
          // empty sha.
          if (pr.headRefOid) {
            writePeerReviewerMarker(pr.number, {
              head_sha: pr.headRefOid,
              dispatched_at: new Date().toISOString(),
              workflow_id: peerReviewerWorkflowIdForPr(pr.number),
            });
          }
        } else {
          result.errors += 1;
        }
      }
    }

    // Comment-responder
    if (agentDecision.dispatchCommentResponder) {
      if (opts.dryRun) {
        log(JSON.stringify({
          component: 'pr-event-ingester',
          msg: 'would-enqueue-comment-responder',
          pr: pr.number,
          comment_count: pr.copilotCommentCount ?? 0,
        }));
        result.comment_responders_enqueued += 1;
      } else {
        const respRes = await enqueueCommentResponder({
          client: opts.client,
          taskQueue: opts.taskQueue,
          pr_url: pr.url,
          repo: opts.repo,
          log,
        });
        if (respRes.enqueued) {
          result.comment_responders_enqueued += 1;
          if (pr.headRefOid) {
            writeCommentResponderMarker(pr.number, {
              head_sha: pr.headRefOid,
              comment_count: pr.copilotCommentCount ?? 0,
              dispatched_at: new Date().toISOString(),
              workflow_id: commentResponderWorkflowIdForPr(pr.number),
            });
          }
        } else {
          result.errors += 1;
        }
      }
    }
  }

  log(JSON.stringify({
    component: 'pr-event-ingester',
    msg: 'tick-complete',
    ...result,
  }));

  return result;
}

async function main() {
  const args = process.argv.slice(2);
  const dryRun = args.includes('--dry-run');
  const repoIdx = args.indexOf('--repo');
  const repoFromFlag = repoIdx >= 0 ? args[repoIdx + 1] : undefined;
  const repo = repoFromFlag ?? defaultRepo();

  const connection = await Connection.connect({ address: '127.0.0.1:7233' });
  try {
    const client = new Client({ connection, namespace: 'default' });
    const result = await runIngesterTick({ client, taskQueue: TASK_QUEUE, repo, dryRun });
    if (result.errors > 0) {
      process.exit(1);
    }
  } finally {
    await connection.close();
  }
}

function defaultRepo(): string {
  const out = execFileSync('gh', ['repo', 'view', '--json', 'nameWithOwner', '--jq', '.nameWithOwner'], {
    encoding: 'utf8',
  });
  return out.trim();
}

const isCli = fileURLToPath(import.meta.url) === process.argv[1];
if (isCli) {
  main().catch((err: unknown) => {
    console.error(JSON.stringify({
      component: 'pr-event-ingester',
      msg: 'fatal',
      error: err instanceof Error ? err.message : String(err),
    }));
    process.exit(1);
  });
}
