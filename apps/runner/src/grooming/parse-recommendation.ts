// Extract the json block the groomer agent emits from a workflow stdout
// tail, validate the shape, and return a typed recommendation. Tolerant of
// surrounding text — Copilot CLI output includes session preambles and
// tool-call traces; we only need the json block.

// The agent's recommendation status is distinct from BacklogEntry.status:
// the entry's status is the file's current state (ready / in_design /
// needs_human), the recommendation's status is what the entry should
// become after this grooming pass (ready / still_in_design / needs_human).
// 'still_in_design' is the explicit "I looked, it still needs more breakdown"
// outcome — separate from the file vocabulary so a misclassified read of
// 'in_design' can't slip through.
export type RecommendationStatus = 'ready' | 'still_in_design' | 'needs_human';

export interface DecompositionItem {
  id: string;
  title: string;
  tier: string;
}

export interface GroomingRecommendation {
  entryId: string;
  status: RecommendationStatus;
  tierRecommendation: 'T0' | 'T1' | 'T2' | 'T3' | 'T4';
  estimatedLoc: number;
  implementationSteps: string[];
  decomposition: DecompositionItem[];
  confidence: number;
  reasoning: string;
}

export interface ParseResult {
  ok: true;
  recommendation: GroomingRecommendation;
}

export interface ParseError {
  ok: false;
  error: string;
  rawExtract?: string;
}

export function parseRecommendation(stdout: string, expectedEntryId: string): ParseResult | ParseError {
  const block = extractJsonBlock(stdout);
  if (!block) {
    return { ok: false, error: 'no ```json block in agent output' };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(block);
  } catch (err) {
    return {
      ok: false,
      error: `json parse failed: ${err instanceof Error ? err.message : String(err)}`,
      rawExtract: block.slice(0, 400),
    };
  }
  if (!parsed || typeof parsed !== 'object') {
    return { ok: false, error: 'parsed value is not an object', rawExtract: block.slice(0, 400) };
  }
  const r = parsed as Record<string, unknown>;
  const status = readStatus(r.status);
  const tier = readTier(r.tier_recommendation);
  if (!status || !tier) {
    return {
      ok: false,
      error: `invalid status (${String(r.status)}) or tier (${String(r.tier_recommendation)})`,
      rawExtract: block.slice(0, 400),
    };
  }
  const entryId = typeof r.entry_id === 'string' ? r.entry_id : '';
  if (entryId !== expectedEntryId) {
    return {
      ok: false,
      error: `entry_id mismatch: expected '${expectedEntryId}', got '${entryId}'`,
    };
  }
  const recommendation: GroomingRecommendation = {
    entryId,
    status,
    tierRecommendation: tier,
    estimatedLoc: typeof r.estimated_loc === 'number' ? r.estimated_loc : 0,
    implementationSteps: readStringArray(r.implementation_steps),
    decomposition: readDecomposition(r.decomposition),
    confidence: typeof r.confidence === 'number' ? r.confidence : 0,
    reasoning: typeof r.reasoning === 'string' ? r.reasoning.slice(0, 280) : '',
  };
  // Contract checks the prompt enforces — sanity-check the output too so a
  // misbehaving agent's JSON can't slip into the backlog as malformed.
  if (recommendation.status === 'ready' && recommendation.implementationSteps.length === 0) {
    return { ok: false, error: 'status=ready but implementation_steps is empty' };
  }
  if (recommendation.status === 'still_in_design' && recommendation.decomposition.length < 2) {
    return { ok: false, error: 'status=still_in_design but decomposition has <2 items' };
  }
  return { ok: true, recommendation };
}

// Find the LAST ```json fenced block in stdout. Copilot-CLI may print
// multiple code blocks (some from earlier reasoning); the agent's actual
// answer is conventionally the final block.
function extractJsonBlock(stdout: string): string | null {
  const fences = stdout.matchAll(/```json\s*\n([\s\S]*?)\n```/g);
  let last: string | null = null;
  for (const m of fences) last = m[1];
  return last;
}

function readStatus(v: unknown): RecommendationStatus | null {
  if (v === 'ready' || v === 'still_in_design' || v === 'needs_human') return v;
  return null;
}

function readTier(v: unknown): GroomingRecommendation['tierRecommendation'] | null {
  if (v === 'T0' || v === 'T1' || v === 'T2' || v === 'T3' || v === 'T4') return v;
  return null;
}

function readStringArray(v: unknown): string[] {
  if (!Array.isArray(v)) return [];
  return v.filter((x): x is string => typeof x === 'string' && x.length > 0);
}

function readDecomposition(v: unknown): DecompositionItem[] {
  if (!Array.isArray(v)) return [];
  return v
    .filter((x): x is Record<string, unknown> => x !== null && typeof x === 'object')
    .map((x) => ({
      id: typeof x.id === 'string' ? x.id : '',
      title: typeof x.title === 'string' ? x.title : '',
      tier: typeof x.tier === 'string' ? x.tier : '',
    }))
    .filter((x) => x.id.length > 0 && x.title.length > 0);
}
