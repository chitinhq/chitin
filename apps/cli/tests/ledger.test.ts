import { describe, expect, it } from 'vitest';
import {
  buildStubEntry,
  laneLabelFor,
  lintStructural,
  nextGDL,
  pad,
  splitEntries,
} from '../src/commands/ledger';

describe('nextGDL', () => {
  it('returns 1 for an empty ledger', () => {
    expect(nextGDL('# Governance-Debt Ledger\n\n## Entries\n')).toBe(1);
  });

  it('returns 2 when GDL-001 exists', () => {
    expect(nextGDL('### GDL-001 — foo\n\nbody\n')).toBe(2);
  });

  it('returns max + 1 across non-contiguous ids', () => {
    const body = '### GDL-001 — a\n\n### GDL-003 — b\n\n### GDL-002 — c\n';
    expect(nextGDL(body)).toBe(4);
  });

  it('ignores HTML comment references like GDL-000', () => {
    const body = '<!-- GDL-000 reserved -->\n### GDL-001 — real\n';
    expect(nextGDL(body)).toBe(2);
  });
});

describe('laneLabelFor', () => {
  it('maps 1/fix', () => {
    expect(laneLabelFor('1')).toBe('① FIX');
    expect(laneLabelFor('fix')).toBe('① FIX');
    expect(laneLabelFor('FIX')).toBe('① FIX');
  });

  it('maps 2/determinism', () => {
    expect(laneLabelFor('2')).toBe('② DETERMINISM');
    expect(laneLabelFor('determinism')).toBe('② DETERMINISM');
  });

  it('maps 3/soul-routing/soul', () => {
    expect(laneLabelFor('3')).toBe('③ SOUL ROUTING');
    expect(laneLabelFor('soul-routing')).toBe('③ SOUL ROUTING');
    expect(laneLabelFor('soul')).toBe('③ SOUL ROUTING');
  });

  it('throws on unknown lanes', () => {
    expect(() => laneLabelFor('4')).toThrow(/unknown lane/);
    expect(() => laneLabelFor('bogus')).toThrow(/unknown lane/);
  });
});

describe('pad', () => {
  it('zero-pads to 3 digits', () => {
    expect(pad(1)).toBe('001');
    expect(pad(42)).toBe('042');
    expect(pad(999)).toBe('999');
    expect(pad(1000)).toBe('1000');
  });
});

describe('splitEntries', () => {
  it('returns [] on empty body', () => {
    expect(splitEntries('')).toEqual([]);
  });

  it('skips preamble before the first entry', () => {
    const body = '# Header\n\nsome text\n\n### GDL-001 — a\n\nbody a\n';
    const out = splitEntries(body);
    expect(out).toHaveLength(1);
    expect(out[0].id).toBe('GDL-001');
    expect(out[0].body).toContain('body a');
    expect(out[0].body).not.toContain('some text');
  });

  it('splits multiple entries', () => {
    const body = '### GDL-001 — a\n\nbody a\n\n### GDL-002 — b\n\nbody b\n';
    const out = splitEntries(body);
    expect(out.map((e) => e.id)).toEqual(['GDL-001', 'GDL-002']);
    expect(out[0].body).toContain('body a');
    expect(out[0].body).not.toContain('body b');
    expect(out[1].body).toContain('body b');
  });
});

describe('buildStubEntry', () => {
  const baseInput = {
    lane: '1',
    surface: 'claude-code',
    repo: 'chitin',
    soul: 'davinci',
    today: '2026-04-20',
  };

  it('includes header with padded id', () => {
    const entry = buildStubEntry(7, baseInput);
    expect(entry).toContain('### GDL-007 — <one-line');
  });

  it('emits lane label for the given lane', () => {
    expect(buildStubEntry(1, { ...baseInput, lane: '2' })).toContain('② DETERMINISM');
  });

  it('substitutes provided chain / seq / hash', () => {
    const entry = buildStubEntry(1, {
      ...baseInput,
      chain: 'abcdef',
      seq: '5',
      hash: '0123456789abcdef012345',
    });
    expect(entry).toContain('chain `abcdef`');
    expect(entry).toContain('seq `5`');
    expect(entry).toContain('hash `0123456789ab`'); // first 12 chars
  });

  it('inserts placeholder references when trace fields are omitted', () => {
    const entry = buildStubEntry(1, baseInput);
    expect(entry).toContain('chain `<chain_id>`');
    expect(entry).toContain('seq `<n>`');
    expect(entry).toContain('hash `<this_hash>`');
  });

  it('records surface, repo, soul, and today', () => {
    const entry = buildStubEntry(1, { ...baseInput, surface: 'gh-actions', soul: 'knuth' });
    expect(entry).toContain('gh-actions / chitin');
    expect(entry).toContain('knuth @ <soul_hash[:8]>');
    expect(entry).toContain('**Observed:** 2026-04-20');
  });
});

describe('lintStructural', () => {
  function bodyOf(block: string): { body: string; blocks: { id: string; body: string }[] } {
    const blocks = splitEntries(block);
    return { body: block, blocks };
  }

  it('reports nothing on a well-formed entry', () => {
    const { body, blocks } = bodyOf(
      '### GDL-001 — ok\n\n- **Observed:** x\n- **Lane:** ① FIX\n- **Graduated:** <null>\n',
    );
    expect(lintStructural(body, blocks)).toEqual([]);
  });

  it('reports duplicate ids as error', () => {
    const body =
      '### GDL-001 — a\n\n- **Observed:** x\n- **Lane:** ① FIX\n- **Graduated:** <null>\n' +
      '### GDL-001 — b\n\n- **Observed:** y\n- **Lane:** ② DETERMINISM\n- **Graduated:** <null>\n';
    const blocks = splitEntries(body);
    const findings = lintStructural(body, blocks);
    expect(findings.some((f) => f.level === 'error' && f.msg === 'duplicate ID')).toBe(true);
  });

  it('reports missing Observed as error', () => {
    const { body, blocks } = bodyOf(
      '### GDL-001 — ok\n\n- **Lane:** ① FIX\n- **Graduated:** <null>\n',
    );
    const findings = lintStructural(body, blocks);
    expect(findings).toContainEqual({
      level: 'error',
      gdl: 'GDL-001',
      msg: 'missing Observed field',
    });
  });

  it('reports missing Lane as error', () => {
    const { body, blocks } = bodyOf(
      '### GDL-001 — ok\n\n- **Observed:** x\n- **Graduated:** <null>\n',
    );
    const findings = lintStructural(body, blocks);
    expect(findings).toContainEqual({
      level: 'error',
      gdl: 'GDL-001',
      msg: 'missing or malformed Lane',
    });
  });

  it('reports malformed Lane (no circled digit) as error', () => {
    const { body, blocks } = bodyOf(
      '### GDL-001 — ok\n\n- **Observed:** x\n- **Lane:** FIX\n- **Graduated:** <null>\n',
    );
    const findings = lintStructural(body, blocks);
    expect(findings).toContainEqual({
      level: 'error',
      gdl: 'GDL-001',
      msg: 'missing or malformed Lane',
    });
  });

  it('reports missing Graduated as warn (not error)', () => {
    const { body, blocks } = bodyOf(
      '### GDL-001 — ok\n\n- **Observed:** x\n- **Lane:** ① FIX\n',
    );
    const findings = lintStructural(body, blocks);
    expect(findings).toContainEqual({
      level: 'warn',
      gdl: 'GDL-001',
      msg: 'missing Graduated field',
    });
    expect(findings.filter((f) => f.level === 'error')).toEqual([]);
  });
});
