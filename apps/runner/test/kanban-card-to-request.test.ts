import { describe, expect, it } from 'vitest';
import { __test__ } from '../src/kanban-card-to-request.ts';

const { parseCardBody, cardToEntry } = __test__;

const SAMPLE_BODY = `\`\`\`yaml
id: chain-fingerprint-tagging-coverage
tier: T3
status: ready
estimated_loc: 250
blocks: []
file: apps/runner/src/dispatcher.ts, libs/chain/src/event.ts
references_finding: 2026-05-05-fingerprint-coverage-gap
role: programmer
\`\`\`

Telemetry blind spot: \`/mine\` 2026-05-05 shows 2317 of ~2530
chain dispatches (92%) emit with empty role / model / fingerprint.
The P2 fingerprint plumbing landed but isn't reaching the dispatcher.

Fix scope:
1. Audit emit sites
2. Wire role/model through
`;

describe('parseCardBody', () => {
  it('extracts the YAML frontmatter into a flat key→value map', () => {
    const r = parseCardBody(SAMPLE_BODY);
    expect(r.fields.id).toBe('chain-fingerprint-tagging-coverage');
    expect(r.fields.tier).toBe('T3');
    expect(r.fields.role).toBe('programmer');
    expect(r.fields.references_finding).toBe('2026-05-05-fingerprint-coverage-gap');
  });

  it('captures the raw frontmatter string for downstream re-emission', () => {
    const r = parseCardBody(SAMPLE_BODY);
    expect(r.rawFrontmatter).toMatch(/id: chain-fingerprint/);
    expect(r.rawFrontmatter).not.toContain('```');
  });

  it('captures the description as everything after the YAML block', () => {
    const r = parseCardBody(SAMPLE_BODY);
    expect(r.description).toMatch(/^Telemetry blind spot/);
    expect(r.description).toContain('Fix scope:');
    expect(r.description).not.toContain('```yaml');
  });

  it('handles a body with no YAML frontmatter (operator-created card)', () => {
    const body = 'Just a free-form note from the operator.';
    const r = parseCardBody(body);
    expect(r.fields).toEqual({});
    expect(r.rawFrontmatter).toBe('');
    expect(r.description).toBe(body);
  });

  it('strips an optional leading separator after the YAML block', () => {
    const body = '```yaml\nid: x\n```\n\n---\n\nbody text';
    const r = parseCardBody(body);
    expect(r.description).toBe('body text');
  });
});

describe('cardToEntry', () => {
  it('builds a BacklogEntry-shape object from parsed card fields', () => {
    const parsed = parseCardBody(SAMPLE_BODY);
    const entry = cardToEntry('t_834d2e12', 'chain-fingerprint-tagging-coverage', parsed);
    expect(entry.id).toBe('chain-fingerprint-tagging-coverage');
    expect(entry.role).toBe('programmer');
    expect(entry.tier).toBe('T3');
    expect(entry.file).toBe('apps/runner/src/dispatcher.ts, libs/chain/src/event.ts');
    expect(entry.referencesFinding).toBe('2026-05-05-fingerprint-coverage-gap');
    expect(entry.description).toMatch(/Telemetry blind spot/);
    // Status is set to 'ready' by definition — if a card got here, it's claimable.
    expect(entry.status).toBe('ready');
  });

  it('falls back to the card title for id when YAML lacks an id field', () => {
    const parsed = parseCardBody('no yaml here');
    const entry = cardToEntry('t_xyz', 'fallback-title', parsed);
    expect(entry.id).toBe('fallback-title');
  });
});
