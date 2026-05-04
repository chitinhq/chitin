// Phase 2 of the swarm-as-software-factory design. The pure-function
// core of the review-tier escalation graph from
// docs/design/2026-05-02-swarm-as-software-factory.md §5.
//
// This module is deliberately *just* the brains:
//   - `computeStartingTier(prMeta, entry)` — reads the §5 trigger
//     matrix and returns the starting reviewer tier + whether the PR
//     is T5-shape (governance-self-modification path) for the
//     gatekeeper to short-circuit on.
//   - `escalateOneTier(currentTier)` — bumps to the next tier,
//     saturating at R4.
//   - `shouldEscalateToOperator(reviewerOutput)` — the rule for
//     stopping the graph and pinging the human.
//
// Temporal workflow + dispatcher integration land in follow-up
// slices. Keeping the brains separable from Temporal means we can
// table-test every row of the §5 matrix without spinning up a
// workflow runtime — and a future change to the matrix is one PR
// against this file with new test rows, not a workflow rewrite.

import type { BacklogEntry } from './grooming/parse-backlog.ts';

// Review-tier ordinal vocabulary. R0 = "Copilot bot review only"
// (the GitHub side fires this automatically on every PR — chitin
// doesn't dispatch it). R1..R3 are dispatched as workflows at
// progressively heavier driver+model combinations. R4 = "stop, ping
// the operator" — terminal.
export type ReviewTier = 'R0' | 'R1' | 'R2' | 'R3' | 'R4';

const TIER_ORDER: readonly ReviewTier[] = ['R0', 'R1', 'R2', 'R3', 'R4'] as const;

function ord(t: ReviewTier): number {
  return TIER_ORDER.indexOf(t);
}

function maxTier(a: ReviewTier, b: ReviewTier): ReviewTier {
  return ord(a) >= ord(b) ? a : b;
}

/**
 * The (driver, model) pair the review-graph workflow dispatches at
 * each tier. R0 is intentionally non-dispatchable here — Copilot's
 * server-side review fires on PR open without us asking. R4 is
 * non-dispatchable too — it's the "ping operator" terminal.
 *
 * R1: copilot/gpt-4.1 — free, fast, mid-confidence
 * R2: copilot/sonnet-4.6 — free under Pro plan, sonnet-class
 * R3: claude-code-headless/opus-4.7 — paid, ~$0.10–0.50/run
 *
 * Format mirrors `TIER_DRIVER` in dispatcher.ts so a future caller
 * can pick the spawn args by reading this map alone.
 */
// envOrDefault treats whitespace-only env values as unset, matching
// the elsewhere-in-this-package convention (envOverride / resolveAgent).
// Without this, an accidentally-blank
// `CHITIN_REVIEWER_R2_DRIVER=` line in chitin.env would override the
// default to an invalid empty string.
//
// CRITICAL: this file is imported by review-graph-workflow.ts which
// runs in Temporal's V8 workflow isolate where `process` is NOT
// defined. Reading process.env at module-load (i.e., inside an IIFE
// like the previous shape) crashes the workflow with
// `ReferenceError: process is not defined`. Read env lazily (inside
// the function the activity layer calls), not at module evaluation.
function envOrDefault(name: string, fallback: string | null): string | null {
  // typeof guard — workflow isolate has no `process` global at all.
  if (typeof process === 'undefined' || !process.env) return fallback;
  const raw = process.env[name];
  if (raw === undefined) return fallback;
  const trimmed = raw.trim();
  return trimmed === '' ? fallback : trimmed;
}

// Default tier→driver mapping (constant; no env reads, safe to
// evaluate at module load in the workflow isolate).
//
// 2026-05-04 reshuffle, driven by Copilot Pro premium-request multipliers:
//   R1: gpt-5-mini    (0× free, was gpt-4.1 also free; gpt-5-mini is newer)
//   R2: haiku-4-5     (0.33× — was sonnet-4.6 at 1×; haiku is the bulk-reviewer
//                       capability tier and 3× cheaper per dispatch)
//   R3: opus-4-7      (Anthropic Max plan, unchanged — escalation only)
//
// Operator can override per-tier with CHITIN_REVIEWER_R<N>_{DRIVER,MODEL}.
const REVIEW_TIER_DRIVER_DEFAULTS: Record<ReviewTier, { driver: string | null; model: string | null }> = {
  R0: { driver: null, model: null },                                          // GH bot, not dispatched
  R1: { driver: 'copilot', model: 'gpt-5-mini' },                             // 0× free
  R2: { driver: 'copilot', model: 'claude-haiku-4-5' },                       // 0.33× — bulk reviewer
  R3: { driver: 'claude-code-headless', model: 'claude-opus-4-7' },           // metered, escalation
  R4: { driver: null, model: null },                                          // operator, not dispatched
};

/**
 * Resolve the driver+model for a review tier, applying the
 * CHITIN_REVIEWER_R<N>_{DRIVER,MODEL} env override if set.
 *
 * Called LAZILY by the activity layer (not the workflow). The
 * workflow isolate has no `process` so it can't run this; the
 * activity wraps it instead. The dispatcher / pr-event-ingester
 * call this directly.
 */
export function resolveReviewTierDriver(tier: ReviewTier): { driver: string | null; model: string | null } {
  const def = REVIEW_TIER_DRIVER_DEFAULTS[tier];
  return {
    driver: envOrDefault(`CHITIN_REVIEWER_${tier}_DRIVER`, def.driver),
    model: envOrDefault(`CHITIN_REVIEWER_${tier}_MODEL`, def.model),
  };
}

/**
 * Eagerly-resolved table preserved for callers that historically
 * read REVIEW_TIER_DRIVER as a constant (test fixtures, log lines).
 * Defaults only — env overrides are NOT applied here because the
 * workflow isolate import would crash. Callers that need the
 * override should call `resolveReviewTierDriver(tier)` directly.
 */
export const REVIEW_TIER_DRIVER: Record<ReviewTier, { driver: string | null; model: string | null }> = REVIEW_TIER_DRIVER_DEFAULTS;

/**
 * Per-tier resource bounds. R1 is fast + cheap (small diffs go through
 * here); R3 gets enough wall time and tool-calls to actually walk a
 * hard PR. R0 + R4 are non-dispatchable so values are 0.
 *
 * Lifted from the closed PR #133's review-graph attempt — the swarm's
 * intuition there was right even though the rest of the implementation
 * skipped the design-doc-first plan. Per-tier bounds keep cost-shaped:
 * R3 (Opus, $0.10–0.50/run) gets generous timeouts because cancelling
 * a partially-done Opus run costs as much as letting it finish.
 */
export const REVIEW_TIER_WALL_TIMEOUT_S: Record<ReviewTier, number> = {
  R0: 0,
  R1: 600,    // 10 min
  R2: 900,    // 15 min
  R3: 1800,   // 30 min — Opus on a hard PR
  R4: 0,
};

export const REVIEW_TIER_MAX_TOOL_CALLS: Record<ReviewTier, number> = {
  R0: 0,
  R1: 20,
  R2: 30,
  R3: 60,
  R4: 0,
};

/**
 * Inputs to `computeStartingTier`. Captures the PR-side state the
 * dispatcher learns at apply-step time, plus the originating entry
 * so file-scope and implementor-tier checks have what they need.
 *
 * Field shapes match what `apps/temporal-worker/src/grooming/
 * apply-workflow-result.ts` already records in the result envelope —
 * the graph executor (next slice) will project an `ApplyResult`
 * into a `PrMeta` shape.
 */
export interface PrMeta {
  /** From `worktree.diff_shortstat` — total insertions + deletions. */
  diff_loc: number;
  /** From `worktree.diff_shortstat` — file count. */
  files_changed: number;
  /** Files in the diff (as repo-relative paths). Used for path-scope
   *  bumps. May be empty if the apply step couldn't enumerate (rare;
   *  treat as "unknown — defer to LOC-based bumps"). */
  files: string[];
  /** Number of inline review comments Copilot's GH bot has left.
   *  Often unknown at the moment the review-graph kicks off (Copilot
   *  is racing the dispatcher). When undefined, we don't bump on
   *  this signal — the graph re-evaluates after R0 settles. */
  copilot_comment_count?: number;
  /** Pull request URL — purely audit context, not used in tier
   *  decisions. Surfaced in tier-decision logs so the chain can be
   *  traversed back to the PR. */
  pr_url?: string;
  /** PR number — required by the workflow loop so it can construct
   *  reviewer prompts (which name the PR# explicitly so the agent
   *  can `gh pr diff <num>` etc.). Optional in the
   *  `computeStartingTier` path (which doesn't need it), required
   *  by the workflow runner. */
  pr_number?: number;
}

/**
 * Result of `computeStartingTier`. Returns the tier *plus* the
 * reasons (so logs + telemetry can show which rule fired) plus a
 * `t5_shape` flag for paths the chitin policy considers
 * governance-self-modification — those always escalate at the
 * gatekeeper layer regardless of the reviewer chain's verdict.
 */
export interface ReviewTierDecision {
  tier: ReviewTier;
  /** True if the PR touches `chitin.yaml` / `.chitin/` /
   *  governance-config paths. The review chain still runs (audit
   *  matters) but the gatekeeper layer must escalate even on
   *  approval. */
  t5_shape: boolean;
  /** Human-readable explanations of which rules bumped the tier.
   *  One entry per fired rule. Empty when starting tier is the
   *  default R0. */
  reasons: string[];
}

// File-scope rules. Order is intentional: T5-shape check runs
// independently (sets the `t5_shape` flag, doesn't change tier).
//
// Kernel-internals + schema files are starting-tier bumps because
// they're load-bearing surfaces — a bug in normalize.go or in the
// ExecutionRequest schema cascades broadly. Worth a heavy reviewer
// even on small diffs.
//
// T5 path coverage mirrors the `no-governance-self-modification`
// regex in `chitin.yaml` (see id: no-governance-self-modification):
//   target_regex: '(?:(?:^|/)chitin\.yaml$|(?:^|/)\.chitin/|(?:^|/)\.hermes/plugins/chitin-governance/)'
// Keep this list in sync with that regex; both are governance-
// self-modification guards and a hole in either is a hole in both.

const T5_FILENAMES: readonly string[] = ['chitin.yaml'];

const T5_PATH_FRAGMENTS: readonly string[] = [
  '/.chitin/',
  '.chitin/',
  '/.hermes/plugins/chitin-governance/',
  '.hermes/plugins/chitin-governance/',
];

const KERNEL_INTERNAL_PREFIXES: readonly string[] = [
  'go/execution-kernel/internal/gov/',
  'go/execution-kernel/internal/canon/',
  'go/execution-kernel/internal/govhookinstall/',
  'go/execution-kernel/internal/hookinstall/',
  'go/execution-kernel/internal/driver/',
  'go/execution-kernel/internal/normalize/',
];

const SCHEMA_PATH_REGEX = /^libs\/contracts\/src\/[\w.-]+\.schema\.ts$/;

function isT5Shape(file: string): boolean {
  if (T5_FILENAMES.includes(file)) return true;
  // Match `chitin.yaml` at any nested path
  if (file === 'chitin.yaml' || file.endsWith('/chitin.yaml')) return true;
  for (const frag of T5_PATH_FRAGMENTS) {
    if (file.includes(frag)) return true;
  }
  return false;
}

function isKernelInternal(file: string): boolean {
  for (const pre of KERNEL_INTERNAL_PREFIXES) {
    if (file.startsWith(pre)) return true;
  }
  return false;
}

function isSchemaFile(file: string): boolean {
  return SCHEMA_PATH_REGEX.test(file);
}

/**
 * Compute the starting reviewer tier from §5's trigger matrix.
 *
 * Pure function. Each rule sets a *minimum* tier; the final tier is
 * the maximum across rules. Defaults to R0 (Copilot bot — fires
 * server-side, no dispatch needed). The reasons array names which
 * rules contributed.
 *
 * Trigger matrix (mirrors the design doc §5 table):
 *
 * | Signal | Bumps to | Threshold |
 * |--------|----------|-----------|
 * | Copilot bot leaves > N comments | R1 | N=2 |
 * | Diff > N LOC OR > M files (mid) | R2 | 200 LOC / 10 files |
 * | Diff > N LOC OR > M files (high) | R3 | 500 LOC / 20 files |
 * | Touches schema files | R2 minimum | always |
 * | Touches kernel internals | R3 minimum | always |
 * | Implementor was tier T3+ | R2 minimum | always |
 *
 * Two §5 rules are intentionally deferred:
 *   - "Touches public API exports (top-level `export`s in `apps/*`)
 *     → R2 minimum" — requires reading the diff content (not just
 *     paths) to detect `+export ...` lines. The path-only signal is
 *     too noisy (every TS module has exports). Encode this when the
 *     workflow has the diff body in hand (step 3).
 *   - "Previous-attempt history" — requires a state-store lookup;
 *     future enhancement that consumes `parent_workflow_id` chains.
 */
export function computeStartingTier(prMeta: PrMeta, entry: BacklogEntry): ReviewTierDecision {
  let tier: ReviewTier = 'R0';
  const reasons: string[] = [];
  let t5_shape = false;

  // Copilot comment count
  if (prMeta.copilot_comment_count !== undefined && prMeta.copilot_comment_count > 2) {
    tier = maxTier(tier, 'R1');
    reasons.push(`Copilot bot left ${prMeta.copilot_comment_count} comments (> 2)`);
  }

  // Diff size — high cutoff first so we attribute correctly
  if (prMeta.diff_loc > 500 || prMeta.files_changed > 20) {
    tier = maxTier(tier, 'R3');
    reasons.push(
      prMeta.diff_loc > 500
        ? `large diff (${prMeta.diff_loc} LOC > 500)`
        : `wide diff (${prMeta.files_changed} files > 20)`,
    );
  } else if (prMeta.diff_loc > 200 || prMeta.files_changed > 10) {
    tier = maxTier(tier, 'R2');
    reasons.push(
      prMeta.diff_loc > 200
        ? `mid-size diff (${prMeta.diff_loc} LOC > 200)`
        : `mid-width diff (${prMeta.files_changed} files > 10)`,
    );
  }

  // File-scope rules
  let kernelHit = false;
  let schemaHit = false;
  for (const f of prMeta.files) {
    if (isT5Shape(f)) t5_shape = true;
    if (isKernelInternal(f)) kernelHit = true;
    if (isSchemaFile(f)) schemaHit = true;
  }
  if (kernelHit) {
    tier = maxTier(tier, 'R3');
    reasons.push('touches kernel internals (gov / canon / hookinstall / driver / normalize)');
  }
  if (schemaHit) {
    tier = maxTier(tier, 'R2');
    reasons.push('touches a libs/contracts schema file');
  }

  // Implementor tier — entry.tier is a string from yaml frontmatter
  // (BacklogEntry.tier is `string | undefined`); coerce + check.
  if (entry.tier === 'T3' || entry.tier === 'T4') {
    tier = maxTier(tier, 'R2');
    reasons.push(`implementor was tier ${entry.tier}`);
  }

  return { tier, t5_shape, reasons };
}

/**
 * Bump to the next reviewer tier. Saturates at R4 (the terminal
 * "ping operator" tier — escalating past R4 doesn't make sense).
 *
 * Used by the review-graph workflow when a reviewer at the current
 * tier requests-changes-with-low-confidence or explicitly returns
 * `decision: 'escalate'`.
 */
export function escalateOneTier(current: ReviewTier): ReviewTier {
  const next: Record<ReviewTier, ReviewTier> = {
    R0: 'R1',
    R1: 'R2',
    R2: 'R3',
    R3: 'R4',
    R4: 'R4',
  };
  return next[current];
}

/**
 * Structured output the reviewer-role agents emit at every tier
 * (modeled here so the workflow + adversarial prompt have a single
 * source of truth). The actual prompt template that produces this
 * shape lands in the `agent-adversarial-review-pass` entry; this
 * module just defines the contract.
 */
export interface ReviewerOutput {
  decision: 'approve' | 'request_changes' | 'escalate';
  confidence: 'high' | 'medium' | 'low';
  findings: ReviewerFinding[];
}

export interface ReviewerFinding {
  /** 🔴 = real bug, blocks merge. 🟡 = worth fixing, doesn't block.
   *  🟢 = doc/nit. */
  severity: '🔴' | '🟡' | '🟢';
  file: string;
  line?: number;
  /** Free-form category. Canonical set is bug/test_gap/design/doc/
   *  infra/security/perf, but agents in practice invent reasonable
   *  ones (`infra`, `ops`, `perf`, etc) and rejecting those at the
   *  schema level cascades the entire review to parse-fail —
   *  losing the substantive findings. Downstream tooling (debt-
   *  ledger, etc) should normalize, not require pre-normalization.
   */
  category: string;
  summary: string;
  suggested_fix?: string;
}

/**
 * Decide whether the review-graph should stop and ping the
 * operator (R4) instead of merging or escalating to the next tier.
 *
 * Called specifically when the chain is at R3 (the heaviest
 * dispatchable reviewer) and needs to decide R3-resolves-here vs.
 * R4-escalate-to-human. Rules (any one fires → escalate):
 *
 *   - reviewer explicitly returned `decision: 'escalate'` — they
 *     self-flagged something they can't decide at this tier.
 *   - reviewer returned `confidence: 'low'` — at R3, low confidence
 *     means "the heaviest reviewer in the graph couldn't fully
 *     evaluate this PR." Per design §5 + the operator's stated rule
 *     ("if Headless Claude Opus can't figure it out, escalate it up
 *     to me"), low confidence at R3 is the operator's signal.
 *
 * Note that `decision: 'request_changes'` with high/medium
 * confidence does NOT trigger operator escalation — that path
 * loops back to the implementor with the reviewer's findings (the
 * implementor either fixes and re-pushes, or the apply step errors
 * and the chain ends naturally). 🔴 findings on their own don't
 * block the operator's time when the reviewer is confident in the
 * fix; the implementor-rerun handles them.
 *
 * NOT covered here (handled at the gatekeeper layer):
 *   - T5-shape paths (escalate regardless of reviewer approval)
 *   - CI failure
 *   - bucket-B telemetry alarm
 *   - diff-vs-file:scope mismatch
 *
 * Those are merge-time gates, not reviewer-tier-graph escalations.
 * They feed the gatekeeper's "auto-merge or escalate" call after
 * the review chain settles.
 */
export function shouldEscalateToOperator(out: ReviewerOutput): boolean {
  if (out.decision === 'escalate') return true;
  if (out.confidence === 'low') return true;
  return false;
}

export const __test__ = {
  ord,
  maxTier,
  isT5Shape,
  isKernelInternal,
  isSchemaFile,
};
