import { describe, expect, it } from 'vitest';
import {
  REVIEW_TIER_DRIVER,
  computeStartingTier,
  escalateOneTier,
  shouldEscalateToOperator,
  __test__,
  type PrMeta,
  type ReviewTier,
  type ReviewerOutput,
} from '../src/review-graph.ts';
import type { BacklogEntry } from '../src/grooming/parse-backlog.ts';

const { isT5Shape, isKernelInternal, isSchemaFile, maxTier } = __test__;

function makeEntry(overrides: Partial<BacklogEntry> = {}): BacklogEntry {
  return {
    id: 'e1',
    status: 'ready',
    description: 'desc',
    rawFrontmatter: '```yaml\nid: e1\nstatus: ready\n```',
    rawSection: '### e1',
    tier: 'T1',
    ...overrides,
  };
}

function makePrMeta(overrides: Partial<PrMeta> = {}): PrMeta {
  return {
    diff_loc: 50,
    files_changed: 2,
    files: ['apps/runner/src/foo.ts'],
    ...overrides,
  };
}

// ─── escalateOneTier ─────────────────────────────────────────────────────

describe('escalateOneTier', () => {
  it.each([
    ['R0', 'R1'],
    ['R1', 'R2'],
    ['R2', 'R3'],
    ['R3', 'R4'],
    ['R4', 'R4'],  // saturates
  ] as [ReviewTier, ReviewTier][])('%s -> %s', (input, expected) => {
    expect(escalateOneTier(input)).toBe(expected);
  });
});

// ─── REVIEW_TIER_DRIVER shape ────────────────────────────────────────────

describe('REVIEW_TIER_DRIVER', () => {
  it('has exactly 5 entries (R0-R4)', () => {
    expect(Object.keys(REVIEW_TIER_DRIVER).sort()).toEqual(['R0', 'R1', 'R2', 'R3', 'R4']);
  });

  it('R0 (Copilot bot) and R4 (operator) are non-dispatchable (driver/model null)', () => {
    expect(REVIEW_TIER_DRIVER.R0).toEqual({ driver: null, model: null });
    expect(REVIEW_TIER_DRIVER.R4).toEqual({ driver: null, model: null });
  });

  it('R1-R3 each have a real driver+model', () => {
    for (const t of ['R1', 'R2', 'R3'] as const) {
      expect(REVIEW_TIER_DRIVER[t].driver).toBeTruthy();
      expect(REVIEW_TIER_DRIVER[t].model).toBeTruthy();
    }
  });

  it('R3 routes to claude-code-headless + opus (the heavy-reviewer tier)', () => {
    expect(REVIEW_TIER_DRIVER.R3.driver).toBe('claude-code-headless');
    expect(REVIEW_TIER_DRIVER.R3.model).toContain('opus');
  });
});

// ─── path-classification helpers ─────────────────────────────────────────

describe('isT5Shape', () => {
  it.each([
    ['chitin.yaml', true],
    ['apps/some/chitin.yaml', true],
    ['.chitin/policy.yaml', true],
    ['some/.chitin/anything.txt', true],
    // The chitin.yaml `no-governance-self-modification` regex covers
    // `.hermes/plugins/chitin-governance/` too; mirror that here so a
    // hole in one is a hole in the other.
    ['.hermes/plugins/chitin-governance/manifest.json', true],
    ['some/.hermes/plugins/chitin-governance/anything.ts', true],
  ])('matches %s as T5-shape', (path, expected) => {
    expect(isT5Shape(path)).toBe(expected);
  });

  it.each([
    'apps/runner/src/dispatcher.ts',
    'libs/contracts/src/execution-request.schema.ts',
    'docs/swarm-backlog.md',
    'README.md',
    'chitin.config.ts',  // similar prefix, not the actual file
  ])('does not match unrelated path %s', (path) => {
    expect(isT5Shape(path)).toBe(false);
  });
});

describe('isKernelInternal', () => {
  it.each([
    ['go/execution-kernel/internal/gov/normalize.go', true],
    ['go/execution-kernel/internal/canon/normalize.go', true],
    ['go/execution-kernel/internal/govhookinstall/install.go', true],
    ['go/execution-kernel/internal/hookinstall/install.go', true],
    ['go/execution-kernel/internal/driver/copilot/handler.go', true],
    ['go/execution-kernel/internal/normalize/normalize.go', true],
  ])('matches %s as kernel internal', (path, expected) => {
    expect(isKernelInternal(path)).toBe(expected);
  });

  it.each([
    'go/execution-kernel/cmd/chitin-kernel/main.go',
    'apps/runner/src/dispatcher.ts',
    'libs/contracts/src/event.schema.ts',
  ])('does not match non-kernel-internal path %s', (path) => {
    expect(isKernelInternal(path)).toBe(false);
  });
});

describe('isSchemaFile', () => {
  it('matches libs/contracts schema files', () => {
    expect(isSchemaFile('libs/contracts/src/execution-request.schema.ts')).toBe(true);
    expect(isSchemaFile('libs/contracts/src/event.schema.ts')).toBe(true);
  });

  it('does not match non-schema files in libs/contracts', () => {
    expect(isSchemaFile('libs/contracts/src/index.ts')).toBe(false);
    expect(isSchemaFile('libs/contracts/src/hash.ts')).toBe(false);
  });

  it('does not match schema files outside libs/contracts', () => {
    expect(isSchemaFile('apps/foo/something.schema.ts')).toBe(false);
  });
});

// ─── computeStartingTier — §5 trigger matrix, one row per case ───────────

describe('computeStartingTier — default cases', () => {
  it('returns R0 for a tiny diff with no special signals (the cheap-merge case)', () => {
    const out = computeStartingTier(makePrMeta(), makeEntry());
    expect(out.tier).toBe('R0');
    expect(out.t5_shape).toBe(false);
    expect(out.reasons).toEqual([]);
  });

  it('returns R0 for an empty file list (apply step couldn\'t enumerate; defer to size signals)', () => {
    const out = computeStartingTier(makePrMeta({ files: [], diff_loc: 5, files_changed: 0 }), makeEntry());
    expect(out.tier).toBe('R0');
  });
});

describe('computeStartingTier — Copilot comment trigger (R1)', () => {
  it('does not bump on 0/1/2 Copilot comments', () => {
    for (const n of [0, 1, 2]) {
      const out = computeStartingTier(makePrMeta({ copilot_comment_count: n }), makeEntry());
      expect(out.tier).toBe('R0');
    }
  });

  it('bumps to R1 on 3+ Copilot comments', () => {
    const out = computeStartingTier(makePrMeta({ copilot_comment_count: 3 }), makeEntry());
    expect(out.tier).toBe('R1');
    expect(out.reasons.some((r) => r.includes('Copilot') && r.includes('3'))).toBe(true);
  });

  it('bumps to R1 on a high comment count without conflict', () => {
    const out = computeStartingTier(makePrMeta({ copilot_comment_count: 12 }), makeEntry());
    expect(out.tier).toBe('R1');
  });

  it('does not regress when copilot_comment_count is undefined (unknown ≠ trigger)', () => {
    const out = computeStartingTier(makePrMeta({ copilot_comment_count: undefined }), makeEntry());
    expect(out.tier).toBe('R0');
  });
});

describe('computeStartingTier — diff size triggers (R2 mid, R3 high)', () => {
  it('bumps to R2 at 201 LOC (just over mid threshold)', () => {
    const out = computeStartingTier(makePrMeta({ diff_loc: 201, files_changed: 5 }), makeEntry());
    expect(out.tier).toBe('R2');
    expect(out.reasons.some((r) => r.includes('mid-size'))).toBe(true);
  });

  it('does NOT bump at exactly 200 LOC (boundary — > 200 is the rule)', () => {
    const out = computeStartingTier(makePrMeta({ diff_loc: 200, files_changed: 5 }), makeEntry());
    expect(out.tier).toBe('R0');
  });

  it('bumps to R2 at 11 files (just over mid threshold)', () => {
    const out = computeStartingTier(makePrMeta({ diff_loc: 50, files_changed: 11 }), makeEntry());
    expect(out.tier).toBe('R2');
    expect(out.reasons.some((r) => r.includes('mid-width'))).toBe(true);
  });

  it('bumps to R3 at 501 LOC (just over high threshold)', () => {
    const out = computeStartingTier(makePrMeta({ diff_loc: 501, files_changed: 5 }), makeEntry());
    expect(out.tier).toBe('R3');
  });

  it('bumps to R3 at 21 files (just over high threshold)', () => {
    const out = computeStartingTier(makePrMeta({ diff_loc: 50, files_changed: 21 }), makeEntry());
    expect(out.tier).toBe('R3');
  });

  it('attributes R3 to LOC when both LOC and files cross the high threshold', () => {
    const out = computeStartingTier(makePrMeta({ diff_loc: 600, files_changed: 25 }), makeEntry());
    expect(out.tier).toBe('R3');
    // LOC is checked first in the high-cutoff branch — reason mentions LOC
    expect(out.reasons.some((r) => r.includes('large diff'))).toBe(true);
  });
});

describe('computeStartingTier — file-scope triggers', () => {
  it('bumps to R2 minimum when touching a schema file', () => {
    const out = computeStartingTier(
      makePrMeta({ files: ['libs/contracts/src/execution-request.schema.ts'] }),
      makeEntry(),
    );
    expect(out.tier).toBe('R2');
    expect(out.reasons.some((r) => r.includes('schema'))).toBe(true);
  });

  it('bumps to R3 minimum when touching kernel internals (overrides R2 schema bump)', () => {
    const out = computeStartingTier(
      makePrMeta({
        files: [
          'libs/contracts/src/execution-request.schema.ts',
          'go/execution-kernel/internal/gov/normalize.go',
        ],
      }),
      makeEntry(),
    );
    expect(out.tier).toBe('R3');
    expect(out.reasons.some((r) => r.includes('kernel internals'))).toBe(true);
  });

  it('flags t5_shape on chitin.yaml without bumping tier (gatekeeper handles T5)', () => {
    const out = computeStartingTier(
      makePrMeta({ files: ['chitin.yaml'] }),
      makeEntry(),
    );
    expect(out.t5_shape).toBe(true);
  });

  it('flags t5_shape on .chitin/ paths', () => {
    const out = computeStartingTier(
      makePrMeta({ files: ['.chitin/something.yaml', 'apps/runner/src/foo.ts'] }),
      makeEntry(),
    );
    expect(out.t5_shape).toBe(true);
  });

  it('does not flag t5_shape on schema files (those are R2 bump, not governance-self-mod)', () => {
    const out = computeStartingTier(
      makePrMeta({ files: ['libs/contracts/src/execution-request.schema.ts'] }),
      makeEntry(),
    );
    expect(out.t5_shape).toBe(false);
  });
});

describe('computeStartingTier — implementor-tier trigger', () => {
  it.each(['T3', 'T4'] as const)('bumps to R2 when entry.tier = %s', (entryTier) => {
    const out = computeStartingTier(makePrMeta(), makeEntry({ tier: entryTier }));
    expect(out.tier).toBe('R2');
    expect(out.reasons.some((r) => r.includes(entryTier))).toBe(true);
  });

  it.each(['T0', 'T1', 'T2'] as const)('does not bump for entry.tier = %s', (entryTier) => {
    const out = computeStartingTier(makePrMeta(), makeEntry({ tier: entryTier }));
    expect(out.tier).toBe('R0');
  });
});

describe('computeStartingTier — multiple triggers compose to maximum', () => {
  it('takes the max across multiple bump rules', () => {
    // Schema (R2) + kernel-internals (R3) + T3 (R2) + 600 LOC (R3) + 4 comments (R1)
    // → max = R3
    const out = computeStartingTier(
      makePrMeta({
        diff_loc: 600,
        files_changed: 8,
        copilot_comment_count: 4,
        files: [
          'libs/contracts/src/execution-request.schema.ts',
          'go/execution-kernel/internal/gov/normalize.go',
        ],
      }),
      makeEntry({ tier: 'T3' }),
    );
    expect(out.tier).toBe('R3');
    // All five rules should have contributed — reasons logs each
    expect(out.reasons.length).toBeGreaterThanOrEqual(4);
  });

  it('R0 starting tier carries empty reasons (silent-default audit signal)', () => {
    const out = computeStartingTier(makePrMeta(), makeEntry());
    expect(out.tier).toBe('R0');
    expect(out.reasons).toEqual([]);
  });
});

// ─── shouldEscalateToOperator ─────────────────────────────────────────────

describe('shouldEscalateToOperator', () => {
  function out(overrides: Partial<ReviewerOutput> = {}): ReviewerOutput {
    return {
      decision: 'approve',
      confidence: 'high',
      findings: [],
      ...overrides,
    };
  }

  it('returns false for clean approve+high', () => {
    expect(shouldEscalateToOperator(out())).toBe(false);
  });

  it('returns false for request_changes+high (handled by escalate-tier, not operator)', () => {
    expect(shouldEscalateToOperator(out({ decision: 'request_changes' }))).toBe(false);
  });

  it('returns true on explicit decision: escalate', () => {
    expect(shouldEscalateToOperator(out({ decision: 'escalate' }))).toBe(true);
  });

  it('returns true on confidence: low alone (R3 couldn\'t decide → operator)', () => {
    expect(shouldEscalateToOperator(out({ confidence: 'low' }))).toBe(true);
  });

  it('returns true on confidence: low + 🔴 finding', () => {
    expect(
      shouldEscalateToOperator(
        out({
          confidence: 'low',
          findings: [{ severity: '🔴', file: 'x.ts', category: 'bug', summary: 's' }],
        }),
      ),
    ).toBe(true);
  });

  it('returns true on confidence: low + only 🟡/🟢 findings (low alone fires regardless of finding severity)', () => {
    expect(
      shouldEscalateToOperator(
        out({
          confidence: 'low',
          findings: [
            { severity: '🟡', file: 'x.ts', category: 'design', summary: 'nit' },
          ],
        }),
      ),
    ).toBe(true);
  });

  it('returns false on confidence: medium + 🔴 finding (medium = trust the reviewer; implementor re-runs)', () => {
    expect(
      shouldEscalateToOperator(
        out({
          confidence: 'medium',
          findings: [{ severity: '🔴', file: 'x.ts', category: 'bug', summary: 's' }],
        }),
      ),
    ).toBe(false);
  });

  it('returns false on confidence: high + 🔴 finding + request_changes (request_changes loops to implementor, not operator)', () => {
    expect(
      shouldEscalateToOperator(
        out({
          decision: 'request_changes',
          confidence: 'high',
          findings: [{ severity: '🔴', file: 'x.ts', category: 'bug', summary: 's' }],
        }),
      ),
    ).toBe(false);
  });
});

// ─── maxTier helper ──────────────────────────────────────────────────────

describe('maxTier', () => {
  it.each([
    ['R0', 'R0', 'R0'],
    ['R0', 'R3', 'R3'],
    ['R3', 'R0', 'R3'],
    ['R2', 'R3', 'R3'],
    ['R4', 'R4', 'R4'],
  ] as [ReviewTier, ReviewTier, ReviewTier][])('maxTier(%s, %s) = %s', (a, b, expected) => {
    expect(maxTier(a, b)).toBe(expected);
  });
});
