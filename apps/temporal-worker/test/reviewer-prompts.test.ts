import { describe, expect, it } from 'vitest';
import {
  ReviewerOutputSchema,
  ReviewerFindingSchema,
  buildAdversarialReviewerPrompt,
  parseReviewerOutput,
  __test__,
  type ReviewerPromptInputs,
} from '../src/reviewer-prompts.ts';

const { REVIEW_MARKER, TIER_TONE, STRUCTURED_OUTPUT_INSTRUCTIONS } = __test__;

const baseInputs: ReviewerPromptInputs = {
  tier: 'R3',
  pr_number: 132,
  pr_url: 'https://github.com/chitinhq/chitin/pull/132',
  entry_id: 'review-graph-executor',
};

// ─── Schema validation ───────────────────────────────────────────────────

describe('ReviewerFindingSchema', () => {
  it('accepts a complete finding', () => {
    const ok = {
      severity: '🔴',
      file: 'apps/temporal-worker/src/dispatcher.ts',
      line: 42,
      category: 'bug',
      summary: 'something is broken',
      suggested_fix: 'fix it like this',
    };
    expect(() => ReviewerFindingSchema.parse(ok)).not.toThrow();
  });

  it('accepts a finding without optional line/suggested_fix', () => {
    const ok = {
      severity: '🟡',
      file: 'README.md',
      category: 'doc',
      summary: 'typo',
    };
    expect(() => ReviewerFindingSchema.parse(ok)).not.toThrow();
  });

  it('rejects an unknown severity emoji', () => {
    const bad = { severity: '🔵', file: 'x.ts', category: 'bug', summary: 's' };
    expect(() => ReviewerFindingSchema.parse(bad)).toThrow();
  });

  it('rejects an unknown category', () => {
    const bad = { severity: '🔴', file: 'x.ts', category: 'security', summary: 's' };
    expect(() => ReviewerFindingSchema.parse(bad)).toThrow();
  });

  it('rejects empty summary', () => {
    const bad = { severity: '🔴', file: 'x.ts', category: 'bug', summary: '' };
    expect(() => ReviewerFindingSchema.parse(bad)).toThrow();
  });

  it('rejects negative line number', () => {
    const bad = { severity: '🔴', file: 'x.ts', line: -1, category: 'bug', summary: 's' };
    expect(() => ReviewerFindingSchema.parse(bad)).toThrow();
  });
});

describe('ReviewerOutputSchema', () => {
  it('accepts approve+empty findings', () => {
    expect(() =>
      ReviewerOutputSchema.parse({ decision: 'approve', confidence: 'high', findings: [] }),
    ).not.toThrow();
  });

  it('accepts request_changes with multiple findings', () => {
    const ok = {
      decision: 'request_changes',
      confidence: 'medium',
      findings: [
        { severity: '🔴', file: 'a.ts', category: 'bug', summary: 'crashes' },
        { severity: '🟡', file: 'b.ts', category: 'design', summary: 'rename' },
      ],
    };
    expect(() => ReviewerOutputSchema.parse(ok)).not.toThrow();
  });

  it('rejects unknown decision', () => {
    const bad = { decision: 'merge_now', confidence: 'high', findings: [] };
    expect(() => ReviewerOutputSchema.parse(bad)).toThrow();
  });

  it('rejects unknown confidence', () => {
    const bad = { decision: 'approve', confidence: 'absolute', findings: [] };
    expect(() => ReviewerOutputSchema.parse(bad)).toThrow();
  });
});

// ─── parseReviewerOutput ──────────────────────────────────────────────────

describe('parseReviewerOutput', () => {
  it('extracts a well-formed payload after the marker', () => {
    const tail = `Some chatter from the agent.\n${REVIEW_MARKER}{"decision":"approve","confidence":"high","findings":[]}`;
    const r = parseReviewerOutput(tail);
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.output.decision).toBe('approve');
  });

  it('takes the LAST marker when multiple are present (echoed example in prompt vs. real output)', () => {
    const tail = `Example from prompt: ${REVIEW_MARKER}{"decision":"approve","confidence":"high","findings":[]}\nReal output:\n${REVIEW_MARKER}{"decision":"request_changes","confidence":"medium","findings":[{"severity":"🔴","file":"x.ts","category":"bug","summary":"real bug"}]}`;
    const r = parseReviewerOutput(tail);
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.output.decision).toBe('request_changes');
      expect(r.output.findings[0].severity).toBe('🔴');
    }
  });

  it('returns ok:false when the marker is missing', () => {
    const r = parseReviewerOutput('agent forgot to emit structured output');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain('marker');
  });

  it('returns ok:false on malformed JSON after the marker', () => {
    const r = parseReviewerOutput(`${REVIEW_MARKER}{"decision":"approve",}`);  // trailing comma
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain('JSON.parse');
  });

  it('returns ok:false when JSON is valid but schema rejects (unknown decision)', () => {
    const r = parseReviewerOutput(`${REVIEW_MARKER}{"decision":"merge_now","confidence":"high","findings":[]}`);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain('schema');
  });

  it('returns ok:false when the post-marker content does not start with {', () => {
    const r = parseReviewerOutput(`${REVIEW_MARKER}sorry no review today`);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toContain('expected JSON object');
  });

  it('handles trailing newline + content after the JSON line gracefully (takes only the line)', () => {
    const tail = `${REVIEW_MARKER}{"decision":"approve","confidence":"high","findings":[]}\nOh and one more thing...`;
    const r = parseReviewerOutput(tail);
    expect(r.ok).toBe(true);
  });

  it('roundtrips findings with line numbers', () => {
    const tail = `${REVIEW_MARKER}{"decision":"request_changes","confidence":"high","findings":[{"severity":"🔴","file":"apps/temporal-worker/src/dispatcher.ts","line":372,"category":"bug","summary":"writeDispatchMarker fires before submit","suggested_fix":"reorder"}]}`;
    const r = parseReviewerOutput(tail);
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.output.findings[0].line).toBe(372);
      expect(r.output.findings[0].suggested_fix).toBe('reorder');
    }
  });


  it('parses the marker when embedded inside Claude Code stream-json (escaped quotes)', () => {
    // Real-world tail captured from PR #275 R3 reviewer 2026-05-04.
    // Claude Code's --output-format stream-json wraps the agent's
    // text in JSON-encoded fields; the marker shows up with escaped
    // backslashes inside the wrapper. Pre-fix: parseReviewerOutput
    // saw the escaped form, JSON.parse threw, every reviewer tier
    // parse-failed → review chain cascaded to operator. Pin so this
    // doesn't regress.
    const streamJsonTail =
      'some preamble\n' +
      '{"type":"assistant","message":{"content":[{"type":"text",' +
      '"text":"clean diff.\\n\\n<<<REVIEW>>>{\\"decision\\":\\"approve\\",\\"confidence\\":\\"high\\",\\"findings\\":[]}"}]}}\n' +
      '{"type":"result","subtype":"success","result":"... <<<REVIEW>>>{\\"decision\\":\\"approve\\",\\"confidence\\":\\"high\\",\\"findings\\":[]}"}\n';
    const r = parseReviewerOutput(streamJsonTail);
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.output.decision).toBe('approve');
      expect(r.output.confidence).toBe('high');
      expect(r.output.findings).toEqual([]);
    }
  });

  it('parses the marker when emitted raw (agent printed directly to stdout)', () => {
    // Plain-text driver path (codex / openclaw / older claude)
    // emits the marker line without the stream-json wrapper.
    const rawTail =
      'reviewing the diff... ok approving.\n' +
      '<<<REVIEW>>>{"decision":"approve","confidence":"high","findings":[]}\n';
    const r = parseReviewerOutput(rawTail);
    expect(r.ok).toBe(true);
  });

  it('prefers the LATEST valid emit when multiple markers appear (e.g., agent self-corrected)', () => {
    const tail =
      '<<<REVIEW>>>{"decision":"approve","confidence":"low","findings":[]}\n' +
      'on reflection, this is bigger than I thought\n' +
      '<<<REVIEW>>>{"decision":"request_changes","confidence":"high","findings":[' +
      '{"severity":"🔴","file":"x","category":"bug","summary":"y"}]}\n';
    const r = parseReviewerOutput(tail);
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.output.decision).toBe('request_changes');
      expect(r.output.confidence).toBe('high');
      expect(r.output.findings).toHaveLength(1);
    }
  });
});

// ─── buildAdversarialReviewerPrompt ──────────────────────────────────────

describe('buildAdversarialReviewerPrompt', () => {
  it('throws on R0 (Copilot bot — not dispatched programmatically)', () => {
    expect(() => buildAdversarialReviewerPrompt({ ...baseInputs, tier: 'R0' })).toThrow(
      /not a dispatchable reviewer tier/,
    );
  });

  it('throws on R4 (operator escalation — not dispatched programmatically)', () => {
    expect(() => buildAdversarialReviewerPrompt({ ...baseInputs, tier: 'R4' })).toThrow(
      /not a dispatchable reviewer tier/,
    );
  });

  it('produces a non-empty prompt for each dispatchable tier', () => {
    for (const tier of ['R1', 'R2', 'R3'] as const) {
      const out = buildAdversarialReviewerPrompt({ ...baseInputs, tier });
      expect(out.length).toBeGreaterThan(500);
    }
  });

  it('includes the PR number + URL + entry id in the prompt', () => {
    const out = buildAdversarialReviewerPrompt(baseInputs);
    expect(out).toContain('PR #132');
    expect(out).toContain('https://github.com/chitinhq/chitin/pull/132');
    expect(out).toContain('review-graph-executor');
  });

  it('includes the structured-output marker so the agent knows to emit it', () => {
    const out = buildAdversarialReviewerPrompt(baseInputs);
    expect(out).toContain('<<<REVIEW>>>');
  });

  it('includes severity rules + decision rules + confidence rules', () => {
    const out = buildAdversarialReviewerPrompt(baseInputs);
    expect(out).toContain('Severity rules');
    expect(out).toContain('Decision rules');
    expect(out).toContain('Confidence rules');
    // Each severity emoji must appear in the rules section
    expect(out).toContain('🔴');
    expect(out).toContain('🟡');
    expect(out).toContain('🟢');
  });

  it('R3 tone is more adversarial than R1', () => {
    const r1 = buildAdversarialReviewerPrompt({ ...baseInputs, tier: 'R1' });
    const r3 = buildAdversarialReviewerPrompt({ ...baseInputs, tier: 'R3' });
    expect(r1).not.toContain('hostile reviewer');
    expect(r3).toContain('hostile reviewer');
  });

  it('R2 names the bucket-B contamination signature as a 🔴 (the load-bearing diff-vs-scope check)', () => {
    const r2 = buildAdversarialReviewerPrompt({ ...baseInputs, tier: 'R2' });
    expect(r2).toContain('bucket-B');
  });

  it('renders the file: scope when provided', () => {
    const out = buildAdversarialReviewerPrompt({
      ...baseInputs,
      entry_file_scope: 'apps/temporal-worker/src/foo.ts, libs/contracts/src/bar.ts',
    });
    expect(out).toContain('apps/temporal-worker/src/foo.ts');
    expect(out).toContain('libs/contracts/src/bar.ts');
  });

  it('handles missing file: scope gracefully (degrades to "evaluate without scope check")', () => {
    const out = buildAdversarialReviewerPrompt({ ...baseInputs, entry_file_scope: undefined });
    expect(out).toContain('not declared');
    expect(out).toContain('flag if the diff is suspiciously broad');
  });

  it('renders Copilot comments when provided', () => {
    const out = buildAdversarialReviewerPrompt({
      ...baseInputs,
      copilot_comments: '- L42: typo\n- L100: missing null check',
    });
    expect(out).toContain('typo');
    expect(out).toContain('missing null check');
    expect(out).toContain('verify EACH one');
  });

  it("notes when Copilot hasn't reviewed yet", () => {
    const out = buildAdversarialReviewerPrompt({ ...baseInputs, copilot_comments: undefined });
    expect(out).toContain("hasn't landed yet");
  });

  it('renders prior reviewer findings when escalating', () => {
    const out = buildAdversarialReviewerPrompt({
      ...baseInputs,
      prior_findings: '- 🔴 dispatcher.ts:42 — ordering bug',
    });
    expect(out).toContain('ordering bug');
    expect(out).toContain('escalated to');
  });

  it("declares the reviewer is the first when there are no prior findings", () => {
    const out = buildAdversarialReviewerPrompt({ ...baseInputs, prior_findings: undefined });
    expect(out).toContain('first reviewer');
  });

  it('lists the agent tools (gh CLI + read + tests + telemetry)', () => {
    const out = buildAdversarialReviewerPrompt(baseInputs);
    expect(out).toContain('gh pr diff');
    expect(out).toContain('gh pr view');
    expect(out).toContain('python/analysis/');
  });

  it('says the reviewer must NOT push to the PR branch (boundary with implementor)', () => {
    const out = buildAdversarialReviewerPrompt(baseInputs);
    expect(out).toContain("NOT allowed to push");
  });
});

// ─── End-to-end: build → mock agent output → parse round-trips ────────────

describe('end-to-end prompt → parse', () => {
  it('a well-formed agent response (matching the prompt example) parses cleanly', () => {
    const prompt = buildAdversarialReviewerPrompt(baseInputs);
    expect(prompt).toContain(REVIEW_MARKER);

    // Mock what an obedient agent would emit
    const mockTail = `Step-by-step review:
- read dispatcher.ts:300-380
- verified Copilot's L372 comment — confirmed, writeDispatchMarker fires before submit
- ran existing tests; sigkill-propagation suite still green
${REVIEW_MARKER}{"decision":"request_changes","confidence":"high","findings":[{"severity":"🔴","file":"apps/temporal-worker/src/dispatcher.ts","line":372,"category":"bug","summary":"writeDispatchMarker fires before client.workflow.start; if submit fails the marker is orphaned and the operator must rm to retry, but a fresh dispatch is what they actually want.","suggested_fix":"Move writeDispatchMarker after the workflow.start() try/catch — only mark on successful submit."}]}`;

    const result = parseReviewerOutput(mockTail);
    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.output.decision).toBe('request_changes');
      expect(result.output.confidence).toBe('high');
      expect(result.output.findings).toHaveLength(1);
      expect(result.output.findings[0].severity).toBe('🔴');
      expect(result.output.findings[0].line).toBe(372);
    }
  });
});
