import { describe, expect, it } from 'vitest';
import {
  extractRawEntries,
  lintRawEntries,
  type RawEntry,
} from '../backlog-entry-shape.ts';

const VALID_ROLES = new Set([
  'researcher',
  'product',
  'groomer',
  'architect',
  'programmer',
  'reviewer',
  'qa',
  'gatekeeper',
  'tech-writer',
  'analyst',
  'refactorer',
  'debt-curator',
]);

function entry(headingId: string, fields: Record<string, string>): RawEntry {
  return {
    headingId,
    rawFrontmatter: Object.entries(fields).map(([k, v]) => `${k}: ${v}`).join('\n'),
    fields,
    line: 1,
  };
}

describe('lintRawEntries — schema rules', () => {
  it('clean entry → no errors', () => {
    const errs = lintRawEntries(
      [entry('foo', { id: 'foo', tier: 'T2', status: 'ready', role: 'programmer', blocks: '[]' })],
      VALID_ROLES,
    );
    expect(errs).toEqual([]);
  });

  it('absent optional fields → no errors', () => {
    // tier / status / role / blocks all optional per the parser
    const errs = lintRawEntries([entry('foo', { id: 'foo' })], VALID_ROLES);
    expect(errs).toEqual([]);
  });

  it('heading id ≠ frontmatter id → mismatch error (PR #200 footgun)', () => {
    const errs = lintRawEntries(
      [entry('comment-responder', { id: 'comment-responder-role' })],
      VALID_ROLES,
    );
    expect(errs).toHaveLength(1);
    expect(errs[0]!.rule).toBe('heading-id-frontmatter-id-mismatch');
  });

  it('heading id with no frontmatter id → no error (id field is optional)', () => {
    const errs = lintRawEntries([entry('foo', {})], VALID_ROLES);
    expect(errs).toEqual([]);
  });

  it('invalid tier → error', () => {
    const errs = lintRawEntries([entry('foo', { tier: 'T7', status: 'ready' })], VALID_ROLES);
    expect(errs).toHaveLength(1);
    expect(errs[0]!.rule).toBe('invalid-tier');
  });

  it.each(['T0', 'T1', 'T2', 'T3', 'T4', 'T5'])('tier %s is valid', (tier) => {
    const errs = lintRawEntries([entry('foo', { tier })], VALID_ROLES);
    expect(errs).toEqual([]);
  });

  it('tier: TBD is valid on status=in_design (ungroomed sentinel)', () => {
    const errs = lintRawEntries(
      [entry('foo', { tier: 'TBD', status: 'in_design' })],
      VALID_ROLES,
    );
    expect(errs).toEqual([]);
  });

  it('tier: TBD is INVALID on status=ready (must be groomed by then)', () => {
    const errs = lintRawEntries(
      [entry('foo', { tier: 'TBD', status: 'ready' })],
      VALID_ROLES,
    );
    expect(errs).toHaveLength(1);
    expect(errs[0]!.rule).toBe('invalid-tier');
  });

  it('invalid status → error', () => {
    // Use a clearly-invalid value (not one of the lifecycle states
    // the linter recognizes — see VALID_STATUSES in the linter).
    const errs = lintRawEntries([entry('foo', { status: 'queued-for-tomorrow' })], VALID_ROLES);
    expect(errs).toHaveLength(1);
    expect(errs[0]!.rule).toBe('invalid-status');
  });

  it.each([
    'ready',
    'in_design',
    'needs_human',
    'blocked',
    'in_flight',
    'completed',
    'shipped',
    'partial',
    'decomposed',
  ])('status %s is valid', (status) => {
    const errs = lintRawEntries([entry('foo', { status })], VALID_ROLES);
    expect(errs).toEqual([]);
  });

  it('invalid role → error', () => {
    const errs = lintRawEntries([entry('foo', { role: 'fixer' })], VALID_ROLES);
    expect(errs).toHaveLength(1);
    expect(errs[0]!.rule).toBe('invalid-role');
  });

  it('valid role from RoleSchema → no error', () => {
    const errs = lintRawEntries([entry('foo', { role: 'programmer' })], VALID_ROLES);
    expect(errs).toEqual([]);
  });

  it('blockedBy field → forbidden error (PR #200 footgun)', () => {
    const errs = lintRawEntries([entry('foo', { blockedBy: '[bar]' })], VALID_ROLES);
    expect(errs).toHaveLength(1);
    expect(errs[0]!.rule).toBe('forbidden-blockedBy');
  });

  it('snake_case blocked_by also caught (extra defensiveness)', () => {
    const errs = lintRawEntries([entry('foo', { blocked_by: '[bar]' })], VALID_ROLES);
    expect(errs).toHaveLength(1);
    expect(errs[0]!.rule).toBe('forbidden-blockedBy');
  });

  it('blocks: [a, b] is valid', () => {
    const errs = lintRawEntries([entry('foo', { blocks: '[a, b]' })], VALID_ROLES);
    expect(errs).toEqual([]);
  });

  it('blocks: [] is valid', () => {
    const errs = lintRawEntries([entry('foo', { blocks: '[]' })], VALID_ROLES);
    expect(errs).toEqual([]);
  });

  it('blocks not in inline-array shape → malformed error', () => {
    const errs = lintRawEntries([entry('foo', { blocks: 'a, b' })], VALID_ROLES);
    expect(errs).toHaveLength(1);
    expect(errs[0]!.rule).toBe('malformed-blocks');
  });

  it('multiple violations on one entry → all reported', () => {
    const errs = lintRawEntries(
      [entry('foo', {
        id: 'bar',                  // mismatch
        tier: 'T9',                 // invalid tier
        status: 'queued',           // invalid status
        role: 'unknown',            // invalid role
        blockedBy: '[x]',           // forbidden
      })],
      VALID_ROLES,
    );
    const rules = errs.map((e) => e.rule).sort();
    expect(rules).toEqual([
      'forbidden-blockedBy',
      'heading-id-frontmatter-id-mismatch',
      'invalid-role',
      'invalid-status',
      'invalid-tier',
    ]);
  });

  it('errors sorted by (entry, rule) for deterministic output', () => {
    const entries = [
      entry('zeta', { tier: 'T9' }),
      entry('alpha', { tier: 'T9' }),
    ];
    const errs = lintRawEntries(entries, VALID_ROLES);
    expect(errs.map((e) => e.entry)).toEqual(['alpha', 'zeta']);
  });
});

describe('extractRawEntries', () => {
  const sample = `# Some unrelated heading

## Section
prose
prose

### \`foo\`

\`\`\`yaml
id: foo
tier: T2
status: ready
\`\`\`

description prose

### \`bar\`

\`\`\`yaml
id: bar
tier: T1
\`\`\`

more description

## Different section
unrelated
`;

  it('finds all ### entries', () => {
    const entries = extractRawEntries(sample);
    expect(entries.map((e) => e.headingId)).toEqual(['foo', 'bar']);
  });

  it('parses YAML fields per entry', () => {
    const entries = extractRawEntries(sample);
    expect(entries[0]?.fields).toEqual({ id: 'foo', tier: 'T2', status: 'ready' });
    expect(entries[1]?.fields).toEqual({ id: 'bar', tier: 'T1' });
  });

  it('records 1-indexed line of heading', () => {
    const entries = extractRawEntries(sample);
    // The first ### `foo` heading line in the sample (1-indexed).
    // The leading blank-prefix lines plus "## Section" and prose
    // put it at line 7 in the template literal layout above.
    expect(entries[0]?.line).toBe(7);
  });

  it('handles entry with no YAML block (heading only)', () => {
    const text = '### `lonely`\n\nNo yaml block here.\n';
    const entries = extractRawEntries(text);
    expect(entries).toHaveLength(1);
    expect(entries[0]?.headingId).toBe('lonely');
    expect(entries[0]?.fields).toEqual({});
  });

  it('captures unparseable heading shape gracefully', () => {
    const text = '### no-backticks-here\n\n\`\`\`yaml\nid: x\n\`\`\`\n';
    const entries = extractRawEntries(text);
    expect(entries[0]?.headingId).toBe('<unparseable-heading>');
  });
});
