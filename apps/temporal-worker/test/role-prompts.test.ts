import { describe, expect, it } from 'vitest';
import {
  buildPromptForEntry,
  buildProgrammerPrompt,
  resolveEntryRole,
  __test__,
} from '../src/role-prompts.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';

const { ROLE_VOCAB, ROLE_PROMPTS } = __test__;

function makeEntry(overrides: Partial<BacklogEntry>): BacklogEntry {
  return {
    id: 'sample',
    status: 'ready',
    description: 'sample description',
    rawFrontmatter: '```yaml\nid: sample\nstatus: ready\n```',
    rawSection: '### sample',
    ...overrides,
  };
}

describe('resolveEntryRole', () => {
  it('returns programmer when role is absent', () => {
    expect(resolveEntryRole(makeEntry({}))).toEqual({ role: 'programmer' });
  });

  it('returns programmer with no warning for empty / whitespace role', () => {
    expect(resolveEntryRole(makeEntry({ role: '' }))).toEqual({ role: 'programmer' });
    expect(resolveEntryRole(makeEntry({ role: '   ' }))).toEqual({ role: 'programmer' });
  });

  it('returns the role when it matches the registry', () => {
    for (const role of Array.from(ROLE_VOCAB)) {
      const result = resolveEntryRole(makeEntry({ role }));
      expect(result.role).toBe(role);
      expect(result.warning).toBeUndefined();
    }
  });

  it('falls back to programmer with a warning for an unknown role', () => {
    const r = resolveEntryRole(makeEntry({ id: 'oddball', role: 'wizard' }));
    expect(r.role).toBe('programmer');
    expect(r.warning).toContain('oddball');
    expect(r.warning).toContain('"wizard"');
  });
});

describe('buildProgrammerPrompt', () => {
  it('uses the entry file with ./ prefix when relative', () => {
    const out = buildProgrammerPrompt(makeEntry({ file: 'apps/foo/bar.ts' }));
    expect(out).toContain('TARGET FILE: ./apps/foo/bar.ts');
  });

  it('preserves the absolute path when entry.file is /-prefixed', () => {
    const out = buildProgrammerPrompt(makeEntry({ file: '/abs/path.ts' }));
    expect(out).toContain('TARGET FILE: /abs/path.ts');
  });

  it('falls back to a placeholder when entry.file is missing (does not prefix it)', () => {
    const out = buildProgrammerPrompt(makeEntry({}));
    expect(out).toContain('TARGET FILE: the file named in the entry');
    expect(out).not.toContain('TARGET FILE: ./the file named in the entry');
  });

  it('preserves the prompt invariants from slice 7b (tool-dispatch directives, scope rules)', () => {
    const out = buildProgrammerPrompt(makeEntry({}));
    expect(out).toContain('TOOL DISPATCHES count');
    expect(out).toContain('Do not modify chitin.yaml');
    expect(out).toContain('Only edit files referenced in the entry');
  });
});

describe('buildPromptForEntry', () => {
  it('uses the programmer template when role is absent', () => {
    const programmer = buildProgrammerPrompt(makeEntry({ file: 'a.ts' }));
    const dispatched = buildPromptForEntry(makeEntry({ file: 'a.ts' }));
    expect(dispatched).toBe(programmer);
  });

  it('uses the programmer template explicitly when role: programmer', () => {
    const programmer = buildProgrammerPrompt(makeEntry({ file: 'a.ts' }));
    const dispatched = buildPromptForEntry(makeEntry({ file: 'a.ts', role: 'programmer' }));
    expect(dispatched).toBe(programmer);
  });

  it('routes still-stubbed non-programmer roles to a stub that names the role', () => {
    const dispatched = buildPromptForEntry(makeEntry({ id: 'p-test', role: 'product' }));
    expect(dispatched).toContain('product');
    expect(dispatched).toContain('p-test');
    // Stub does NOT have the programmer's TOOL DISPATCHES preamble.
    expect(dispatched).not.toContain('TOOL DISPATCHES count');
  });

  it('routes role: researcher to the dedicated researcher prompt (not the generic stub)', () => {
    const dispatched = buildPromptForEntry(makeEntry({ id: 'r-test', role: 'researcher' }));
    expect(dispatched).toContain('researcher role');
    expect(dispatched).toContain('r-test');
    // Researcher prompt mandates the structured-output marker — that's
    // the load-bearing diff between the dedicated template and the
    // generic stub.
    expect(dispatched).toContain('<<<CANDIDATES>>>');
    // And it doesn't borrow the programmer preamble.
    expect(dispatched).not.toContain('TOOL DISPATCHES count');
  });

  it('every role in ROLE_VOCAB has a builder that returns a non-empty string', () => {
    for (const role of Array.from(ROLE_VOCAB)) {
      const out = ROLE_PROMPTS[role as keyof typeof ROLE_PROMPTS](makeEntry({ id: `${role}-test` }));
      expect(out.length).toBeGreaterThan(50);
    }
  });

  it('routes role: analyst to the dedicated analyst prompt (internal-telemetry lane)', () => {
    const dispatched = buildPromptForEntry(makeEntry({ id: 'a-test', role: 'analyst' }));
    expect(dispatched).toContain('analyst role');
    expect(dispatched).toContain('a-test');
    // The analyst prompt mandates the structured-output marker.
    expect(dispatched).toContain('<<<ANALYSIS>>>');
    // Names the python/analysis toolkit the role's job is to use.
    expect(dispatched).toContain('python/analysis');
    // Does NOT borrow programmer preamble — analyst output is reports,
    // not commits.
    expect(dispatched).not.toContain('TOOL DISPATCHES count');
    // Names the report path convention so the apply step's lookup works.
    expect(dispatched).toContain('python/analysis/out/a-test.md');
  });

  it('falls back to programmer prompt when role is unknown (defensive)', () => {
    const fallback = buildPromptForEntry(makeEntry({ file: 'x.ts', role: 'time-traveler' }));
    expect(fallback).toContain('TOOL DISPATCHES count');  // programmer template signature
    expect(fallback).toContain('TARGET FILE: ./x.ts');
  });
});
