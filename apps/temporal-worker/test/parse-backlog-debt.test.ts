import { describe, expect, it } from 'vitest';
import { crossesDebtLedger } from '../src/grooming/parse-backlog.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';

function makeEntry(overrides: Partial<BacklogEntry> = {}): BacklogEntry {
  return {
    id: 'sample',
    status: 'ready',
    description: 'sample',
    rawFrontmatter: '```yaml\nid: sample\n```',
    rawSection: '### sample',
    ...overrides,
  };
}

describe('crossesDebtLedger', () => {
  it('returns false when entry has no file: field', () => {
    expect(crossesDebtLedger(makeEntry({ file: undefined }), ['apps/foo.ts'])).toBe(false);
  });

  it('returns false when debt file list is empty', () => {
    expect(crossesDebtLedger(makeEntry({ file: 'apps/foo.ts' }), [])).toBe(false);
  });

  it('returns true when single-file entry matches a debt file', () => {
    expect(crossesDebtLedger(makeEntry({ file: 'apps/foo.ts' }), ['apps/foo.ts'])).toBe(true);
  });

  it('returns true when any of multiple entry files matches debt', () => {
    expect(
      crossesDebtLedger(
        makeEntry({ file: 'apps/foo.ts, apps/bar.ts, apps/baz.ts' }),
        ['apps/bar.ts', 'libs/qux.ts'],
      ),
    ).toBe(true);
  });

  it('returns false when no entry file matches any debt file', () => {
    expect(
      crossesDebtLedger(
        makeEntry({ file: 'apps/foo.ts, apps/bar.ts' }),
        ['apps/qux.ts', 'libs/zap.ts'],
      ),
    ).toBe(false);
  });

  it('handles whitespace around comma-separated entry paths', () => {
    expect(
      crossesDebtLedger(
        makeEntry({ file: '  apps/foo.ts  ,  apps/bar.ts  ' }),
        ['apps/foo.ts'],
      ),
    ).toBe(true);
  });

  it('handles whitespace in debt file list too', () => {
    expect(
      crossesDebtLedger(
        makeEntry({ file: 'apps/foo.ts' }),
        ['  apps/foo.ts  '],
      ),
    ).toBe(true);
  });

  it('does not match the cross-cutting sentinel by accident', () => {
    // debt-ledger.md uses the literal string 'cross-cutting' as the file:
    // value for cross-cutting debt. Entries don't normally use that
    // string, so the helper shouldn't false-match.
    expect(
      crossesDebtLedger(
        makeEntry({ file: 'apps/foo.ts' }),
        ['cross-cutting'],
      ),
    ).toBe(false);
  });

  it('exact match required (no substring match)', () => {
    expect(
      crossesDebtLedger(
        makeEntry({ file: 'apps/foo.ts' }),
        ['apps/foo.ts.backup'],
      ),
    ).toBe(false);
  });

  it('returns false on entry.file = "" (empty string after split)', () => {
    expect(crossesDebtLedger(makeEntry({ file: '' }), ['apps/foo.ts'])).toBe(false);
  });
});
