// Router hook wrapper — main entry point invoked by Claude Code's
// PreToolUse hook (via the `chitin-router-hook` bin shim).
//
// Pipeline:
//   stdin (Claude Code PreToolUse JSON)
//     ↓
//   step 1: kernel verdict (chitin-kernel gate evaluate --hook-stdin)
//     ↓ if deny → return deny immediately
//   step 2: heuristics (blast-radius + floundering)
//     ↓ if none fired → return kernel verdict (allow)
//   step 3: call advisor with structured request
//     ↓ if advisor signals escalate → call next-tier advisor (chain)
//     ↓ chain bounded by policy.advisor.chain.max_depth
//   step 4: compose final response — kernel verdict + advisor nudge
//     ↓
//   stdout (Claude Code hook response: { decision, message })
//
// Constraints:
//   - kernel stays deterministic (this wrapper is the LLM-mediated layer)
//   - all advisor calls go through `claude -p` (sub-billed, no API)
//   - if anything fails (kernel binary missing, advisor times out),
//     we FAIL OPEN to the kernel verdict — never harden a fallback
//     deny that could brick the agent

import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, readSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { callAdvisor } from './advisor.ts';
import { scoreBlastRadius } from './heuristics/blast-radius.ts';
import { detectFloundering, type ChainEventLite } from './heuristics/floundering.ts';
import { loadRouterPolicy } from './policy-loader.ts';
import { appendSharedMemory, readSharedMemory } from './shared-memory.ts';
import type {
  HeuristicOutcome,
  HookInput,
  HookOutput,
  RouterPolicy,
} from './types.ts';

/** Read all of stdin as a string. */
function readStdin(): string {
  const buf = Buffer.alloc(65536);
  let total = '';
  let bytesRead = 0;
  try {
    while ((bytesRead = readSync(0, buf, 0, buf.length, null)) > 0) {
      total += buf.subarray(0, bytesRead).toString('utf8');
    }
  } catch {
    // EAGAIN on a non-blocking fd or EOF — ignore
  }
  return total;
}

/** Pure: Claude Code allow/deny decision protocol. */
function emitHookResponse(out: HookOutput): void {
  // Claude Code hook protocol: print JSON on stdout for "advanced"
  // shape; or exit 0 for allow (silent), exit non-zero for deny.
  // We use the JSON shape so we can attach a message.
  console.log(JSON.stringify(out));
  process.exit(0);
}

/** Call the kernel hook (deterministic verdict). Returns the parsed
 *  hook output OR null if the kernel binary isn't available. */
function callKernel(payload: string, agent: string): HookOutput | null {
  const result = spawnSync(
    'chitin-kernel',
    ['gate', 'evaluate', '--hook-stdin', `--agent=${agent}`],
    {
      input: payload,
      encoding: 'utf8',
      timeout: 5000,
    },
  );
  if (result.error || result.status === null) {
    // Binary missing or hung — fail open (allow); operator sees
    // the warning in stderr.
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'warn',
        component: 'router-hook',
        msg: 'kernel-call-failed',
        error: result.error?.message,
        status: result.status,
      }),
    );
    return null;
  }
  // Kernel exit 0 + empty stdout = silent allow; non-zero + JSON = deny
  if (result.status === 0 && !result.stdout.trim()) {
    return { decision: 'allow', source: 'kernel-allow' };
  }
  // Kernel emits JSON to stdout when it has something to say
  try {
    const parsed = JSON.parse(result.stdout) as Record<string, unknown>;
    // Kernel's shape: { decision: { Allowed: bool, Mode: ..., Reason: ... } } or similar
    // Map into hook output shape
    if (typeof parsed.decision === 'object' && parsed.decision) {
      const d = parsed.decision as Record<string, unknown>;
      return {
        decision: d.Allowed === true ? 'allow' : 'deny',
        message: typeof d.Reason === 'string' ? d.Reason : undefined,
        source: 'kernel',
      };
    }
    // Fallback: top-level allow/deny
    if (parsed.decision === 'allow' || parsed.decision === 'deny') {
      return {
        decision: parsed.decision,
        message: typeof parsed.message === 'string' ? parsed.message : undefined,
        source: 'kernel',
      };
    }
  } catch {
    // Kernel returned non-JSON (or note like "no_policy_found"); fail open
  }
  return { decision: 'allow', source: 'kernel-allow' };
}

/** Read recent chain events for the agent's session — input to floundering. */
function readChainEvents(sessionId: string | undefined): ChainEventLite[] {
  if (!sessionId) return [];
  // Chain events live at ~/.chitin/events-<chain_id>.jsonl. The
  // session_id maps to chain_id (1:1 today).
  const path = join(homedir(), '.chitin', `events-${sessionId}.jsonl`);
  if (!existsSync(path)) return [];
  const events: ChainEventLite[] = [];
  for (const line of readFileSync(path, 'utf8').split('\n')) {
    if (!line.trim()) continue;
    try {
      events.push(JSON.parse(line) as ChainEventLite);
    } catch {
      /* skip malformed line */
    }
  }
  return events;
}

/** Run heuristics per policy. */
function runHeuristics(input: HookInput, policy: RouterPolicy): HeuristicOutcome {
  const outcome: HeuristicOutcome = { any_fired: false };
  if (policy.heuristics.blast_radius?.enabled) {
    outcome.blast_radius = scoreBlastRadius(input, policy.heuristics.blast_radius.threshold);
    if (outcome.blast_radius.fired) outcome.any_fired = true;
  }
  if (policy.heuristics.floundering?.enabled) {
    const events = readChainEvents(input.session_id);
    outcome.floundering = detectFloundering(events, {
      max_loop_count: policy.heuristics.floundering.max_loop_count,
      max_stall_seconds: policy.heuristics.floundering.max_stall_seconds,
    });
    if (outcome.floundering.fired) outcome.any_fired = true;
  }
  return outcome;
}

/** Should the advisor be invoked? Maps policy.advisor.when to outcome flags. */
function shouldCallAdvisor(
  outcome: HeuristicOutcome,
  kernelDeny: boolean,
  policy: RouterPolicy,
): boolean {
  if (!policy.advisor.enabled) return false;
  for (const trigger of policy.advisor.when) {
    if (trigger === 'blast_radius_above_threshold' && outcome.blast_radius?.fired) return true;
    if (trigger === 'drift_detected' && outcome.drift?.fired) return true;
    if (trigger === 'floundering_detected' && outcome.floundering?.fired) return true;
    if (trigger === 'kernel_denied' && kernelDeny) return true;
  }
  return false;
}

/** Main entry — parse stdin, run pipeline, emit response. */
export async function main(): Promise<void> {
  // Parse args for --agent=X
  let agent = 'claude-code';
  for (const arg of process.argv.slice(2)) {
    if (arg.startsWith('--agent=')) agent = arg.slice('--agent='.length);
  }

  const stdin = readStdin();
  if (!stdin) {
    // No payload — fail open (allow) so an empty hook call doesn't brick the agent
    return emitHookResponse({ decision: 'allow', source: 'kernel-allow' });
  }
  let input: HookInput;
  try {
    input = JSON.parse(stdin) as HookInput;
  } catch {
    return emitHookResponse({ decision: 'allow', source: 'kernel-allow' });
  }

  const cwd = input.cwd ?? process.cwd();
  const policy = loadRouterPolicy(cwd);

  // Step 1: kernel verdict (deterministic)
  const kernel = callKernel(stdin, agent) ?? { decision: 'allow' as const, source: 'kernel-allow' as const };

  // If router is policy-disabled, return kernel verdict directly
  if (!policy.enabled) {
    return emitHookResponse(kernel);
  }

  // Step 2: heuristics
  const outcome = runHeuristics(input, policy);

  // If no heuristic fired AND kernel allowed → silent pass-through
  if (!outcome.any_fired && kernel.decision === 'allow') {
    return emitHookResponse(kernel);
  }

  // Step 3: advisor (if configured to fire)
  const wantAdvisor = shouldCallAdvisor(outcome, kernel.decision === 'deny', policy);
  if (!wantAdvisor) {
    // Heuristic fired but advisor not configured for this trigger — log, pass through
    if (outcome.any_fired) {
      console.error(
        JSON.stringify({
          ts: new Date().toISOString(),
          level: 'info',
          component: 'router-hook',
          msg: 'heuristic-fired-no-advisor',
          outcome,
        }),
      );
    }
    return emitHookResponse(kernel);
  }

  const memory = readSharedMemory(input.session_id ?? 'unknown');
  const advisorReq = {
    question:
      kernel.decision === 'deny'
        ? `The kernel denied this action: ${kernel.message ?? '(no reason)'}. Should the agent be re-routed or is this denial correct?`
        : `Heuristic flagged this action. Is the agent on track, or should it pause/escalate?`,
    context: `Session: ${input.session_id ?? 'unknown'}. Memory entries: ${memory.entries.length}.`,
    proposed_action: input,
    heuristic_outcome: outcome,
    chain_depth: 0,
  };

  const advice = await callAdvisor(advisorReq, { timeoutMs: 60_000 });

  if (!advice) {
    // Advisor failed — fall through to kernel verdict
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'warn',
        component: 'router-hook',
        msg: 'advisor-failed-falling-through-to-kernel',
        kernel_decision: kernel.decision,
      }),
    );
    return emitHookResponse(kernel);
  }

  // Persist the advice in shared memory so the agent's next turn
  // (or post-completion analysis) can read it.
  if (input.session_id) {
    appendSharedMemory(input.session_id, {
      source: 'router-advisor',
      payload: { ...advice, heuristic_outcome: outcome },
    });
  }

  // Step 4: compose. If advisor verdict=takeover → deny + nudge.
  // Otherwise allow + nudge attached.
  if (advice.verdict === 'takeover') {
    return emitHookResponse({
      decision: 'deny',
      message: advice.nudge,
      source: 'advisor-deny',
    });
  }
  // continue with kernel decision; attach nudge as message
  return emitHookResponse({
    decision: kernel.decision,
    message: advice.nudge + (kernel.message ? `\n\nKernel: ${kernel.message}` : ''),
    source: 'advisor-allow',
  });
}

// Run if invoked as the entrypoint
import { fileURLToPath } from 'node:url';
const isCli =
  import.meta.url.startsWith('file:') &&
  process.argv[1] &&
  fileURLToPath(import.meta.url) === process.argv[1];
if (isCli) {
  main().catch((err: unknown) => {
    console.error(
      JSON.stringify({
        ts: new Date().toISOString(),
        level: 'error',
        component: 'router-hook',
        msg: 'fatal',
        error: err instanceof Error ? err.message : String(err),
      }),
    );
    // Fail open
    console.log(JSON.stringify({ decision: 'allow', source: 'kernel-allow' }));
    process.exit(0);
  });
}
