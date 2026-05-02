import { z } from 'zod';

// Output schema for researcher candidates
export const ResearcherCandidatesSchema = z.object({
  candidates: z.array(
    z.object({
      source: z.string(),
      id: z.string(),
      summary: z.string(),
      why: z.string(),
    })
  ),
});

export type ResearcherCandidates = z.infer<typeof ResearcherCandidatesSchema>;

/**
 * Builds the prompt for the researcher agent.
 * @param source_summaries Array of {source, summary}
 * @param existing_candidate_ids Array of candidate IDs already found
 * @param since_window_hours Number of hours for the research window
 */
export function buildResearcherPrompt({
  source_summaries,
  existing_candidate_ids,
  since_window_hours,
}: {
  source_summaries: { source: string; summary: string }[];
  existing_candidate_ids: string[];
  since_window_hours: number;
}): string {
  return `
You are a research agent tasked with synthesizing new candidate entries from recent sources.

- Review the following source summaries (from the last ${since_window_hours} hours):
${source_summaries.length === 0 ? '(none)' : source_summaries.map((s, i) => `  ${i + 1}. [${s.source}] ${s.summary}`).join('\n')}

- Existing candidate IDs (do not duplicate):
${existing_candidate_ids.length === 0 ? '(none)' : existing_candidate_ids.join(', ')}

Your job:
- Propose new, non-duplicate candidates based on the above sources.
- For each, provide:
  - source: which source the candidate is from
  - id: a unique identifier for the candidate
  - summary: a concise summary of the candidate
  - why: why this is a valuable or novel candidate

Output format:
<<<CANDIDATES>>>{"candidates": [{"source": ..., "id": ..., "summary": ..., "why": ...}, ...]}
`;
}

/**
 * Parses the researcher's output and extracts the candidates JSON.
 * Returns null if parsing fails or marker not found.
 */
export function parseResearcherOutput(stdoutTail: string): ResearcherCandidates | null {
  const marker = '<<<CANDIDATES>>>';
  const idx = stdoutTail.lastIndexOf(marker);
  if (idx === -1) return null;
  const jsonStart = idx + marker.length;
  const jsonStr = stdoutTail.slice(jsonStart).trim();
  try {
    const parsed = JSON.parse(jsonStr);
    return ResearcherCandidatesSchema.parse(parsed);
  } catch {
    return null;
  }
}
