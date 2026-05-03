// Uncertainty marker parser — finds `<<<UNCERTAIN>>>{...json...}`
// markers in agent stdout and returns the structured payload.
//
// The marker is the agent's self-report mechanism: when an agent
// is genuinely unsure how to proceed, it emits this marker and
// gracefully exits. The router parses, calls the advisor, writes
// the nudge to shared memory, and re-dispatches the entry.
//
// Marker shape (the agent emits):
//   <<<UNCERTAIN>>>{"question": "...", "context": "...", "blocker": "..."}
//
// Where:
//   question — what the agent needs help with (one specific ask)
//   context — what the agent already knows / has tried
//   blocker — why it can't proceed without the nudge

export interface UncertaintyMarker {
  question: string;
  context: string;
  blocker?: string;
}

const MARKER_RE = /<<<UNCERTAIN>>>(\{[\s\S]*?\})/;

/**
 * Pure: extract the LAST uncertainty marker from agent stdout.
 * Returns null if no marker is found OR if the JSON is malformed
 * (malformed markers are logged at the caller; no exception thrown
 * because a malformed marker shouldn't break dispatch).
 */
export function parseUncertaintyMarker(stdout: string): UncertaintyMarker | null {
  if (!stdout) return null;
  const matches = [...stdout.matchAll(new RegExp(MARKER_RE, 'g'))];
  if (matches.length === 0) return null;
  const last = matches[matches.length - 1];
  try {
    const parsed = JSON.parse(last[1]) as Record<string, unknown>;
    if (typeof parsed.question !== 'string' || typeof parsed.context !== 'string') {
      return null;
    }
    return {
      question: parsed.question,
      context: parsed.context,
      blocker: typeof parsed.blocker === 'string' ? parsed.blocker : undefined,
    };
  } catch {
    return null;
  }
}

/**
 * Boilerplate prompt instructions for any role that should be
 * uncertainty-marker-aware. Render into the role's prompt at the
 * end of the workflow section. Tells the agent WHEN + HOW to emit
 * the marker.
 */
export const UNCERTAINTY_MARKER_INSTRUCTIONS = `
## Uncertainty escalation (router protocol)

If you reach a point where you GENUINELY don't know how to proceed
— not "this is hard" but "I cannot decide between two paths because
I'm missing information / context / domain knowledge" — emit this
marker and exit cleanly:

\`\`\`
<<<UNCERTAIN>>>{"question": "<one specific ask>", "context": "<what you already know + tried>", "blocker": "<why you can't proceed>"}
\`\`\`

Then exit with exit code 0. The router will call a higher-tier
advisor with your question + context, write the advisor's
response to shared memory, and re-dispatch you. On the re-dispatch
you'll see the advisor's nudge in your prompt under "ROUTER SHARED
MEMORY" — read it, then continue from where you left off.

DO NOT emit this marker for routine difficulties (debugging,
reading docs, trying multiple approaches). It's for genuine
blockers where the next step requires judgment you don't have.

DO NOT take any tool actions after emitting the marker — the
router treats the marker as a graceful exit signal.
`.trim();
