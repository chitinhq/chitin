import { describe, expect, it } from 'vitest';
import { parseRecommendation } from '../src/grooming/parse-recommendation.ts';

describe('parseRecommendation', () => {
  it('extracts a ready recommendation from clean stdout', () => {
    const stdout = `\`\`\`json
{
  "entry_id": "my-entry",
  "status": "ready",
  "tier_recommendation": "T0",
  "estimated_loc": 5,
  "implementation_steps": ["s1", "s2", "s3"],
  "decomposition": [],
  "confidence": 0.9,
  "reasoning": "single-file change with clear scope"
}
\`\`\``;
    const r = parseRecommendation(stdout, 'my-entry');
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.recommendation.status).toBe('ready');
      expect(r.recommendation.tierRecommendation).toBe('T0');
      expect(r.recommendation.implementationSteps).toHaveLength(3);
      expect(r.recommendation.confidence).toBe(0.9);
    }
  });

  it('extracts a still_in_design recommendation with decomposition', () => {
    const stdout = `Some preamble text from the agent.

Then the answer:

\`\`\`json
{
  "entry_id": "big-entry",
  "status": "still_in_design",
  "tier_recommendation": "T2",
  "estimated_loc": 200,
  "implementation_steps": [],
  "decomposition": [
    {"id": "sub-a", "title": "Sub a", "tier": "T1"},
    {"id": "sub-b", "title": "Sub b", "tier": "T1"}
  ],
  "confidence": 0.7,
  "reasoning": "needs to split into network and storage parts"
}
\`\`\``;
    const r = parseRecommendation(stdout, 'big-entry');
    expect(r.ok).toBe(true);
    if (r.ok) {
      expect(r.recommendation.status).toBe('still_in_design');
      expect(r.recommendation.decomposition).toHaveLength(2);
      expect(r.recommendation.decomposition[0].id).toBe('sub-a');
    }
  });

  it('takes the LAST json block when multiple are present', () => {
    const stdout = `\`\`\`json
{"this": "is reasoning, not the answer"}
\`\`\`

then later, the actual answer:

\`\`\`json
{
  "entry_id": "x",
  "status": "ready",
  "tier_recommendation": "T0",
  "estimated_loc": 10,
  "implementation_steps": ["s1", "s2"],
  "decomposition": [],
  "confidence": 0.8,
  "reasoning": "ok"
}
\`\`\``;
    const r = parseRecommendation(stdout, 'x');
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.recommendation.status).toBe('ready');
  });

  it('rejects when no json block is present', () => {
    const r = parseRecommendation('agent forgot the json fence', 'x');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toMatch(/no.*json block/i);
  });

  it('rejects when entry_id mismatches', () => {
    const stdout = `\`\`\`json
{
  "entry_id": "wrong-id",
  "status": "ready",
  "tier_recommendation": "T0",
  "estimated_loc": 5,
  "implementation_steps": ["a", "b"],
  "decomposition": [],
  "confidence": 1,
  "reasoning": "x"
}
\`\`\``;
    const r = parseRecommendation(stdout, 'expected-id');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toMatch(/mismatch/);
  });

  it('rejects status=ready with empty implementation_steps', () => {
    const stdout = `\`\`\`json
{
  "entry_id": "x",
  "status": "ready",
  "tier_recommendation": "T0",
  "estimated_loc": 5,
  "implementation_steps": [],
  "decomposition": [],
  "confidence": 1,
  "reasoning": "x"
}
\`\`\``;
    const r = parseRecommendation(stdout, 'x');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toMatch(/implementation_steps.*empty/);
  });

  it('rejects status=still_in_design with too few decomposition items', () => {
    const stdout = `\`\`\`json
{
  "entry_id": "x",
  "status": "still_in_design",
  "tier_recommendation": "T2",
  "estimated_loc": 100,
  "implementation_steps": [],
  "decomposition": [{"id": "a", "title": "a", "tier": "T1"}],
  "confidence": 0.7,
  "reasoning": "x"
}
\`\`\``;
    const r = parseRecommendation(stdout, 'x');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toMatch(/decomposition has <2/);
  });

  it('rejects unknown status or tier', () => {
    const stdout = `\`\`\`json
{
  "entry_id": "x",
  "status": "maybe",
  "tier_recommendation": "T9",
  "estimated_loc": 1,
  "implementation_steps": [],
  "decomposition": [],
  "confidence": 0,
  "reasoning": ""
}
\`\`\``;
    const r = parseRecommendation(stdout, 'x');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toMatch(/invalid status/);
  });

  it('rejects malformed json gracefully', () => {
    const stdout = `\`\`\`json
{ this is not valid json }
\`\`\``;
    const r = parseRecommendation(stdout, 'x');
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toMatch(/json parse failed/);
  });
});
