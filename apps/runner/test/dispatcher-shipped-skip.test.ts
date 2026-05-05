import { describe, expect, it } from 'vitest';
import { entryIdInRecentSubjects } from '../src/dispatcher.ts';

// Tests for the dispatcher's shipped-entry-skip heuristic. Per
// Copilot review on PR #265: every branch of this gate has had
// a real bug at some point (regex meta in --grep, scanning local
// HEAD instead of origin/main, --merges missing squash-merges).
// These tests pin the behavior so future refactors don't regress.

describe('entryIdInRecentSubjects', () => {
  it('returns false when subjects is empty', () => {
    expect(entryIdInRecentSubjects('foo', '')).toBe(false);
  });

  it('returns false when entryId is empty', () => {
    expect(entryIdInRecentSubjects('', 'subject line')).toBe(false);
  });

  it('matches an entry id that appears as a discrete word', () => {
    const subjects = 'swarm: foo — closes #100\nfix: unrelated change';
    expect(entryIdInRecentSubjects('foo', subjects)).toBe(true);
  });

  it('does NOT match an entry id that is a substring of a longer id', () => {
    // Without the discrete-word check, "foo" would match
    // "swarm: foo-bar — closes #100" — but those are different
    // entries. The match must be word-bounded.
    const subjects = 'swarm: foo-bar — closes #100';
    expect(entryIdInRecentSubjects('foo', subjects)).toBe(false);
  });

  it('matches when entry id is bounded by punctuation/whitespace', () => {
    const subjects = 'swarm: my-entry-id (T2) — done';
    expect(entryIdInRecentSubjects('my-entry-id', subjects)).toBe(true);
  });

  it('treats id with regex metacharacters literally (no --grep regex semantics)', () => {
    // Backlog ids historically can contain dots / parens. The old
    // implementation passed the id to `git log --grep <id>` which
    // interprets it as regex; "pr-event.1" would match
    // "pr-eventX1" via regex meta. Confirm we DON'T do that.
    const subjects = 'swarm: pr-eventX1 — done';
    expect(entryIdInRecentSubjects('pr-event.1', subjects)).toBe(false);
  });

  it('matches across multiple subjects when one matches', () => {
    const subjects = [
      'fix: unrelated change',
      'docs: another change',
      'swarm: target-id (T3)',
      'feat: still unrelated',
    ].join('\n');
    expect(entryIdInRecentSubjects('target-id', subjects)).toBe(true);
  });

  it('case-sensitive match (entry ids are lowercase by convention)', () => {
    const subjects = 'swarm: TARGET — done';
    expect(entryIdInRecentSubjects('target', subjects)).toBe(false);
  });

  it('matches when id is at start or end of subject', () => {
    expect(entryIdInRecentSubjects('foo', 'foo')).toBe(true);
    expect(entryIdInRecentSubjects('foo', 'finalized: foo')).toBe(true);
    expect(entryIdInRecentSubjects('foo', 'foo finalized')).toBe(true);
  });

  it('handles empty lines and CRLF gracefully', () => {
    const subjects = 'fix: x\n\n\nswarm: target — closes\n';
    expect(entryIdInRecentSubjects('target', subjects)).toBe(true);
  });

  // Entry-filing prefix filter — the dispatcher's dedup must NOT
  // skip an entry just because the operator's `backlog: <id>` PR
  // landed. Otherwise filing an entry instantly disqualifies it
  // from being dispatched (2026-05-05 incident with PR #322 +
  // entry t0-glm-flash-smoke-confirm-end-to-end).

  it('does NOT match a `backlog:`-prefixed PR title', () => {
    expect(entryIdInRecentSubjects('my-entry', 'backlog: my-entry (filed 2026-05-05)')).toBe(false);
  });

  it('does NOT match an `auto:`-prefixed PR title (auto-flipper / sweepers)', () => {
    expect(entryIdInRecentSubjects('my-entry', 'auto: flip my-entry ready→partial')).toBe(false);
  });

  it('does NOT match a `docs:`-prefixed PR title', () => {
    expect(entryIdInRecentSubjects('my-entry', 'docs: clarify my-entry rationale')).toBe(false);
  });

  it('does NOT match a `doc:`-prefixed PR title (singular alias)', () => {
    expect(entryIdInRecentSubjects('my-entry', 'doc: my-entry footnote')).toBe(false);
  });

  it('does NOT match a `test:`-prefixed PR title', () => {
    expect(entryIdInRecentSubjects('my-entry', 'test: my-entry boundary cases')).toBe(false);
  });

  it('STILL matches when an entry-filing PR sits next to a real implementation PR', () => {
    const subjects = [
      'backlog: my-entry (filed 2026-05-05)',  // would dedup-trip without the filter
      'swarm: my-entry — closes #999',          // real implementation
    ].join('\n');
    expect(entryIdInRecentSubjects('my-entry', subjects)).toBe(true);
  });

  it('STILL matches feat:/fix:/swarm:/refactor:/perf:/chore: prefixes', () => {
    for (const prefix of ['feat', 'fix', 'swarm', 'refactor', 'perf', 'chore']) {
      expect(entryIdInRecentSubjects('my-entry', `${prefix}: my-entry — done`)).toBe(true);
    }
  });
});
