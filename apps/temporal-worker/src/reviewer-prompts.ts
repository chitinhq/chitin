// Phase 2 step 2 of the swarm-as-software-factory design. The
// adversarial-review prompt template the review-graph (review-graph.ts)
// dispatches to at tiers R1-R3 + the structured-output parser the
// graph consumes from the agent's stdout.
//
// Replaces the `reviewer` role's stub from PR #130. The prompt is
// modeled on the manual adversarial-review passes from this session:
// they catch what Copilot misses (PR #78's 8/11 real-bug rate;
// today's bucket-B root-cause discovery; PR #109's notification-
// ordering bug) and the recipe is reproducible.
//
// Contract (mirrors ReviewerOutput / ReviewerFinding in review-graph.ts):
//
//   {
//     "decision": "approve" | "request_changes" | "escalate",
//     "confidence": "high" | "medium" | "low",
//     "findings": [
//       {"severity": "🔴" | "🟡" | "🟢", "file": "...", "line": 42,
//        "category": "bug" | "test_gap" | "design" | "doc",
//        "summary": "...", "suggested_fix": "..."}
//     ]
//   }
//
// Severity rules (also embedded in the prompt — must stay in sync):
//   🔴 = real bug, blocks merge
//   🟡 = worth fixing but doesn't block merge (feeds tech-debt-ledger)
//   🟢 = doc / nit / cosmetic, advisory only
//
// The graph escalates on:
//   - explicit decision: 'escalate'
//   - confidence: 'low' AND any 🔴 finding
//   (other gates — CI, bucket-B telemetry, T5-shape — are gatekeeper-
//    layer concerns; see review-graph.ts shouldEscalateToOperator
//    docstring.)

import { z } from 'zod';
import type { ReviewerOutput, ReviewerFinding, ReviewTier } from './review-graph.ts';

// ─── Zod schema for the agent's JSON output ────────────────────────────────

// The agent emits findings using emoji severity markers — easier for
// the LLM to produce reliably than (e.g.) integer levels. The schema
// validates them strictly so a hallucinated `🔵` or omitted field
// fails parse and the graph can react (typically: re-prompt or
// escalate-tier).

export const ReviewerFindingSchema: z.ZodType<ReviewerFinding> = z.object({
  severity: z.enum(['🔴', '🟡', '🟢']),
  file: z.string().min(1),
  line: z.number().int().positive().optional(),
  category: z.enum(['bug', 'test_gap', 'design', 'doc']),
  summary: z.string().min(1),
  suggested_fix: z.string().optional(),
});

export const ReviewerOutputSchema: z.ZodType<ReviewerOutput> = z.object({
  decision: z.enum(['approve', 'request_changes', 'escalate']),
  confidence: z.enum(['high', 'medium', 'low']),
  findings: z.array(ReviewerFindingSchema),
});

// ─── Prompt builder ────────────────────────────────────────────────────────

export interface ReviewerPromptInputs {
  /** Reviewer tier this prompt is for. Used to set tone (R1 = quick,
   *  R3 = adversarial-deep). */
  tier: ReviewTier;
  /** PR number for context — agents read GitHub via gh CLI when
   *  needed; this saves them a discovery hop. */
  pr_number: number;
  /** PR URL — same as above. */
  pr_url: string;
  /** Backlog entry id — what the PR is supposed to be doing. */
  entry_id: string;
  /** The entry's declared file: scope as a comma-separated string.
   *  Used for the diff-vs-scope mismatch check (the bucket-B
   *  detection signal). Empty / undefined = entry didn't declare a
   *  scope; reviewer evaluates without that constraint. */
  entry_file_scope?: string;
  /** Inline review comments Copilot's GH bot has left, formatted as
   *  newline-separated bullets the agent can verify one-by-one.
   *  Empty when Copilot hasn't reviewed yet (R0 still pending). */
  copilot_comments?: string;
  /** Previous reviewer's findings (when escalating from a lower
   *  tier). Newline-separated bullets. Empty on first dispatch. */
  prior_findings?: string;
}

const STRUCTURED_OUTPUT_INSTRUCTIONS = `\
At the END of your review, emit EXACTLY ONE JSON object on a single line, prefixed with the literal token \`<<<REVIEW>>>\` and nothing else after the closing brace on that line. No code fence, no commentary. The graph's parser keys on \`<<<REVIEW>>>\`. Example:

<<<REVIEW>>>{"decision":"request_changes","confidence":"high","findings":[{"severity":"🔴","file":"apps/temporal-worker/src/dispatcher.ts","line":372,"category":"bug","summary":"writeDispatchMarker fires before submit, so a submit failure leaves a marker without a workflow.","suggested_fix":"Move writeDispatchMarker after client.workflow.start() returns."}]}

Severity rules (load-bearing — match these EXACTLY):
- 🔴 = real bug. Blocks merge. Examples: incorrect logic, missing null/undefined guards, race conditions, security issues, incorrect git semantics, type errors that would crash at runtime.
- 🟡 = worth fixing but doesn't block merge. Examples: naming nits with real cost, missing test cases for non-obvious branches, code-smell that will rot, a TODO that should be filed in the debt ledger.
- 🟢 = cosmetic / doc / typo / advisory only. No merge gate.

Decision rules:
- "approve" — no 🔴 findings; the PR is mergeable as-is.
- "request_changes" — at least one 🔴; PR needs fixes before merge.
- "escalate" — you found something the next reviewer tier should weigh in on (e.g., architectural design call you don't feel confident judging at this tier).

Confidence rules:
- "high" — you read every changed line, verified each Copilot comment against the actual code, and tested the assertions you make. No reasonable next-tier reviewer would flip your decision.
- "medium" — you skimmed parts; if you returned 'approve' someone could plausibly find a 🔴 you missed.
- "low" — you can't fully evaluate the PR at this tier. Either escalate or request_changes.

The graph escalates to the next tier (or to a human at R3) when decision='escalate' OR (confidence='low' AND any 🔴 finding). So: if you're uncertain about a 🔴 finding, mark confidence:'low' rather than guessing.`;

const TIER_TONE: Record<ReviewTier, string> = {
  R0: '',  // not dispatched
  R1: `You are a tier-1 reviewer (Copilot CLI / GPT-4.1). Aim for a quick but rigorous pass — read the diff, verify each Copilot bot comment against actual code, flag obvious bugs. If something feels off but you'd need deep reasoning to be sure, return decision:'escalate' rather than guessing — a heavier reviewer (R2/R3) will pick it up.`,
  R2: `You are a tier-2 reviewer (Copilot CLI / Claude Sonnet 4.6). The PR has been bumped to you because it's mid-complexity, touches a schema/public-API surface, or the implementor was T3+. Treat this as the equivalent of a careful staff-engineer review: verify each line of the diff makes the change the entry asked for, no more no less. The 'diff doesn't match the entry's declared file: scope' check is a hard 🔴 — that's how bucket-B contamination got past the apply step on 2026-05-02 (4 PRs shipped a settings.json overwrite labeled as feature work).`,
  R3: `You are a tier-3 reviewer (Claude Opus 4.7). The PR has been bumped to you because it's heavy: large diff, kernel internals, or the lower-tier reviewers escalated. Treat yourself as a hostile reviewer:
- Look for cases the code doesn't handle (empty input, null/undefined, race conditions, edge values).
- Question assumptions: are the comments correct? does the test prove what it claims?
- Verify EACH Copilot bot comment against the actual code — do not auto-dismiss any (per chitin memory, PR #78 caught 8/11 real bugs Copilot flagged).
- Read the entry's declared file: scope and confirm the diff intersects it. If the diff is exclusively in files NOT named in the entry, that's a bucket-B contamination signature — emit a 🔴 with category:'design' and suggest closing-and-redispatching.
- Cross-check telemetry implications: does this change degrade success rates for any driver/tier? Is bucket-B preventable with this code?`,
  R4: '',  // not dispatched
};

/**
 * Build the adversarial-review prompt for a given tier. Returns a
 * single string the dispatcher passes as the agent's system+user
 * prompt (the wrapper that decides system-vs-user split lives in
 * activity.ts; this function just produces the content).
 */
export function buildAdversarialReviewerPrompt(opts: ReviewerPromptInputs): string {
  const tierTone = TIER_TONE[opts.tier];
  if (!tierTone) {
    throw new Error(`tier ${opts.tier} is not a dispatchable reviewer tier (R0/R4 are non-dispatchable)`);
  }

  const scopeBlock = opts.entry_file_scope
    ? `Entry's declared file: scope (the agent should ONLY have changed files matching these globs):\n${opts.entry_file_scope}`
    : `Entry's file: scope was not declared. Evaluate without the diff-scope check; flag if the diff is suspiciously broad.`;

  const copilotBlock = opts.copilot_comments
    ? `Copilot bot's inline comments (verify EACH one against the actual code — do not auto-dismiss):\n${opts.copilot_comments}`
    : `Copilot's bot review hasn't landed yet (or left no comments). Do your own pass.`;

  const priorBlock = opts.prior_findings
    ? `Previous reviewer's findings (you've been escalated to because they couldn't fully resolve):\n${opts.prior_findings}`
    : `You're the first reviewer.`;

  return `You are reviewing PR #${opts.pr_number} (${opts.pr_url}) for chitin. The PR is meant to implement backlog entry \`${opts.entry_id}\`.

${tierTone}

${scopeBlock}

${copilotBlock}

${priorBlock}

YOUR TOOLS: \`gh pr diff ${opts.pr_number}\`, \`gh pr view ${opts.pr_number}\`, \`gh api repos/chitinhq/chitin/pulls/${opts.pr_number}/comments\`, plus \`read\` on any file in the repo. You're allowed to run tests; you're allowed to query telemetry via \`python/analysis/\`. You're NOT allowed to push to the PR's branch — that's the implementor's job; if you find a 🔴 the graph re-dispatches to the implementor (or escalates to a higher reviewer).

${STRUCTURED_OUTPUT_INSTRUCTIONS}`;
}

// ─── Output parser ─────────────────────────────────────────────────────────

const REVIEW_MARKER = '<<<REVIEW>>>';

/**
 * Extract the reviewer's structured decision from agent stdout.
 *
 * The agent is instructed to emit `<<<REVIEW>>>{...json...}` on a
 * single line at the end. We scan for the marker (last occurrence
 * wins, so an example marker in the prompt that the agent echoes
 * doesn't false-match), JSON-parse the slice, and validate against
 * ReviewerOutputSchema.
 *
 * Returns:
 *   { ok: true, output }      — well-formed
 *   { ok: false, error }      — missing marker, JSON parse error, or
 *                                schema validation failure
 *
 * The graph escalates a tier on { ok: false } — either the agent
 * didn't follow the contract (next tier may; or operator), or it's
 * a sign the prompt template needs hardening.
 */
export function parseReviewerOutput(
  stdoutTail: string,
): { ok: true; output: ReviewerOutput } | { ok: false; error: string } {
  const lastMarker = stdoutTail.lastIndexOf(REVIEW_MARKER);
  if (lastMarker < 0) {
    return { ok: false, error: 'no <<<REVIEW>>> marker in stdout' };
  }
  const after = stdoutTail.slice(lastMarker + REVIEW_MARKER.length);
  // Take everything up to the first newline (the contract is "one
  // line"). This shields against trailing noise the agent might
  // emit despite instructions.
  const lineEnd = after.indexOf('\n');
  const candidate = (lineEnd >= 0 ? after.slice(0, lineEnd) : after).trim();
  if (!candidate.startsWith('{')) {
    return { ok: false, error: `expected JSON object after marker, got: ${truncate(candidate, 200)}` };
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(candidate);
  } catch (err) {
    // Include the candidate slice so debugging doesn't require
    // re-fetching the raw stdout. Truncated to keep error messages
    // readable in logs (full slice is in stdout_tail anyway).
    return {
      ok: false,
      error:
        `JSON.parse failed: ${err instanceof Error ? err.message : String(err)} ` +
        `(candidate: ${truncate(candidate, 200)})`,
    };
  }

  const result = ReviewerOutputSchema.safeParse(parsed);
  if (!result.success) {
    return {
      ok: false,
      error: `schema validation: ${result.error.message} (candidate: ${truncate(candidate, 200)})`,
    };
  }
  return { ok: true, output: result.data };
}

function truncate(s: string, max: number): string {
  return s.length <= max ? s : `${s.slice(0, max)}…[+${s.length - max} more chars]`;
}

export const __test__ = {
  REVIEW_MARKER,
  TIER_TONE,
  STRUCTURED_OUTPUT_INSTRUCTIONS,
};
