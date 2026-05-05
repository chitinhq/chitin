// chitin-execute-request — generic CLI that runs an ExecutionRequest
// to completion, including kernel-driven mid-task tier escalation.
//
// Reads an ExecutionRequest from `--request-file <path>`, calls
// runAgentTurn, and LOOPS on the kernel's `escalation_requested`
// signal: when the router/advisor decides the current tier can't
// finish the work, it bumps the tier, injects the prior advisor's
// nudge as escalation context, and re-spawns. Repeats until either
// success, hard failure, or the attempt cap is reached.
//
// At T4, the loop switches the agent's role to `advisor` rather
// than bumping further (the advisor diagnoses why the work didn't
// land at lower tiers; doesn't try to fix it directly).
//
// On terminal exit, the FINAL ActivityResult is printed to stdout
// (NDJSON one line). Per-attempt traces are kept in stderr as
// structured log lines so the operator / dashboard can replay the
// escalation chain.
//
// See docs/design/2026-05-03-mid-task-continuation.md for the
// architectural intent. The Temporal-flavored continueAsNew
// implementation in that doc is replaced here by an in-process
// loop (post-Temporal).

import { parseArgs } from 'node:util';
import { readFileSync } from 'node:fs';
import { ExecutionRequestSchema, type ExecutionRequest, type Tier } from '@chitin/contracts';
import { runAgentTurn } from './activity.ts';
import type { ActivityResult } from './activity-types.ts';

const MAX_ATTEMPTS = parseInt(process.env.CHITIN_RUNNER_MAX_ATTEMPTS ?? '5', 10);

const TIER_LADDER: Tier[] = ['T0', 'T1', 'T2', 'T3', 'T4'];

function bumpTier(current: Tier): Tier {
  const idx = TIER_LADDER.indexOf(current);
  if (idx < 0 || idx === TIER_LADDER.length - 1) return current;
  return TIER_LADDER[idx + 1];
}

/**
 * Inject the prior tier's advisor nudge as a prompt prefix so the
 * higher-tier driver picks up where the lower one left off. The
 * format mirrors the design doc's Step 1 carrier.
 */
function withEscalationPrefix(req: ExecutionRequest): ExecutionRequest {
  if (!req.escalation_context) return req;
  const ec = req.escalation_context;
  const prefix =
    `# MID-TASK CONTINUATION\n\n` +
    `You are picking up a task that was started by a lower-tier driver and escalated\n` +
    `to you mid-flight by chitin's router/advisor. The prior driver's context:\n\n` +
    `- prior_tier: ${ec.from_tier}\n` +
    `- attempt: ${ec.attempt}\n` +
    `- advisor_nudge: ${ec.advisor_nudge}\n\n` +
    `The task itself is below. Read the prior driver's context, then proceed.\n\n` +
    `---\n\n`;
  return { ...req, prompt: prefix + req.prompt };
}

function logLine(level: 'info' | 'warn' | 'error', msg: string, fields: Record<string, unknown> = {}): void {
  const line = JSON.stringify({
    ts: new Date().toISOString(),
    level,
    component: 'chitin-execute-request',
    msg,
    ...fields,
  });
  process.stderr.write(line + '\n');
}

/**
 * Pure escalation loop — extracted so tests can pin the contract
 * without spinning up a real agent. Calls `runFn` per attempt; on
 * `escalation_requested` it bumps tier (or switches to advisor at T4)
 * and re-invokes with the prior nudge as escalation context.
 *
 * Returns the FINAL ActivityResult, possibly with `escalation_exhausted: true`
 * when the attempt cap was hit while still escalating.
 */
export interface RunWithEscalationDeps {
  runFn: (req: ExecutionRequest) => Promise<ActivityResult>;
  log?: (level: 'info' | 'warn' | 'error', msg: string, fields?: Record<string, unknown>) => void;
  maxAttempts?: number;
}

export interface ActivityResultWithExhaustion extends ActivityResult {
  escalation_exhausted?: boolean;
}

export async function runWithEscalation(
  initial: ExecutionRequest,
  deps: RunWithEscalationDeps,
): Promise<ActivityResultWithExhaustion> {
  const log = deps.log ?? logLine;
  const max = deps.maxAttempts ?? MAX_ATTEMPTS;
  let req = initial;
  let attempt = 1;
  let lastResult: ActivityResult | undefined;

  while (attempt <= max) {
    const reqWithContext = withEscalationPrefix(req);
    log('info', 'attempt-start', {
      workflow_id: req.workflow_id,
      attempt,
      tier: req.tier,
      role: req.role,
      escalation_context: req.escalation_context,
    });

    const result = await deps.runFn(reqWithContext);
    lastResult = result;

    log('info', 'attempt-end', {
      workflow_id: req.workflow_id,
      attempt,
      tier: req.tier,
      exit_code: result.exit_code,
      escalation_requested: !!result.escalation_requested,
    });

    if (!result.escalation_requested) {
      log('info', 'terminal', { workflow_id: req.workflow_id, attempts_used: attempt });
      return result;
    }

    if (attempt >= max) {
      log('warn', 'escalation-exhausted', {
        workflow_id: req.workflow_id,
        attempts_used: attempt,
        final_tier: req.tier,
        final_role: req.role,
      });
      return { ...result, escalation_exhausted: true };
    }

    const escalation = result.escalation_requested;
    const wasAtCeiling = req.tier === 'T4';
    if (wasAtCeiling && req.role !== 'advisor') {
      req = { ...req, role: 'advisor' };
      log('info', 'switching-to-advisor', { workflow_id: req.workflow_id, attempt: attempt + 1, from_tier: escalation.from_tier });
    } else if (wasAtCeiling) {
      log('warn', 'advisor-also-escalated', { workflow_id: req.workflow_id, attempts_used: attempt });
      return { ...result, escalation_exhausted: true };
    } else {
      const newTier = bumpTier(req.tier as Tier);
      req = { ...req, tier: newTier };
      log('info', 'tier-bump', { workflow_id: req.workflow_id, attempt: attempt + 1, from_tier: escalation.from_tier, to_tier: newTier });
    }
    req = {
      ...req,
      escalation_context: {
        from_tier: escalation.from_tier,
        advisor_nudge: escalation.advisor_nudge,
        attempt: attempt + 1,
      },
    };
    attempt++;
  }

  return lastResult ?? ({ exit_code: -1, stdout_tail: '', stderr_tail: 'loop exited without result', duration_ms: 0 } as ActivityResult);
}

async function main(): Promise<void> {
  const { values } = parseArgs({
    options: { 'request-file': { type: 'string' } },
    strict: false,
  });

  const path = values['request-file'] as string | undefined;
  if (!path) {
    process.stderr.write('chitin-execute-request: --request-file <path> is required\n');
    process.exit(1);
  }

  const reqJson = readFileSync(path, 'utf8');
  const initial = ExecutionRequestSchema.parse(JSON.parse(reqJson));
  logLine('info', 'starting', {
    workflow_id: initial.workflow_id,
    initial_tier: initial.tier,
    initial_role: initial.role,
    max_attempts: MAX_ATTEMPTS,
  });
  const final = await runWithEscalation(initial, { runFn: runAgentTurn });
  process.stdout.write(JSON.stringify(final) + '\n');
}

// Only auto-run when invoked as a CLI. Tests import { runWithEscalation }
// from this module; without this guard, main() would fire on import and
// exit the test runner.
import { fileURLToPath } from 'node:url';
const isCli = process.argv[1] === fileURLToPath(import.meta.url);
if (isCli) {
  main().catch((err) => {
    process.stderr.write(
      JSON.stringify({ error: err instanceof Error ? err.message : String(err) }) + '\n',
    );
    process.exit(1);
  });
}
