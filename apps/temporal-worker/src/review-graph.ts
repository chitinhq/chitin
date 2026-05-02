// Review-tier escalation graph — §5 of docs/design/2026-05-02-swarm-as-software-factory.md
//
// Implements the R0→R1→R2→R3→R4 chain as a Temporal multi-step workflow.
// Each tier dispatches a child executeRequestWorkflow at the appropriate
// driver + model; the reviewer's structured decision is carried forward as
// context for the next tier. Chain is traversable via parent_workflow_id.
//
// NOTE: reviewGraphWorkflow must be registered in worker.ts's workflowsPath
// module (workflow.ts re-export) before it can be dispatched by Temporal.
// The dispatcher integration (enqueue on PR open) is a follow-up step.

import { executeChild, workflowInfo } from '@temporalio/workflow';
import type { ExecutionRequest, DriverId } from '@chitin/contracts';
import type { ActivityResult } from './activity-types.ts';

// ─── Review tier types ───────────────────────────────────────────────────────

export type ReviewTier = 'R0' | 'R1' | 'R2' | 'R3' | 'R4';

// Ordered sequence used by escalateOneTier and computeStartingTier.
// Invariant: tier order is monotonically increasing cost/capability.
const TIER_ORDER: ReviewTier[] = ['R0', 'R1', 'R2', 'R3', 'R4'];

// Driver assigned to each programmatic reviewer tier.
// R0 = Copilot bot (automated by GitHub — no programmatic dispatch needed).
// R4 = operator escalation — never dispatched.
export const REVIEW_TIER_DRIVER: Record<ReviewTier, DriverId | null> = {
  R0: null,              // GitHub Copilot bot — runs automatically, not dispatched
  R1: 'copilot',         // Copilot CLI w/ GPT-4.1 or Haiku-4.5
  R2: 'copilot',         // Copilot CLI w/ GPT-5.4 or Sonnet-4.6
  R3: 'claude-code-headless', // claude-opus-4-7
  R4: null,              // operator escalation — no programmatic driver
};

// Human-readable model hint for each tier (for prompt context / logging).
const REVIEW_TIER_MODEL: Record<ReviewTier, string> = {
  R0: 'copilot-bot',
  R1: 'gpt-4.1',
  R2: 'claude-sonnet-4-6',
  R3: 'claude-opus-4-7',
  R4: 'operator',
};

// Per-tier Temporal bounds for child reviewer workflows.
const REVIEW_TIER_WALL_TIMEOUT_S: Record<ReviewTier, number> = {
  R0: 0,
  R1: 600,  // 10 min
  R2: 900,  // 15 min
  R3: 1200, // 20 min — Opus gets full room for hard PRs
  R4: 0,
};

const REVIEW_TIER_MAX_TOOL_CALLS: Record<ReviewTier, number> = {
  R0: 0,
  R1: 20,
  R2: 30,
  R3: 50,
  R4: 0,
};

// ─── Input / output types ────────────────────────────────────────────────────

// PR metadata produced by the programmer apply step and supplied to reviewGraphWorkflow.
export interface PrMeta {
  prUrl: string;
  prNumber: number;
  diffLoc: number;             // total lines changed (insertions + deletions)
  filesChanged: number;        // number of distinct files in the diff
  copilotCommentCount: number; // inline comments left by the Copilot bot (R0 output)
  touchesSchemaFiles: boolean; // diff includes libs/contracts/src/*.schema.ts
  touchesKernelInternals: boolean; // diff includes go/execution-kernel/internal/gov/ etc.
  touchesPublicApiExports: boolean; // diff includes top-level exports in apps/*
}

// Entry context relevant to reviewer routing.
export interface ReviewEntryContext {
  entryId: string;
  implementorTier: string; // 'T0'..'T4' — tier of the programmer workflow
  fileScope: string;       // entry's declared file: field
  priorAttempts: number;   // number of prior failed dispatch attempts for this entry
}

// Structured decision emitted by a reviewer agent.
// The reviewer prompt mandates this exact JSON shape — parseDecision tolerates malformed output.
export interface ReviewerDecision {
  decision: 'approve' | 'request_changes' | 'escalate';
  severity: 'clean' | 'low' | 'medium' | 'high';
  confidence: 'high' | 'medium' | 'low';
  findings: Array<{
    location: string; // '<file>:<line>' or description
    reason: string;   // one sentence
    severity: '🔴' | '🟡' | '🟢';
  }>;
  summary: string;
}

// Input envelope for reviewGraphWorkflow.
export interface ReviewGraphInput {
  prMeta: PrMeta;
  entry: ReviewEntryContext;
  // workflow_id of the programmer run that opened this PR (parent_workflow_id in §4 contract)
  programmerWorkflowId: string;
  // PR diff text — passed to each reviewer; truncated to 8 KB in the prompt
  diffText: string;
}

// Result envelope produced by reviewGraphWorkflow.
export interface ReviewGraphResult {
  finalTier: ReviewTier;
  finalDecision: ReviewerDecision;
  // number of programmatic reviewer tiers that actually ran (R1+ only; R0 is pre-computed)
  chainLength: number;
  escalatedToOperator: boolean;
  programmerWorkflowId: string;
}

// ─── §5 trigger matrix ────────────────────────────────────────────────────────

// Compute the starting review tier from the §5 trigger matrix.
// Returns R0 when no triggers fire (PR is clean — approve without programmatic reviewer).
// Returns R1/R2/R3 when triggers bump the tier.
// Returns R4 only for T5-shape paths (chitin.yaml / .chitin/) — direct escalation.
//
// Invariant: computeStartingTier is a pure function of (prMeta, entry).
// Invariant: return value is in TIER_ORDER; no trigger fires below R0.
export function computeStartingTier(prMeta: PrMeta, entry: ReviewEntryContext): ReviewTier {
  let tier: ReviewTier = 'R0';

  const bump = (to: ReviewTier): void => {
    if (TIER_ORDER.indexOf(to) > TIER_ORDER.indexOf(tier)) {
      tier = to;
    }
  };

  // §5: Copilot bot leaves > 2 inline comments → R1
  if (prMeta.copilotCommentCount > 2) bump('R1');

  // §5: Diff > 200 LOC or > 10 files → R2; > 500 LOC or > 20 files → R3
  if (prMeta.diffLoc > 500 || prMeta.filesChanged > 20) {
    bump('R3');
  } else if (prMeta.diffLoc > 200 || prMeta.filesChanged > 10) {
    bump('R2');
  }

  // §5: Touches schema files → R2 minimum
  if (prMeta.touchesSchemaFiles) bump('R2');

  // §5: Touches kernel internals → R3 minimum
  if (prMeta.touchesKernelInternals) bump('R3');

  // §5: Touches public API exports → R2 minimum
  if (prMeta.touchesPublicApiExports) bump('R2');

  // §5: Implementor was tier T3 or T4 → R2 minimum
  if (entry.implementorTier === 'T3' || entry.implementorTier === 'T4') bump('R2');

  // §5: Previous attempts at this entry failed → one tier above last reviewer.
  // Conservative mapping: first retry → R2, subsequent retries → R3.
  if (entry.priorAttempts === 1) bump('R2');
  else if (entry.priorAttempts > 1) bump('R3');

  return tier;
}

// ─── Tier escalation ──────────────────────────────────────────────────────────

// Advance one tier up the escalation chain.
// Invariant: escalateOneTier(t) > t for all t ≠ 'R4' (strictly higher in TIER_ORDER).
// Invariant: escalateOneTier('R4') === 'R4' (terminal — operator escalation).
export function escalateOneTier(current: ReviewTier): ReviewTier {
  const idx = TIER_ORDER.indexOf(current);
  return TIER_ORDER[Math.min(idx + 1, TIER_ORDER.length - 1)] as ReviewTier;
}

// ─── Reviewer prompt template ─────────────────────────────────────────────────

// Build the reviewer-role prompt for a given tier.
// The reviewer consumes the diff + previous findings and emits a structured JSON decision.
// No edit/git tools should be invoked — the prompt forbids it explicitly.
export function buildReviewerPrompt(opts: {
  tier: ReviewTier;
  diffText: string;
  fileScope: string;
  entryId: string;
  prUrl: string;
  previousFindings: ReviewerDecision | null;
}): string {
  const { tier, diffText, fileScope, entryId, prUrl, previousFindings } = opts;
  const model = REVIEW_TIER_MODEL[tier];

  const prevSection = previousFindings
    ? `\n## Previous reviewer findings\n\n\`\`\`json\n${JSON.stringify(previousFindings, null, 2)}\n\`\`\`\n\nYou MUST address or escalate each 🔴 finding above. 🟡/🟢 are informational.\n`
    : '';

  return `You are a ${tier} code reviewer (model hint: ${model}) in the chitin swarm factory.

## Context

- PR: ${prUrl}
- Backlog entry: \`${entryId}\`
- Declared file scope: ${fileScope}
- Review tier: ${tier}
${prevSection}
## Task

Review the diff below for:
1. Correctness — bugs, logic errors, invariant violations
2. Scope adherence — the diff must only touch files declared in the entry's file scope
3. Quality — naming, error handling, security

DO NOT use edit, write, or git tools. Read-only review only.
Output ONLY the JSON object below — no leading prose, no trailing prose.

## Required output format

{
  "decision": "approve" | "request_changes" | "escalate",
  "severity": "clean" | "low" | "medium" | "high",
  "confidence": "high" | "medium" | "low",
  "findings": [
    {
      "location": "<file>:<line> or description",
      "reason": "one sentence",
      "severity": "🔴" | "🟡" | "🟢"
    }
  ],
  "summary": "one sentence overall assessment"
}

Severity semantics:
- 🔴 = real bug or correctness issue; blocks merge
- 🟡 = style / naming / doc concern; does not block
- 🟢 = nit; informational only

Decision semantics:
- "approve": diff is correct, in-scope, no 🔴 findings
- "request_changes": one or more 🔴 findings a programmer could fix
- "escalate": structural ambiguity, confidence low, or touches governance/safety paths
  that exceed your reviewer authority

## PR Diff

\`\`\`diff
${diffText.slice(0, 8000)}${diffText.length > 8000 ? '\n... (truncated)' : ''}
\`\`\`

Respond with ONLY the JSON object.`;
}

// ─── Decision parsing ─────────────────────────────────────────────────────────

// Parse structured ReviewerDecision from an ActivityResult's stdout_tail.
// Tolerates partial or malformed output — returns a pessimistic 'escalate' decision
// with confidence: 'low' so the escalation loop handles it safely.
//
// Invariant: parseDecision always returns a fully-shaped ReviewerDecision.
export function parseDecision(result: ActivityResult, tier: ReviewTier): ReviewerDecision {
  try {
    // Find the outermost JSON object containing "decision" in the stdout tail.
    const jsonMatch = result.stdout_tail.match(/\{[\s\S]*?"decision"[\s\S]*?\}/);
    if (jsonMatch) {
      const parsed = JSON.parse(jsonMatch[0]) as Partial<ReviewerDecision>;
      return {
        decision: parsed.decision ?? 'escalate',
        severity: parsed.severity ?? 'high',
        confidence: parsed.confidence ?? 'low',
        findings: Array.isArray(parsed.findings) ? parsed.findings : [],
        summary: parsed.summary ?? '(no summary)',
      };
    }
  } catch {
    // fall through to pessimistic default
  }
  return {
    decision: 'escalate',
    severity: 'high',
    confidence: 'low',
    findings: [],
    summary: `${tier} reviewer output could not be parsed (exit_code=${result.exit_code})`,
  };
}

// ─── Escalation predicate ─────────────────────────────────────────────────────

// True if a decision requires immediate operator escalation rather than
// advancing one more tier programmatically.
//
// §5 escalation-to-R4 criteria:
//   - R3 returns confidence: low
//   - R3 returns 'escalate' decision
//   - Any tier returns 🔴 and we are already at R3
function requiresOperatorEscalation(decision: ReviewerDecision, tier: ReviewTier): boolean {
  if (decision.decision === 'escalate') return true;
  if (tier === 'R3' && decision.confidence === 'low') return true;
  if (tier === 'R3' && decision.findings.some((f) => f.severity === '🔴')) return true;
  return false;
}

// ─── Reviewer ExecutionRequest builder ───────────────────────────────────────

function buildReviewerRequest(opts: {
  tier: ReviewTier;
  input: ReviewGraphInput;
  previousFindings: ReviewerDecision | null;
  parentWorkflowId: string;
}): ExecutionRequest {
  const { tier, input, previousFindings, parentWorkflowId } = opts;
  const driver = REVIEW_TIER_DRIVER[tier];
  if (!driver) {
    // R0 and R4 have no programmatic driver — caller must guard before calling this.
    throw new Error(`buildReviewerRequest called for non-dispatchable tier ${tier}`);
  }
  const workflowId = `review-${input.entry.entryId}-${tier.toLowerCase()}-${Date.now()}`;

  // parent_workflow_id and role are Phase 1 schema additions (multi-step-flows /
  // role-typed-backlog-entries — PR #130). Until those schema fields land the values
  // are embedded as prompt context only. The prompt header carries both so the
  // reviewer agent sees them even without schema enforcement.
  const prompt = buildReviewerPrompt({
    tier,
    diffText: input.diffText,
    fileScope: input.entry.fileScope,
    entryId: input.entry.entryId,
    prUrl: input.prMeta.prUrl,
    previousFindings,
  });

  // Embed lineage in the prompt header since schema fields aren't available yet.
  const lineageHeader = [
    `<!-- parent_workflow_id: ${parentWorkflowId} -->`,
    `<!-- role: reviewer -->`,
    '',
  ].join('\n');

  return {
    schema_version: '1',
    workflow_id: workflowId,
    run_id: `${workflowId}-attempt-1`,
    repo: 'chitinhq/chitin',
    task_class: 'exploration',
    risk_level: 'low',
    allowed_drivers: [driver],
    network_policy: 'allowlist',
    // Reviewers are read-only — no worktree, no writes.
    write_policy: 'none',
    bounds: {
      max_tool_calls: REVIEW_TIER_MAX_TOOL_CALLS[tier],
      max_cost_usd: 0,
      wall_timeout_s: REVIEW_TIER_WALL_TIMEOUT_S[tier],
    },
    prompt: lineageHeader + prompt,
    // No base_ref: reviewers read the diff from the prompt, not a worktree.
  };
}

// ─── Temporal workflow ────────────────────────────────────────────────────────

// Maximum number of programmatic reviewer tiers before forcing operator escalation.
// Chain: R1 → R2 → R3 = 3 steps. Cap at 4 to leave room for an unexpected R0→R1→R2→R3 path.
// Invariant: chainLength ≤ MAX_CHAIN_LENGTH in every execution path.
const MAX_CHAIN_LENGTH = 4;

// reviewGraphWorkflow walks the R0→R1→R2→R3→R4 escalation chain:
//   1. computeStartingTier() determines the minimum tier from §5 triggers.
//   2. If starting tier is R0 and copilotCommentCount ≤ 2 and no other trigger fired,
//      the PR is clean — return approve at R0 without dispatching any child.
//   3. Otherwise, dispatch a child executeRequestWorkflow for the current tier.
//   4. Parse the structured decision from the child's stdout_tail.
//   5. On approve with no 🔴 findings → return the approval.
//   6. On requiresOperatorEscalation → return R4 escalation.
//   7. Otherwise → escalateOneTier, carry findings forward, recurse.
//
// The chain is traversable end-to-end via programmerWorkflowId (the parent) and
// each child's workflow_id (embedded in each child's ExecutionRequest).
//
// NOTE: This workflow must be exported from workflow.ts and registered with the
// Temporal worker before it can be dispatched. See worker.ts workflowsPath.
export async function reviewGraphWorkflow(input: ReviewGraphInput): Promise<ReviewGraphResult> {
  const startingTier = computeStartingTier(input.prMeta, input.entry);

  // R0 clean path: no programmatic reviewer needed.
  if (startingTier === 'R0') {
    return {
      finalTier: 'R0',
      finalDecision: {
        decision: 'approve',
        severity: 'clean',
        confidence: 'high',
        findings: [],
        summary: 'R0 Copilot bot review passed; no escalation triggers fired',
      },
      chainLength: 0,
      escalatedToOperator: false,
      programmerWorkflowId: input.programmerWorkflowId,
    };
  }

  // R4 direct path: T5-shape content or other immediate escalation signal.
  if (startingTier === 'R4') {
    return {
      finalTier: 'R4',
      finalDecision: {
        decision: 'escalate',
        severity: 'high',
        confidence: 'high',
        findings: [],
        summary: 'Governance/safety path detected — escalated directly to operator',
      },
      chainLength: 0,
      escalatedToOperator: true,
      programmerWorkflowId: input.programmerWorkflowId,
    };
  }

  let tier: ReviewTier = startingTier;
  let previousFindings: ReviewerDecision | null = null;
  let chainLength = 0;
  const parentWorkflowId = workflowInfo().workflowId;

  while (chainLength < MAX_CHAIN_LENGTH) {
    if (tier === 'R4') {
      return {
        finalTier: 'R4',
        finalDecision: previousFindings ?? {
          decision: 'escalate',
          severity: 'high',
          confidence: 'low',
          findings: [],
          summary: 'Escalated to operator — reviewers could not reach consensus',
        },
        chainLength,
        escalatedToOperator: true,
        programmerWorkflowId: input.programmerWorkflowId,
      };
    }

    const req = buildReviewerRequest({
      tier,
      input,
      previousFindings,
      parentWorkflowId,
    });

    // Dispatch the reviewer as a child workflow and wait for its result.
    const result = await executeChild<ActivityResult>('executeRequestWorkflow', {
      args: [req],
      taskQueue: 'chitin-worker-q',
      workflowId: req.workflow_id,
    });

    const decision = parseDecision(result, tier);
    chainLength++;

    // Approved with no blocking findings — exit the chain.
    if (
      decision.decision === 'approve' &&
      !decision.findings.some((f) => f.severity === '🔴')
    ) {
      return {
        finalTier: tier,
        finalDecision: decision,
        chainLength,
        escalatedToOperator: false,
        programmerWorkflowId: input.programmerWorkflowId,
      };
    }

    // Operator escalation required — cannot go higher programmatically.
    if (requiresOperatorEscalation(decision, tier)) {
      return {
        finalTier: 'R4',
        finalDecision: decision,
        chainLength,
        escalatedToOperator: true,
        programmerWorkflowId: input.programmerWorkflowId,
      };
    }

    // Advance one tier, carrying findings forward as context for the next reviewer.
    previousFindings = decision;
    tier = escalateOneTier(tier);
  }

  // Exhausted MAX_CHAIN_LENGTH without consensus — force operator escalation.
  return {
    finalTier: 'R4',
    finalDecision: previousFindings ?? {
      decision: 'escalate',
      severity: 'high',
      confidence: 'low',
      findings: [],
      summary: `Chain length (${MAX_CHAIN_LENGTH}) exhausted without reviewer consensus`,
    },
    chainLength,
    escalatedToOperator: true,
    programmerWorkflowId: input.programmerWorkflowId,
  };
}
