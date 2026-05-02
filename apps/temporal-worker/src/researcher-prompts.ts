// Researcher-role prompt template + structured-output parser. Mirrors
// the pattern reviewer-prompts.ts established in PR #134:
//   - tier-aware prompt builder
//   - <<<CANDIDATES>>>-marked structured emit
//   - discriminated-union parser ({ ok, output | error })
//
// Used by:
//   - the role-prompts.ts registry's `researcher` entry (BacklogEntry-
//     level dispatch when a backlog entry has `role: researcher`)
//   - the future external-signal-fetchers runner, which calls
//     buildResearcherPrompt directly with richer context (source
//     summaries, existing candidate ids, since-window) than a single
//     BacklogEntry can carry.
//
// See docs/design/2026-05-02-swarm-as-software-factory.md §3 for what
// the researcher role owns.

import { z } from 'zod';

// ─── Zod schema for structured output ──────────────────────────────────────

export const ResearcherCandidateSchema = z.object({
  /** Which source the candidate came from — arxiv / reddit / hn / openclaw / ollama / etc. */
  source: z.string().min(1),
  /** Stable id for dedup against existing roadmap entries. Typically
   *  the source's native id (arxiv id, reddit post id, gh release tag). */
  id: z.string().min(1),
  /** One-sentence summary the operator can grep through. */
  summary: z.string().min(1),
  /** Why this is worth surfacing as a candidate — the load-bearing
   *  reasoning step. Empty/generic "why" rows feel like noise. */
  why: z.string().min(1),
});

export const ResearcherCandidatesSchema = z.object({
  candidates: z.array(ResearcherCandidateSchema),
});

export type ResearcherCandidate = z.infer<typeof ResearcherCandidateSchema>;
export type ResearcherCandidates = z.infer<typeof ResearcherCandidatesSchema>;

// ─── Prompt builder ────────────────────────────────────────────────────────

export interface ResearcherPromptInputs {
  /** One row per source the runner pulled. Newline-separated body of
   *  recent items the agent should read — title + URL + a few-line
   *  excerpt. */
  source_summaries: { source: string; summary: string }[];
  /** Candidate ids already in roadmap.md's "Candidates from external
   *  signal" section. The agent must NOT propose duplicates. */
  existing_candidate_ids: string[];
  /** Look-back window the runner used. Surfaces in the prompt so the
   *  agent knows recency semantics. */
  since_window_hours: number;
}

/**
 * Structured-output instructions appended to any researcher prompt
 * (runner-level via buildResearcherPrompt, backlog-entry-level via
 * role-prompts.ts). Exported so the entry-level adapter doesn't drift
 * away from the runner-level marker contract.
 */
export const RESEARCHER_OUTPUT_INSTRUCTIONS = `\
At the END of your synthesis, emit EXACTLY ONE JSON object on a single line, prefixed with the literal token \`<<<CANDIDATES>>>\` and nothing else after the closing brace on that line. No code fence, no commentary. The runner's parser keys on \`<<<CANDIDATES>>>\`. Example:

<<<CANDIDATES>>>{"candidates":[{"source":"arxiv","id":"2511.13646v3","summary":"Live-SWE-agent v3: tool-registry pattern + benchmark loop.","why":"Closest external prior art for chitin's role-typed dispatcher; v3 adds the benchmark replay we don't yet have."}]}

If you find no new non-duplicate candidates worth surfacing, emit \`<<<CANDIDATES>>>{"candidates":[]}\` — empty list is fine. Do not invent low-signal candidates to fill the cap. The roadmap reader's time is more valuable than the per-candidate cost.`;

/**
 * Build the researcher's prompt. Takes source summaries the runner
 * pre-fetched + existing candidate ids the agent must not duplicate +
 * the look-back window for recency framing.
 */
export function buildResearcherPrompt(opts: ResearcherPromptInputs): string {
  const sourceLines =
    opts.source_summaries.length === 0
      ? '(no source summaries — runner pulled an empty window; emit empty candidates list)'
      : opts.source_summaries
          .map((s, i) => `  ${i + 1}. [${s.source}] ${s.summary}`)
          .join('\n');

  const existingLine =
    opts.existing_candidate_ids.length === 0
      ? '(roadmap.md has no candidate ids yet)'
      : opts.existing_candidate_ids.join(', ');

  return `You are the researcher-role agent for chitin's autonomous swarm. Your job: read the source summaries below (recent activity from arxiv / Reddit / HN / openclaw / ollama / etc.) and propose new candidate entries for \`docs/roadmap.md\`'s "Candidates from external signal" section.

Source summaries (last ${opts.since_window_hours} hours):
${sourceLines}

Existing candidate ids in roadmap.md (do not duplicate):
${existingLine}

Synthesis rules:
- One candidate per genuinely-new finding. Don't over-batch related items into one row; don't fragment one finding into multiple rows.
- The "why" field is load-bearing — spend the words on chitin-specific implications (e.g., "extends our role-typed dispatcher", "alternative to claude-code-headless"), not generic novelty.
- Skip items that look like restatements of existing chitin work. Skip items that are pure marketing.
- If you can't tell whether something matters from the summary, skip it. The roadmap reader trusts you to filter — false-positives are worse than false-negatives.

${RESEARCHER_OUTPUT_INSTRUCTIONS}`;
}

// ─── Output parser ─────────────────────────────────────────────────────────

const CANDIDATES_MARKER = '<<<CANDIDATES>>>';

/**
 * Extract the researcher's structured candidate list from agent
 * stdout. Mirrors `parseReviewerOutput` from reviewer-prompts.ts:
 * last-marker-wins (so a prompt example the agent echoes doesn't
 * false-match), one-line slice (so trailing chatter doesn't break
 * JSON.parse), strict zod validation.
 *
 * Returns:
 *   { ok: true, output }   — well-formed
 *   { ok: false, error }   — missing marker / parse failure / schema
 *                              validation failure (with candidate slice
 *                              for debugging)
 */
export function parseResearcherOutput(
  stdoutTail: string,
): { ok: true; output: ResearcherCandidates } | { ok: false; error: string } {
  const lastMarker = stdoutTail.lastIndexOf(CANDIDATES_MARKER);
  if (lastMarker < 0) {
    return { ok: false, error: 'no <<<CANDIDATES>>> marker in stdout' };
  }
  const after = stdoutTail.slice(lastMarker + CANDIDATES_MARKER.length);
  // Take only the first line — agents sometimes emit trailing chatter
  // despite instructions; including it would fail JSON.parse on
  // otherwise-valid output.
  const lineEnd = after.indexOf('\n');
  const candidate = (lineEnd >= 0 ? after.slice(0, lineEnd) : after).trim();
  if (!candidate.startsWith('{')) {
    return { ok: false, error: `expected JSON object after marker, got: ${truncate(candidate, 200)}` };
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(candidate);
  } catch (err) {
    return {
      ok: false,
      error:
        `JSON.parse failed: ${err instanceof Error ? err.message : String(err)} ` +
        `(candidate: ${truncate(candidate, 200)})`,
    };
  }

  const result = ResearcherCandidatesSchema.safeParse(parsed);
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
  CANDIDATES_MARKER,
};
