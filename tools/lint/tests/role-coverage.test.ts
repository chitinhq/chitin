import { describe, expect, it } from 'vitest';
import { findRoleCoverageGaps } from '../role-coverage.ts';

describe('findRoleCoverageGaps', () => {
  it('zero schema, zero prompts → no gaps', () => {
    const gaps = findRoleCoverageGaps(new Set(), new Set());
    expect(gaps.missingPrompts).toEqual([]);
    expect(gaps.orphanedPrompts).toEqual([]);
  });

  it('symmetric coverage → no gaps', () => {
    const roles = new Set(['programmer', 'reviewer', 'qa']);
    const prompts = new Set(['programmer', 'reviewer', 'qa']);
    const gaps = findRoleCoverageGaps(roles, prompts);
    expect(gaps.missingPrompts).toEqual([]);
    expect(gaps.orphanedPrompts).toEqual([]);
  });

  it('schema role missing from prompts → reported as missingPrompts', () => {
    const roles = new Set(['programmer', 'reviewer']);
    const prompts = new Set(['programmer']);
    const gaps = findRoleCoverageGaps(roles, prompts);
    expect(gaps.missingPrompts).toEqual(['reviewer']);
    expect(gaps.orphanedPrompts).toEqual([]);
  });

  it('prompt key not in schema → reported as orphanedPrompts', () => {
    const roles = new Set(['programmer']);
    const prompts = new Set(['programmer', 'old-stale-role']);
    const gaps = findRoleCoverageGaps(roles, prompts);
    expect(gaps.missingPrompts).toEqual([]);
    expect(gaps.orphanedPrompts).toEqual(['old-stale-role']);
  });

  it('both gaps simultaneously', () => {
    const roles = new Set(['programmer', 'comment-responder']);
    const prompts = new Set(['programmer', 'old-role']);
    const gaps = findRoleCoverageGaps(roles, prompts);
    expect(gaps.missingPrompts).toEqual(['comment-responder']);
    expect(gaps.orphanedPrompts).toEqual(['old-role']);
  });

  it('output is sorted (deterministic)', () => {
    const roles = new Set(['zeta', 'alpha', 'mu']);
    const prompts = new Set();
    const gaps = findRoleCoverageGaps(roles, prompts);
    expect(gaps.missingPrompts).toEqual(['alpha', 'mu', 'zeta']);
  });

  it('expected role vocabulary as of 2026-05-03 (snapshot)', () => {
    // Documentation-style snapshot — fixes the role list at a known
    // point in time. NOT a live comparison against RoleSchema or
    // ROLE_PROMPTS (those live in production code; the live check is
    // the linter's job, run via
    // `nx run @chitin/tooling-lint:lint:role-coverage`).
    //
    // The snapshot's job is to make any *change* to the role
    // vocabulary surface as a test diff — adding a role here forces
    // a deliberate test edit, which forces author-time attention.
    const expectedRoles = [
      'researcher',
      'product',
      'groomer',
      'architect',
      'programmer',
      'reviewer',
      'peer-reviewer',
      'comment-responder',
      'qa',
      'gatekeeper',
      'tech-writer',
      'analyst',
      'refactorer',
      'debt-curator',
    ];
    expect(expectedRoles.length).toBe(14);
    // Self-symmetric — sanity for the snapshot itself, not a real
    // drift check.
    const gaps = findRoleCoverageGaps(new Set(expectedRoles), new Set(expectedRoles));
    expect(gaps.missingPrompts).toEqual([]);
    expect(gaps.orphanedPrompts).toEqual([]);
  });
});

describe('findRoleCoverageGaps — partition invariant', () => {
  // Knuth-style: every input role/prompt key is accounted for in
  // exactly one bucket — matched (implicit by exclusion from gaps),
  // missingPrompts, or orphanedPrompts. No silent drops.
  it('every input is partitioned exactly once', () => {
    const roles = new Set(['a', 'b', 'c']);
    const prompts = new Set(['b', 'c', 'd']);
    const gaps = findRoleCoverageGaps(roles, prompts);
    const matched = new Set([...roles].filter((r) => prompts.has(r)));
    const allInputs = new Set([...roles, ...prompts]);
    const accountedFor = new Set([
      ...matched,
      ...gaps.missingPrompts,
      ...gaps.orphanedPrompts,
    ]);
    expect(accountedFor).toEqual(allInputs);
  });
});
