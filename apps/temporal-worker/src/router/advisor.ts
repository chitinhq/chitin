// Advisor primitive — wraps `claude -p` (subscription-billed
// headless mode) to produce structured advice for the router's
// hook wrapper.
//
// Constraint: NO metered API calls. We use the Claude Code CLI's
// `claude -p "prompt"` interface which authenticates against the
// user's Claude Code subscription (Pro plan). Per the Anthropic
// AUP clarification (2026-05-02), headless `claude -p` is
// supported for automation.
//
// MVP: single advisor call per invocation. Chain depth is managed
// by the hook wrapper, which can call this multiple times with
// incrementing chain_depth.

import { spawn } from 'node:child_process';
import type {
  AdvisorRequest,
  AdvisorResponse,
  HeuristicOutcome,
} from './types.ts';

/** Build the advisor prompt from a structured request. */
export function buildAdvisorPrompt(request: AdvisorRequest): string {
  const heuristic_summary = renderHeuristicSummary(request.heuristic_outcome);
  return `You are a chitin ROUTER ADVISOR — called when a lower-tier agent's tool call triggered a heuristic threshold.

Your job: decide whether the agent should proceed, with what nudge, OR whether the action should be blocked / escalated to a higher-tier human takeover.

PROPOSED ACTION:
\`\`\`json
${JSON.stringify(request.proposed_action, null, 2)}
\`\`\`

HEURISTIC OUTCOME (which signals fired):
${heuristic_summary}

CALLER CONTEXT:
- caller_tier: ${request.caller_tier ?? 'unknown'}
- chain_depth: ${request.chain_depth} (max chain_depth before takeover: typically 3)

QUESTION:
${request.question}

CONTEXT:
${request.context}

YOUR OUTPUT — emit EXACTLY this JSON line as your final output (no other text after it):

\`\`\`
<<<ROUTER_ADVISOR>>>{"nudge": "<short text shown to the agent>", "verdict": "continue"|"takeover", "escalate": <bool>}
\`\`\`

Where:
- nudge: short specific text (1-3 sentences) the agent will see in its tool result. Focus on the SPECIFIC concern that fired the heuristic. Don't lecture; nudge.
- verdict: "continue" lets the action proceed (with nudge attached); "takeover" blocks it and signals re-dispatch at higher tier.
- escalate: true if you think a higher-tier advisor should be consulted INSTEAD of you (chain to T+1). Use sparingly — only for cases where you don't have the domain knowledge.

Examples of good nudges:
- "Consider running tests before this commit — the diff touches 12 files."
- "This is a force-push on a branch with open PRs (#142, #143). Verify intent."
- "You've tried this same edit 3 times with the same syntax error. The actual issue is X."

Examples of bad nudges:
- "Be careful." (too vague)
- "Don't do this." (no reasoning)
- A multi-paragraph explanation (too verbose for an inline tool message)
`;
}

function renderHeuristicSummary(outcome: HeuristicOutcome): string {
  const parts: string[] = [];
  if (outcome.blast_radius) {
    parts.push(
      `- blast_radius: ${outcome.blast_radius.score} (${outcome.blast_radius.reason})${outcome.blast_radius.fired ? ' [FIRED]' : ''}`,
    );
  }
  if (outcome.drift) {
    parts.push(
      `- drift: ${outcome.drift.score} (${outcome.drift.reason})${outcome.drift.fired ? ' [FIRED]' : ''}`,
    );
  }
  if (outcome.floundering) {
    parts.push(
      `- floundering: ${outcome.floundering.score} (${outcome.floundering.reason})${outcome.floundering.fired ? ' [FIRED]' : ''}`,
    );
  }
  return parts.length > 0 ? parts.join('\n') : '(no heuristic ran — caller invoked advisor directly)';
}

/** Pure: parse the advisor's structured output marker. */
export function parseAdvisorOutput(stdout: string): AdvisorResponse | null {
  const marker = /<<<ROUTER_ADVISOR>>>(\{[\s\S]*?\})/;
  const matches = [...stdout.matchAll(new RegExp(marker, 'g'))];
  if (matches.length === 0) return null;
  const last = matches[matches.length - 1];
  try {
    const parsed = JSON.parse(last[1]) as Record<string, unknown>;
    if (typeof parsed.nudge !== 'string' || typeof parsed.verdict !== 'string') {
      return null;
    }
    if (parsed.verdict !== 'continue' && parsed.verdict !== 'takeover') {
      return null;
    }
    return {
      nudge: parsed.nudge,
      verdict: parsed.verdict,
      escalate: typeof parsed.escalate === 'boolean' ? parsed.escalate : false,
    };
  } catch {
    return null;
  }
}

/**
 * Run the advisor. I/O: spawns `claude -p` with the prompt,
 * waits for completion (bounded by timeout), parses the output
 * marker. Returns null on any failure (timeout, parse error,
 * binary missing) — caller decides fallback (typically: pass-
 * through to deterministic kernel verdict).
 */
export async function callAdvisor(
  request: AdvisorRequest,
  opts: { timeoutMs?: number; binary?: string } = {},
): Promise<AdvisorResponse | null> {
  const timeoutMs = opts.timeoutMs ?? 60_000;
  const binary = opts.binary ?? 'claude';
  const prompt = buildAdvisorPrompt(request);

  return new Promise((resolve) => {
    let resolved = false;
    const finish = (val: AdvisorResponse | null): void => {
      if (resolved) return;
      resolved = true;
      resolve(val);
    };

    const child = spawn(
      binary,
      ['-p', '--dangerously-skip-permissions', '--output-format', 'text'],
      { stdio: ['pipe', 'pipe', 'pipe'] },
    );
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (d: Buffer) => (stdout += d.toString('utf8')));
    child.stderr.on('data', (d: Buffer) => (stderr += d.toString('utf8')));

    const timer = setTimeout(() => {
      child.kill('SIGKILL');
      // Log the timeout to stderr; caller's logging happens upstream
      console.error(
        JSON.stringify({
          ts: new Date().toISOString(),
          level: 'warn',
          component: 'router-advisor',
          msg: 'advisor-timeout',
          timeoutMs,
        }),
      );
      finish(null);
    }, timeoutMs);

    child.on('error', () => {
      clearTimeout(timer);
      console.error(
        JSON.stringify({
          ts: new Date().toISOString(),
          level: 'warn',
          component: 'router-advisor',
          msg: 'advisor-spawn-error',
          stderr: stderr.slice(0, 500),
        }),
      );
      finish(null);
    });
    child.on('close', () => {
      clearTimeout(timer);
      finish(parseAdvisorOutput(stdout));
    });

    // Send the prompt over stdin
    child.stdin.write(prompt);
    child.stdin.end();
  });
}
