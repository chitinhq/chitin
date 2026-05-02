// Phase 2 step 3 of the swarm-as-software-factory design. The Temporal
// workflow that executes the §5 review-tier escalation chain end-to-end:
//
//   compute starting tier
//   loop:
//     dispatch reviewer at current tier (executeChild)
//     parse the structured output
//     decide: approve / request-changes / escalate-tier / escalate-operator
//     if approve / request-changes / escalate-operator → return
//     if escalate-tier → bump tier, carry findings forward, recurse
//   terminate at R4 (operator pickup) if no other branch fired
//
// The loop logic is a pure function (`runReviewGraphLoop`) that takes
// a `reviewerDispatch` callback. The Temporal wrapper
// (`reviewGraphWorkflow`) hands it `executeChild(executeRequestWorkflow, ...)`.
// Tests pass a mock callback so we don't need a Temporal runtime in
// the unit suite.
//
// Dispatcher integration (auto-enqueue this workflow after a programmer
// workflow opens a PR) lands in a follow-up slice. This PR ships the
// workflow itself so it's reviewable in isolation; the dispatcher edit
// is the next step.

import { executeChild } from '@temporalio/workflow';
import {
  ExecutionRequestSchema,
  type ExecutionRequest,
  type DriverId,
  type Tier,
} from '@chitin/contracts';
import type { ActivityResult } from './activity-types.ts';
import type { executeRequestWorkflow } from './workflow.ts';
import {
  REVIEW_TIER_DRIVER,
  REVIEW_TIER_WALL_TIMEOUT_S,
  REVIEW_TIER_MAX_TOOL_CALLS,
  computeStartingTier,
  escalateOneTier,
  shouldEscalateToOperator,
  type PrMeta,
  type ReviewTier,
  type ReviewerOutput,
  type ReviewerFinding,
} from './review-graph.ts';
import {
  buildAdversarialReviewerPrompt,
  parseReviewerOutput,
} from './reviewer-prompts.ts';
import type { BacklogEntry } from './grooming/parse-backlog.ts';

// ─── Public types ──────────────────────────────────────────────────────────

/**
 * Input the dispatcher hands the review-graph workflow when a
 * programmer workflow has opened a PR. The workflow uses this to
 * compute the starting tier + construct each reviewer's prompt.
 */
export interface ReviewGraphInput {
  parent_workflow_id: string;
  pr_meta: PrMeta;
  /** The originating backlog entry — used for tier rules + scope checks. */
  entry: BacklogEntry;
  /** Inline review comments from Copilot's GH bot, if R0 has settled by
   *  the time the dispatcher invokes us. May be undefined; we proceed
   *  with `copilot_comments=undefined` in the prompt and don't bump
   *  on the comment-count signal in computeStartingTier. */
  copilot_comments?: string;
  /** Repo slug — needed when we construct reviewer ExecutionRequests. */
  repo: string;
}

/**
 * Final action the review-graph terminates with. The gatekeeper
 * (separate slice) consumes this to decide auto-merge vs. escalate
 * vs. wait-for-implementor.
 */
export type ReviewGraphAction =
  | 'approve'                    // all reviewers said approve; gatekeeper checks other gates + merges
  | 'request-changes'            // a reviewer wants implementor changes (with high/medium confidence)
  | 'escalate-to-operator'       // R3 returned low confidence OR explicit escalate; human pickup
  | 'parse-failure-at-r4';       // every tier's output failed to parse; chain ran out of tiers

export interface ReviewGraphResult {
  final_tier: ReviewTier;
  action: ReviewGraphAction;
  /** The last successful parse, when one happened. Undefined when
   *  every tier produced unparseable output. */
  output?: ReviewerOutput;
  /** Whether `t5_shape` was detected on input. The gatekeeper escalates
   *  on this regardless of `action`. */
  t5_shape: boolean;
  /** Tier-by-tier audit trail. Each entry: tier the chain visited,
   *  whether the reviewer's emit parsed, the parsed output if so. */
  tier_log: Array<{
    tier: ReviewTier;
    parsed: boolean;
    output?: ReviewerOutput;
    error?: string;
  }>;
}

/** The shape the loop calls when it needs to dispatch a reviewer. The
 *  Temporal wrapper passes a function that calls `executeChild`; tests
 *  pass a mock that returns canned `ActivityResult`s. */
export type ReviewerDispatch = (req: ExecutionRequest) => Promise<ActivityResult>;

// ─── Pure loop ─────────────────────────────────────────────────────────────

/**
 * Run the review-tier escalation loop. Pure modulo `reviewerDispatch`,
 * which the caller injects. Does NOT contain any Temporal API calls —
 * the Temporal wrapper around this is `reviewGraphWorkflow` below.
 */
export async function runReviewGraphLoop(
  input: ReviewGraphInput,
  reviewerDispatch: ReviewerDispatch,
): Promise<ReviewGraphResult> {
  const decision = computeStartingTier(input.pr_meta, input.entry);
  const tier_log: ReviewGraphResult['tier_log'] = [];
  let tier: ReviewTier = decision.tier;
  let priorFindings: string | undefined;
  // Track the last successful parse separately so the fallthrough
  // return can surface it even when the FINAL tier's emit didn't
  // parse. tier_log[last].output would lose earlier parses in that
  // case — the docstring on ReviewGraphResult.output promises "the
  // last successful parse, when one happened".
  let lastParsedOutput: ReviewerOutput | undefined;

  // Skip R0 — it's the GH Copilot bot, fires server-side, not
  // dispatched. If computeStartingTier returned R0, bump to R1 to
  // start the dispatched chain. (R0's findings — copilot_comments —
  // are read in the prompt, not via dispatch.)
  if (tier === 'R0') tier = 'R1';

  // Saturate at R3 — beyond that we escalate to operator (R4) without
  // a dispatched step.
  //
  // Operator-escalation (`shouldEscalateToOperator`) only applies AT
  // R3: the heaviest dispatchable reviewer saying "I can't decide" or
  // "kick this up" is the operator's pickup signal. From R1/R2,
  // those same outputs (`confidence: low` / `decision: escalate`)
  // mean "let a heavier reviewer take it" — bump tier, not operator.
  while (tier === 'R1' || tier === 'R2' || tier === 'R3') {
    const reviewerReq = buildReviewerRequest(input, tier, priorFindings);
    const result = await reviewerDispatch(reviewerReq);
    const parsed = parseReviewerOutput(result.stdout_tail);

    if (!parsed.ok) {
      tier_log.push({ tier, parsed: false, error: parsed.error });
      // Treat unparseable output as a low-confidence signal — escalate
      // one tier. If we were already at R3, the next iteration's
      // `tier` will be R4 and the while-loop falls through to the
      // operator-escalation terminator below.
      tier = escalateOneTier(tier);
      priorFindings = formatParseFailureForNextTier(parsed.error, priorFindings);
      continue;
    }

    tier_log.push({ tier, parsed: true, output: parsed.output });
    lastParsedOutput = parsed.output;

    // R3-only: operator-escalation rule
    if (tier === 'R3' && shouldEscalateToOperator(parsed.output)) {
      return {
        final_tier: tier,
        action: 'escalate-to-operator',
        output: parsed.output,
        t5_shape: decision.t5_shape,
        tier_log,
      };
    }

    // R1/R2: low-confidence OR decision:escalate → bump tier (heavier
    // reviewer takes the call). At R3 these would have been caught
    // by the operator-escalation branch above; reaching here at
    // R1/R2 means the chain continues.
    if (
      tier !== 'R3' &&
      (parsed.output.confidence === 'low' || parsed.output.decision === 'escalate')
    ) {
      tier = escalateOneTier(tier);
      priorFindings = formatFindingsForNextTier(parsed.output.findings, priorFindings);
      continue;
    }

    if (parsed.output.decision === 'approve') {
      return {
        final_tier: tier,
        action: 'approve',
        output: parsed.output,
        t5_shape: decision.t5_shape,
        tier_log,
      };
    }

    if (parsed.output.decision === 'request_changes') {
      // High/medium confidence + request_changes → loop terminates;
      // gatekeeper re-dispatches the implementor with the findings.
      // (R3 + low confidence + request_changes was already caught by
      // the operator-escalation branch above.)
      return {
        final_tier: tier,
        action: 'request-changes',
        output: parsed.output,
        t5_shape: decision.t5_shape,
        tier_log,
      };
    }

    // Defensive fallthrough: decision='escalate' at R3 with high/medium
    // confidence. shouldEscalateToOperator already returned true (it
    // fires on decision='escalate' regardless of confidence), so this
    // path is unreachable. Kept for type completeness.
    return {
      final_tier: tier,
      action: 'escalate-to-operator',
      output: parsed.output,
      t5_shape: decision.t5_shape,
      tier_log,
    };
  }

  // Fell out of the loop with tier saturated past R3. Either every
  // tier escalated, or every tier produced unparseable output
  // (parse-failure cascade). Either way → operator. Distinguish in
  // `action` so telemetry can separate the two failure modes.
  //
  // `final_tier` reports the LAST DISPATCHED tier (R3 here), not R4.
  // R4 is the action category (operator pickup), not a tier the chain
  // visited. Telemetry consumers reading `tier_log` see exactly which
  // tiers ran.
  const lastEntry = tier_log[tier_log.length - 1];
  const action: ReviewGraphAction =
    lastEntry && !lastEntry.parsed && tier_log.every((e) => !e.parsed)
      ? 'parse-failure-at-r4'
      : 'escalate-to-operator';

  return {
    final_tier: lastEntry?.tier ?? 'R3',
    action,
    output: lastParsedOutput,
    t5_shape: decision.t5_shape,
    tier_log,
  };
}

// ─── Reviewer ExecutionRequest construction ────────────────────────────────

/**
 * Build an `ExecutionRequest` for the reviewer at `tier`. The agent
 * is told (via the prompt) what role it's playing and what to emit.
 * Driver + model + bounds come from REVIEW_TIER_DRIVER / WALL_TIMEOUT
 * / MAX_TOOL_CALLS.
 *
 * Each reviewer dispatch gets a deterministic-but-unique workflow_id
 * derived from the parent + tier so the chain is traversable via
 * gov-decisions and the temporal UI.
 */
function buildReviewerRequest(
  input: ReviewGraphInput,
  tier: ReviewTier,
  priorFindings: string | undefined,
): ExecutionRequest {
  const tierConfig = REVIEW_TIER_DRIVER[tier];
  if (!tierConfig.driver) {
    throw new Error(`tier ${tier} is not a dispatchable reviewer tier (R0/R4 are non-dispatchable)`);
  }

  if (input.pr_meta.pr_number === undefined || !input.pr_meta.pr_url) {
    throw new Error('pr_meta.pr_number and pr_meta.pr_url are required for reviewer dispatch');
  }

  const prompt = buildAdversarialReviewerPrompt({
    tier,
    pr_number: input.pr_meta.pr_number,
    pr_url: input.pr_meta.pr_url,
    entry_id: input.entry.id,
    entry_file_scope: input.entry.file,
    copilot_comments: input.copilot_comments,
    prior_findings: priorFindings,
  });

  // Map review-tier driver string to the typed DriverId. R3's
  // 'claude-code-headless' is already that exact id; R1+R2 use
  // 'copilot' (the Copilot CLI driver). REVIEW_TIER_MODEL is the
  // tier-specific model passed via env override (next slice wires
  // `CHITIN_MODEL_<DRIVER>_REV<TIER>` if/when we want per-review-tier
  // model selection — for now the driver's default tier-model wins).
  const driver = tierConfig.driver as DriverId;

  // Reviewer workflows run at "T2" tier (mid-cost reasoning). The
  // ExecutionRequestSchema's tier field drives model resolution —
  // for the reviewer path we want the explicit REVIEW_TIER_MODEL
  // not the implementor's tier-routing. Passing tier here is a
  // hint; the actual model lock-down lives in the env override.
  const reviewerWorkflowId = deriveReviewerWorkflowId(input.parent_workflow_id, tier);

  return ExecutionRequestSchema.parse({
    schema_version: '1',
    workflow_id: reviewerWorkflowId,
    run_id: `${reviewerWorkflowId}-attempt-1`,
    repo: input.repo,
    task_class: 'exploration',         // reviewers don't write code
    risk_level: 'low',
    allowed_drivers: [driver],
    network_policy: 'allowlist',       // gh CLI + git read needed
    write_policy: 'none',              // reviewer must NOT push (boundary with implementor)
    bounds: {
      max_tool_calls: REVIEW_TIER_MAX_TOOL_CALLS[tier],
      max_cost_usd: tier === 'R3' ? 1.0 : 0,   // R3 caps cost; R1/R2 are free under Copilot Pro
      wall_timeout_s: REVIEW_TIER_WALL_TIMEOUT_S[tier],
    },
    prompt,
    tier: 'T2' as Tier,                // model-tier hint; review-graph picks via REVIEW_TIER_DRIVER
    role: 'reviewer',
    parent_workflow_id: input.parent_workflow_id,
    step_index: tierAsStepIndex(tier),
  });
}

/**
 * Map a review tier to its 0-based step_index. Each dispatched
 * reviewer turn is one iteration of the escalation loop; R1 is the
 * first iteration (step 0), R2 the second (1), R3 the third (2).
 * Mirrors Lobster's loop.maxIterations counter as the schema
 * docstring describes.
 *
 * R0 (GH bot) doesn't dispatch through Temporal — its findings come
 * in via copilot_comments, not a workflow turn — so it has no
 * step_index. R4 is the operator-pickup terminator, also not
 * dispatched.
 */
function tierAsStepIndex(tier: ReviewTier): 0 | 1 | 2 {
  if (tier === 'R1') return 0;
  if (tier === 'R2') return 1;
  if (tier === 'R3') return 2;
  throw new Error(`tier ${tier} has no step_index`);
}

// Maximum length Temporal allows for a workflow_id / run_id (matches
// TemporalIdSchema in @chitin/contracts). When the parent's
// workflow_id is already at the cap, naively concatenating "-revrN"
// overflows and ExecutionRequestSchema.parse() rejects the request.
const TEMPORAL_WORKFLOW_ID_MAX = 128;

// run_id is constructed as `${workflow_id}${RUN_ID_SUFFIX}`. Both
// fields share the 128-char regex cap, so workflow_id has to leave
// room for this suffix — that's why the cap below is tighter than
// TEMPORAL_WORKFLOW_ID_MAX.
const RUN_ID_SUFFIX = '-attempt-1';

/**
 * Derive a reviewer workflow_id from the parent + tier, guaranteeing
 * BOTH the result and its derived `<workflow_id>-attempt-1` run_id
 * fit TemporalIdSchema (<=128 chars). The suffix is fixed length
 * ("-revrN" = 6 chars), so when the parent leaves no room we truncate
 * it. We keep the parent's prefix readable when possible (operator
 * can grep the gov-decisions chain by the parent prefix); truncation
 * is a fallback for the edge case.
 */
function deriveReviewerWorkflowId(parentId: string, tier: ReviewTier): string {
  const suffix = `-rev${tier.toLowerCase()}`; // e.g. "-revr1"
  // Headroom is the cap minus BOTH our suffix AND the run_id suffix
  // the schema-parse step appends downstream — otherwise a 128-char
  // workflow_id would push run_id past the cap.
  const headroom = TEMPORAL_WORKFLOW_ID_MAX - suffix.length - RUN_ID_SUFFIX.length;
  const head = parentId.length > headroom ? parentId.slice(0, headroom) : parentId;
  return `${head}${suffix}`;
}

/**
 * Format prior reviewer's findings as a bullet list the next tier's
 * prompt can consume. Limited to the most recent reviewer's findings
 * (don't pile up across the chain — keeps the prompt tight).
 */
function formatFindingsForNextTier(
  findings: ReviewerFinding[],
  _existingPrior: string | undefined,
): string {
  if (findings.length === 0) {
    return 'previous reviewer requested escalation but produced no findings';
  }
  return findings
    .map((f) => {
      const loc = f.line ? `${f.file}:${f.line}` : f.file;
      const fix = f.suggested_fix ? ` [suggested: ${f.suggested_fix}]` : '';
      return `- ${f.severity} ${loc} (${f.category}): ${f.summary}${fix}`;
    })
    .join('\n');
}

/**
 * When a tier's output failed to parse, format that as a "prior
 * findings" entry the next tier can read. Treats unparseable output
 * as a kind of low-confidence-on-something signal worth flagging.
 */
function formatParseFailureForNextTier(
  parseError: string,
  existingPrior: string | undefined,
): string {
  const note = `- 🟡 [parse-failure] previous reviewer's structured output failed to parse: ${parseError}. Treat this PR as if no review has happened at the previous tier.`;
  return existingPrior ? `${existingPrior}\n${note}` : note;
}

// ─── Temporal wrapper ──────────────────────────────────────────────────────

/**
 * The Temporal workflow chitin's review-graph runs as. Thin shell —
 * dispatches each reviewer via `executeChild(executeRequestWorkflow,
 * ...)` and delegates loop logic to runReviewGraphLoop.
 *
 * Register in worker.ts's workflowsPath module (workflow.ts re-export)
 * before the dispatcher can call `client.workflow.start(...)` with
 * this name.
 */
export async function reviewGraphWorkflow(input: ReviewGraphInput): Promise<ReviewGraphResult> {
  return runReviewGraphLoop(input, async (req) => {
    return await executeChild<typeof executeRequestWorkflow>('executeRequestWorkflow', {
      args: [req],
      workflowId: req.workflow_id,
    });
  });
}

export const __test__ = {
  buildReviewerRequest,
  formatFindingsForNextTier,
  formatParseFailureForNextTier,
  tierAsStepIndex,
  deriveReviewerWorkflowId,
  TEMPORAL_WORKFLOW_ID_MAX,
};
