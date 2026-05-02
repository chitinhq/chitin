import { describe, expect, it } from 'vitest';
import { classifyDispatchEscalation } from '../src/dispatcher.ts';
import type { DriverId, Tier } from '@chitin/contracts';

interface MarkerOutcome {
  exit_code: number;
  commits_added: number;
  completed_at: string;
}

interface DispatchMarker {
  entry_id: string;
  workflow_id: string;
  tier: Tier;
  driver: DriverId;
  dispatched_at: string;
  last_result?: MarkerOutcome;
  tier_attempts?: Tier[];
}

function makeMarker(overrides: Partial<DispatchMarker> = {}): DispatchMarker {
  return {
    entry_id: 'sample',
    workflow_id: 'swarm-sample-1',
    tier: 'T1',
    driver: 'copilot',
    dispatched_at: '2026-05-02T18:00:00Z',
    ...overrides,
  };
}

function makeOutcome(overrides: Partial<MarkerOutcome> = {}): MarkerOutcome {
  return { exit_code: 0, commits_added: 1, completed_at: '2026-05-02T18:05:00Z', ...overrides };
}

// ─── No marker ───────────────────────────────────────────────────────────

describe('classifyDispatchEscalation — no marker', () => {
  it('returns no-marker when marker is null (first dispatch)', () => {
    expect(classifyDispatchEscalation(null)).toEqual({ kind: 'no-marker' });
  });
});

// ─── In-flight ───────────────────────────────────────────────────────────

describe('classifyDispatchEscalation — in-flight', () => {
  it('returns in-flight when marker exists but last_result is missing', () => {
    expect(classifyDispatchEscalation(makeMarker())).toEqual({ kind: 'in-flight' });
  });
});

// ─── Shipped ─────────────────────────────────────────────────────────────

describe('classifyDispatchEscalation — shipped', () => {
  it('returns shipped when commits_added > 0 (work landed; chain handles rest)', () => {
    expect(
      classifyDispatchEscalation(makeMarker({ last_result: makeOutcome({ commits_added: 1 }) })),
    ).toEqual({ kind: 'shipped' });
  });

  it('returns shipped even when exit_code != 0 — commits trump exit code', () => {
    // The "junior dev committed code, then tests failed" case. The work
    // is real; reviewer chain catches the test failure.
    expect(
      classifyDispatchEscalation(
        makeMarker({ last_result: makeOutcome({ exit_code: 1, commits_added: 2 }) }),
      ),
    ).toEqual({ kind: 'shipped' });
  });
});

// ─── Escalate ────────────────────────────────────────────────────────────

describe('classifyDispatchEscalation — escalate', () => {
  it('escalates T1 → T2 when commits=0 and only T1 attempted', () => {
    const r = classifyDispatchEscalation(
      makeMarker({
        tier: 'T1',
        last_result: makeOutcome({ exit_code: 1, commits_added: 0 }),
      }),
    );
    expect(r).toEqual({ kind: 'escalate', nextTier: 'T2' });
  });

  it('escalates T2 → T3 when prior tier_attempts is [T1, T2]', () => {
    const r = classifyDispatchEscalation(
      makeMarker({
        tier: 'T2',
        tier_attempts: ['T1', 'T2'],
        last_result: makeOutcome({ commits_added: 0 }),
      }),
    );
    expect(r).toEqual({ kind: 'escalate', nextTier: 'T3' });
  });

  it('escalates on SIGKILL outcome (exit_code=-1) when commits=0', () => {
    const r = classifyDispatchEscalation(
      makeMarker({
        tier: 'T1',
        last_result: makeOutcome({ exit_code: -1, commits_added: 0 }),
      }),
    );
    expect(r).toEqual({ kind: 'escalate', nextTier: 'T2' });
  });

  it('skips already-tried tiers when picking next (e.g., T1, T3 tried → next is T2 NOT T2 again)', () => {
    // Edge case: tier_attempts is non-monotonic. Should pick the lowest
    // un-tried tier above the most recent one.
    const r = classifyDispatchEscalation(
      makeMarker({
        tier: 'T3',
        tier_attempts: ['T1', 'T3'],
        last_result: makeOutcome({ commits_added: 0 }),
      }),
    );
    // After T3, the next-up un-tried tier is T4.
    expect(r).toEqual({ kind: 'escalate', nextTier: 'T4' });
  });
});

// ─── Exhausted ───────────────────────────────────────────────────────────

describe('classifyDispatchEscalation — exhausted', () => {
  it('returns exhausted after T4 was the last attempt + still no commits', () => {
    const r = classifyDispatchEscalation(
      makeMarker({
        tier: 'T4',
        tier_attempts: ['T1', 'T2', 'T3', 'T4'],
        last_result: makeOutcome({ commits_added: 0 }),
      }),
    );
    expect(r).toEqual({ kind: 'exhausted' });
  });

  it('returns exhausted when every tier was visited (operator-only now)', () => {
    const r = classifyDispatchEscalation(
      makeMarker({
        tier: 'T4',
        tier_attempts: ['T0', 'T1', 'T2', 'T3', 'T4'],
        last_result: makeOutcome({ commits_added: 0 }),
      }),
    );
    expect(r).toEqual({ kind: 'exhausted' });
  });
});

// ─── Sanity ───────────────────────────────────────────────────────────────

describe('classifyDispatchEscalation — sanity', () => {
  it("never returns escalate when commits > 0", () => {
    // Determinism: shipped trumps escalation regardless of tier ladder.
    const r = classifyDispatchEscalation(
      makeMarker({
        tier: 'T1',
        tier_attempts: ['T1'],
        last_result: makeOutcome({ exit_code: 1, commits_added: 1 }),
      }),
    );
    expect(r.kind).toBe('shipped');
  });

  it('treats tier as the marker.tier when tier_attempts is missing (legacy markers)', () => {
    // Markers from before the escalation feature have no tier_attempts;
    // we should still classify them via marker.tier.
    const r = classifyDispatchEscalation(
      makeMarker({
        tier: 'T2',
        tier_attempts: undefined,
        last_result: makeOutcome({ commits_added: 0 }),
      }),
    );
    expect(r).toEqual({ kind: 'escalate', nextTier: 'T3' });
  });
});
