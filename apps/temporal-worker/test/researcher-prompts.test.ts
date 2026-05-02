import { describe, expect, it } from 'vitest';
import {
  ResearcherCandidateSchema,
  ResearcherCandidatesSchema,
  RESEARCHER_OUTPUT_INSTRUCTIONS,
  buildResearcherPrompt,
  parseResearcherOutput,
  __test__,
  type ResearcherPromptInputs,
} from '../src/researcher-prompts.ts';

const { CANDIDATES_MARKER } = __test__;

const baseInputs: ResearcherPromptInputs = {
  source_summaries: [
    { source: 'arxiv', summary: '2511.13646v3 — Live-SWE-agent v3 adds tool-registry pattern.' },
    { source: 'reddit', summary: 'r/LocalLLaMA — qwen3-coder-30b benchmarks vs deepseek-v3.' },
  ],
  existing_candidate_ids: ['arxiv:2511.13646v2', 'gh-release:openclaw-v0.4'],
  since_window_hours: 24,
};

// ─── Schema validation ─────────────────────────────────────────────────────

describe('ResearcherCandidateSchema', () => {
  it('accepts a complete candidate', () => {
    const ok = {
      source: 'arxiv',
      id: '2511.13646v3',
      summary: 'Live-SWE-agent v3: tool-registry pattern + benchmark loop.',
      why: 'Closest external prior art for chitin role-typed dispatcher.',
    };
    expect(() => ResearcherCandidateSchema.parse(ok)).not.toThrow();
  });

  it('rejects empty source', () => {
    const bad = { source: '', id: 'x', summary: 's', why: 'w' };
    expect(() => ResearcherCandidateSchema.parse(bad)).toThrow();
  });

  it('rejects empty id', () => {
    const bad = { source: 'arxiv', id: '', summary: 's', why: 'w' };
    expect(() => ResearcherCandidateSchema.parse(bad)).toThrow();
  });

  it('rejects empty summary', () => {
    const bad = { source: 'arxiv', id: 'x', summary: '', why: 'w' };
    expect(() => ResearcherCandidateSchema.parse(bad)).toThrow();
  });

  it('rejects empty why (load-bearing field, must not be blank)', () => {
    const bad = { source: 'arxiv', id: 'x', summary: 's', why: '' };
    expect(() => ResearcherCandidateSchema.parse(bad)).toThrow();
  });

  it('rejects missing why entirely', () => {
    const bad = { source: 'arxiv', id: 'x', summary: 's' };
    expect(() => ResearcherCandidateSchema.parse(bad)).toThrow();
  });
});

describe('ResearcherCandidatesSchema', () => {
  it('accepts an empty list (no findings is a valid output)', () => {
    expect(() => ResearcherCandidatesSchema.parse({ candidates: [] })).not.toThrow();
  });

  it('accepts multiple candidates', () => {
    const ok = {
      candidates: [
        { source: 'arxiv', id: 'x', summary: 's1', why: 'w1' },
        { source: 'reddit', id: 'y', summary: 's2', why: 'w2' },
      ],
    };
    expect(() => ResearcherCandidatesSchema.parse(ok)).not.toThrow();
  });

  it('rejects when candidates is not an array', () => {
    const bad = { candidates: 'not-an-array' };
    expect(() => ResearcherCandidatesSchema.parse(bad)).toThrow();
  });

  it('rejects when an inner candidate is malformed (validation cascades)', () => {
    const bad = { candidates: [{ source: 'arxiv', id: 'x', summary: 's' /* missing why */ }] };
    expect(() => ResearcherCandidatesSchema.parse(bad)).toThrow();
  });
});

// ─── parseResearcherOutput ────────────────────────────────────────────────

describe('parseResearcherOutput', () => {
  it('extracts a well-formed payload after the marker', () => {
    const tail = `Some chatter from the agent.\n${CANDIDATES_MARKER}{"candidates":[{"source":"arxiv","id":"2511.13646v3","summary":"Live-SWE-agent v3.","why":"Prior art for role-typed dispatch."}]}`;
    const r = parseResearcherOutput(tail);
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.output.candidates).toHaveLength(1);
      expect(r.output.candidates[0].id).toBe('2511.13646v3');
    }
  });

  it('accepts an empty candidate list (the explicit no-finding case)', () => {
    const tail = `${CANDIDATES_MARKER}{"candidates":[]}`;
    const r = parseResearcherOutput(tail);
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.output.candidates).toHaveLength(0);
  });

  it('takes the LAST marker when multiple are present (echoed prompt example vs real output)', () => {
    const tail = `Example from prompt: ${CANDIDATES_MARKER}{"candidates":[{"source":"arxiv","id":"example","summary":"s","why":"w"}]}\nReal output:\n${CANDIDATES_MARKER}{"candidates":[{"source":"reddit","id":"real","summary":"s","why":"w"}]}`;
    const r = parseResearcherOutput(tail);
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.output.candidates).toHaveLength(1);
      expect(r.output.candidates[0].id).toBe('real');
    }
  });

  it('returns ok:false when the marker is missing', () => {
    const r = parseResearcherOutput('agent forgot to emit structured output');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain('marker');
  });

  it('returns ok:false on malformed JSON after the marker', () => {
    const r = parseResearcherOutput(`${CANDIDATES_MARKER}{"candidates":[,]}`); // bad comma
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain('JSON.parse');
  });

  it('returns ok:false when JSON is valid but schema rejects (missing required field)', () => {
    const r = parseResearcherOutput(
      `${CANDIDATES_MARKER}{"candidates":[{"source":"arxiv","id":"x","summary":"s"}]}`,
    );
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain('schema');
  });

  it('returns ok:false when post-marker content does not start with {', () => {
    const r = parseResearcherOutput(`${CANDIDATES_MARKER}sorry no candidates today`);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain('expected JSON object');
  });

  it('handles trailing newline + content after the JSON line gracefully (takes only the line)', () => {
    const tail = `${CANDIDATES_MARKER}{"candidates":[]}\nOh and one more thing...`;
    const r = parseResearcherOutput(tail);
    expect(r.ok).toBe(true);
  });

  it('rejects malformed JSON when trailing chatter is on the same line', () => {
    // Same-line trailing content invalidates the JSON parse — this is
    // expected behavior (the prompt mandates "nothing else after the
    // closing brace on that line"). The next line is fair game.
    const tail = `${CANDIDATES_MARKER}{"candidates":[]} oops trailing chatter`;
    const r = parseResearcherOutput(tail);
    expect(r.ok).toBe(false);
  });
});

// ─── buildResearcherPrompt ────────────────────────────────────────────────

describe('buildResearcherPrompt', () => {
  it('produces a non-empty prompt', () => {
    const out = buildResearcherPrompt(baseInputs);
    expect(out.length).toBeGreaterThan(500);
  });

  it('includes the source summaries verbatim in the body', () => {
    const out = buildResearcherPrompt(baseInputs);
    expect(out).toContain('Live-SWE-agent');
    expect(out).toContain('qwen3-coder');
    expect(out).toContain('[arxiv]');
    expect(out).toContain('[reddit]');
  });

  it('includes the existing-candidate ids so the agent can dedup', () => {
    const out = buildResearcherPrompt(baseInputs);
    expect(out).toContain('arxiv:2511.13646v2');
    expect(out).toContain('gh-release:openclaw-v0.4');
    expect(out).toContain('do not duplicate');
  });

  it('includes the since-window in the prompt for recency framing', () => {
    const out = buildResearcherPrompt({ ...baseInputs, since_window_hours: 48 });
    expect(out).toContain('48 hours');
  });

  it('includes the structured-output marker so the agent knows to emit it', () => {
    const out = buildResearcherPrompt(baseInputs);
    expect(out).toContain('<<<CANDIDATES>>>');
  });

  it('embeds the synthesis rules (load-bearing why, no fragmentation, skip restatements)', () => {
    const out = buildResearcherPrompt(baseInputs);
    expect(out).toContain('load-bearing');
    expect(out).toContain("Don't over-batch");
    expect(out).toContain('restatements');
  });

  it('handles empty source_summaries gracefully (degrades to "emit empty list")', () => {
    const out = buildResearcherPrompt({ ...baseInputs, source_summaries: [] });
    expect(out).toContain('no source summaries');
    expect(out).toContain('emit empty candidates list');
  });

  it('handles empty existing_candidate_ids gracefully', () => {
    const out = buildResearcherPrompt({ ...baseInputs, existing_candidate_ids: [] });
    expect(out).toContain('no candidate ids yet');
  });
});

describe('RESEARCHER_OUTPUT_INSTRUCTIONS', () => {
  it('mentions the marker (so the entry-level adapter inherits the same contract)', () => {
    expect(RESEARCHER_OUTPUT_INSTRUCTIONS).toContain('<<<CANDIDATES>>>');
  });

  it('shows the empty-list fallback (so agents know what to emit on a quiet window)', () => {
    expect(RESEARCHER_OUTPUT_INSTRUCTIONS).toContain('"candidates":[]');
  });
});

// ─── End-to-end: build → mock agent output → parse round-trips ────────────

describe('end-to-end prompt → parse', () => {
  it('a well-formed agent response (matching the prompt example) parses cleanly', () => {
    const prompt = buildResearcherPrompt(baseInputs);
    expect(prompt).toContain(CANDIDATES_MARKER);

    // Mock what an obedient agent would emit
    const mockTail = `Step-by-step research:
- read arxiv abstract for 2511.13646v3
- read the reddit thread, weighted comment quality
- decided the qwen3-coder benchmark is restating known territory; skipped
${CANDIDATES_MARKER}{"candidates":[{"source":"arxiv","id":"2511.13646v3","summary":"Live-SWE-agent v3 adds a tool-registry pattern + benchmark loop on SWE-bench Live.","why":"Closest external prior art for chitin's role-typed dispatcher; v3's benchmark replay is something we don't have yet — worth a candidate row."}]}`;

    const result = parseResearcherOutput(mockTail);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.output.candidates).toHaveLength(1);
      expect(result.output.candidates[0].source).toBe('arxiv');
      expect(result.output.candidates[0].id).toBe('2511.13646v3');
    }
  });

  it('an obedient empty-window agent response parses to an empty list', () => {
    const mockTail = `Nothing new in the window.\n${CANDIDATES_MARKER}{"candidates":[]}`;
    const result = parseResearcherOutput(mockTail);
    expect(result.ok).toBe(true);
    if (result.ok) expect(result.output.candidates).toHaveLength(0);
  });
});
