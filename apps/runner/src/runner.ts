// chitin-agent-runner — direct CLI invocation of runAgentTurn for the
// Lobster review-graph port.
//
// Lobster's `when:` expression DSL only reads $step.json.<path> /
// $step.stdout / $step.approved — it cannot read $step.exit_code. So
// reviewers must always exit 0 and encode their verdict in a JSON
// envelope on stdout. This CLI is the small adapter that:
//
//   1. parses CLI args (--role, --tier, --pr-meta, --entry-id, --repo)
//   2. constructs an ExecutionRequest matching review-graph-workflow.ts's
//      buildReviewerRequest (inline duplicate for now; refactor candidate)
//   3. invokes runAgentTurn (the same activity used by the Temporal path)
//   4. parses the structured ReviewerOutput from agent stdout
//   5. emits a JSON envelope: { tier, escalate, decision, findings, ok }
//   6. always exits 0
//
// `--mock-decision <approve|escalate|request_changes>` skips runAgentTurn
// and emits a canned envelope — used to validate the .lobster wiring
// without spending LLM credits.

import { parseArgs } from 'node:util';
import type { ExecutionRequest, DriverId, Tier } from '@chitin/contracts';
import { runAgentTurn } from './activity.ts';
import {
  REVIEW_TIER_DRIVER,
  REVIEW_TIER_WALL_TIMEOUT_S,
  REVIEW_TIER_MAX_TOOL_CALLS,
  type ReviewTier,
  type PrMeta,
} from './review-graph.ts';
import {
  buildAdversarialReviewerPrompt,
  parseReviewerOutput,
} from './reviewer-prompts.ts';

type Decision = 'approve' | 'request_changes' | 'escalate';

interface Envelope {
  tier: string;
  escalate: boolean;
  decision: Decision | 'parse_failed';
  findings: unknown[];
  ok: boolean;
  error?: string;
  duration_ms?: number;
}

function emit(env: Envelope): never {
  process.stdout.write(JSON.stringify(env) + '\n');
  process.exit(0);
}

function buildReviewerRequest(opts: {
  role: 'reviewer';
  tier: ReviewTier;
  pr_meta: PrMeta;
  entry_id: string;
  entry_file_scope: string;
  repo: string;
  parent_workflow_id: string;
}): ExecutionRequest {
  const tierConfig = REVIEW_TIER_DRIVER[opts.tier];
  if (!tierConfig.driver) {
    throw new Error(`tier ${opts.tier} is not a dispatchable reviewer tier (R0/R4 are non-dispatchable here)`);
  }
  if (opts.pr_meta.pr_number === undefined || !opts.pr_meta.pr_url) {
    throw new Error('pr_meta.pr_number and pr_meta.pr_url are required for reviewer dispatch');
  }

  const prompt = buildAdversarialReviewerPrompt({
    tier: opts.tier,
    pr_number: opts.pr_meta.pr_number,
    pr_url: opts.pr_meta.pr_url,
    entry_id: opts.entry_id,
    entry_file_scope: opts.entry_file_scope,
    copilot_comments: undefined,
    prior_findings: undefined,
  });

  const driver = tierConfig.driver as DriverId;
  const reviewerWorkflowId = `${opts.parent_workflow_id}-rev-${opts.tier}`;

  return {
    schema_version: '1',
    workflow_id: reviewerWorkflowId,
    run_id: `${reviewerWorkflowId}-attempt-1`,
    repo: opts.repo,
    task_class: 'exploration',
    risk_level: 'low',
    allowed_drivers: [driver],
    network_policy: 'allowlist',
    write_policy: 'none',
    bounds: {
      max_tool_calls: REVIEW_TIER_MAX_TOOL_CALLS[opts.tier],
      max_cost_usd: opts.tier === 'R3' ? 1.0 : 0,
      wall_timeout_s: REVIEW_TIER_WALL_TIMEOUT_S[opts.tier],
    },
    prompt,
    tier: 'T2' as Tier,
    role: 'reviewer',
    parent_workflow_id: opts.parent_workflow_id,
    step_index: opts.tier === 'R1' ? 0 : opts.tier === 'R2' ? 1 : 2,
  };
}

async function main(): Promise<void> {
  const { values } = parseArgs({
    options: {
      role:                { type: 'string' },
      tier:                { type: 'string' },
      'pr-meta':           { type: 'string', default: '{}' },
      'entry-id':          { type: 'string', default: '' },
      'entry-file-scope':  { type: 'string', default: '' },
      repo:                { type: 'string', default: '' },
      'parent-workflow-id':{ type: 'string', default: 'standalone' },
      'mock-decision':     { type: 'string' },
    },
    strict: false,
  });

  const tier = (values.tier ?? 'R1') as ReviewTier;
  const role = (values.role ?? 'reviewer') as string;

  if (role !== 'reviewer') {
    emit({ tier, escalate: false, decision: 'parse_failed', findings: [], ok: false,
           error: `runner only supports --role reviewer (got ${role})` });
  }

  // --mock-decision short-circuits runAgentTurn so we can validate the
  // Lobster wiring without spending LLM credits.
  if (values['mock-decision']) {
    const decision = values['mock-decision'] as Decision;
    emit({ tier, escalate: decision === 'escalate', decision, findings: [], ok: true });
  }

  let pr_meta: PrMeta;
  try {
    pr_meta = JSON.parse(values['pr-meta'] as string);
  } catch (err) {
    emit({ tier, escalate: false, decision: 'parse_failed', findings: [], ok: false,
           error: `--pr-meta is not valid JSON: ${err instanceof Error ? err.message : String(err)}` });
  }

  let req: ExecutionRequest;
  try {
    req = buildReviewerRequest({
      role: 'reviewer',
      tier,
      pr_meta: pr_meta!,
      entry_id: values['entry-id'] as string,
      entry_file_scope: values['entry-file-scope'] as string,
      repo: values.repo as string,
      parent_workflow_id: values['parent-workflow-id'] as string,
    });
  } catch (err) {
    emit({ tier, escalate: false, decision: 'parse_failed', findings: [], ok: false,
           error: `request build failed: ${err instanceof Error ? err.message : String(err)}` });
  }

  const t0 = Date.now();
  let result;
  try {
    result = await runAgentTurn(req!);
  } catch (err) {
    emit({ tier, escalate: false, decision: 'parse_failed', findings: [], ok: false,
           error: `runAgentTurn threw: ${err instanceof Error ? err.message : String(err)}`,
           duration_ms: Date.now() - t0 });
  }

  const parsed = parseReviewerOutput(result!.stdout_tail);
  if (!parsed.ok) {
    // Per chitin's existing escalate-on-parse-fail rule (review-graph.ts):
    // an unparseable reviewer turn escalates — next tier may produce a
    // well-formed verdict, or operator picks up at R4.
    emit({ tier, escalate: true, decision: 'parse_failed', findings: [], ok: false,
           error: parsed.error, duration_ms: result!.duration_ms });
  }

  emit({
    tier,
    escalate: parsed.output.decision === 'escalate',
    decision: parsed.output.decision,
    findings: parsed.output.findings,
    ok: true,
    duration_ms: result!.duration_ms,
  });
}

main().catch((err) => {
  // Last-resort: any uncaught path still produces a well-formed envelope
  // so Lobster's `when:` expressions never see undefined.
  emit({
    tier: 'unknown',
    escalate: true,  // unknown error → escalate to next tier (safe default)
    decision: 'parse_failed',
    findings: [],
    ok: false,
    error: `uncaught: ${err instanceof Error ? err.message : String(err)}`,
  });
});
