import { describe, expect, it } from 'vitest';
import { findUnmetBlocker } from '../src/dispatcher.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';

function makeEntry(overrides: Partial<BacklogEntry>): BacklogEntry {
  return {
    id: 'sample',
    status: 'ready',
    description: 'sample',
    rawFrontmatter: '',
    rawSection: '',
    ...overrides,
  };
}

describe('findUnmetBlocker', () => {
  it('returns undefined when entry.blocks is undefined (no blockers declared)', () => {
    expect(findUnmetBlocker(makeEntry({}), () => true)).toBeUndefined();
  });

  it('returns undefined when entry.blocks is an empty array', () => {
    expect(findUnmetBlocker(makeEntry({ blocks: [] }), () => false)).toBeUndefined();
  });

  it('returns undefined when every blocker has shipped (origin branch exists for each)', () => {
    const entry = makeEntry({ blocks: ['a', 'b', 'c'] });
    const isShipped = (id: string) => ['a', 'b', 'c'].includes(id);
    expect(findUnmetBlocker(entry, isShipped)).toBeUndefined();
  });

  it('returns the unmet blocker id when one blocker has not shipped', () => {
    const entry = makeEntry({ blocks: ['a', 'b', 'c'] });
    const shipped = new Set(['a', 'c']);
    expect(findUnmetBlocker(entry, (id) => shipped.has(id))).toBe('b');
  });

  it('returns the FIRST unmet blocker, in declared order (deterministic for logging)', () => {
    const entry = makeEntry({ blocks: ['a', 'b', 'c'] });
    expect(findUnmetBlocker(entry, () => false)).toBe('a');
  });

  it('returns the unmet blocker even when later blockers are also unmet (short-circuit)', () => {
    const calls: string[] = [];
    const entry = makeEntry({ blocks: ['x', 'y', 'z'] });
    const isShipped = (id: string) => {
      calls.push(id);
      return false;
    };
    expect(findUnmetBlocker(entry, isShipped)).toBe('x');
    // Array.prototype.find short-circuits on first match — important
    // because each isShipped call is a real git invocation in
    // production. Verify we don't invoke after the first hit.
    expect(calls).toEqual(['x']);
  });

  it("treats the dispatch-marker semantics correctly: a marker alone doesn't make a blocker shipped", () => {
    // Regression: the swarm's first cut used `entryHasDispatchMarker(blocker)`
    // as a "still in flight" signal. Markers persist past completion, so
    // a blocker that shipped successfully would still match — incorrectly
    // blocking descendants forever. The fix moves the source of truth to
    // the origin branch alone. This test pins the invariant by passing
    // an isShipped that returns false for an id that "had a marker" — it
    // should still register as blocked.
    const entry = makeEntry({ blocks: ['has-stale-marker'] });
    const isShipped = () => false; // origin branch not present
    expect(findUnmetBlocker(entry, isShipped)).toBe('has-stale-marker');
  });

  it("treats a single shipped blocker correctly when entry.blocks has exactly one entry", () => {
    const entry = makeEntry({ blocks: ['only-blocker'] });
    expect(findUnmetBlocker(entry, () => true)).toBeUndefined();
    expect(findUnmetBlocker(entry, () => false)).toBe('only-blocker');
  });
});
