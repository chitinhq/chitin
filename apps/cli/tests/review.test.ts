import { describe, expect, it } from 'vitest';
import { parseWindow, renderHealth } from '../src/commands/review';
import type { HealthReport } from '../src/commands/health';

function mkReport(overrides: Partial<HealthReport> = {}): HealthReport {
  return {
    events_total: 0,
    events_by_window: {},
    hook_failure_count: 0,
    schema_drift_count: 0,
    orphaned_chains: 0,
    dir_exists: true,
    clock_skew_suspected: false,
    ...overrides,
  };
}

describe('parseWindow', () => {
  it('parses hours', () => {
    expect(parseWindow('24h')).toBe(24);
    expect(parseWindow('1h')).toBe(1);
  });

  it('parses days to hours', () => {
    expect(parseWindow('7d')).toBe(168);
    expect(parseWindow('1d')).toBe(24);
  });

  it('throws on malformed input', () => {
    expect(() => parseWindow('7')).toThrow(/bad window/);
    expect(() => parseWindow('1w')).toThrow(/bad window/);
    expect(() => parseWindow('')).toThrow(/bad window/);
  });
});

describe('renderHealth', () => {
  it('includes the chitin dir in the header', () => {
    const lines = renderHealth(mkReport(), '/tmp/chit');
    expect(lines[0]).toBe('## Health (/tmp/chit)');
  });

  it('renders MISSING when dir absent and skips metrics', () => {
    const lines = renderHealth(mkReport({ dir_exists: false }), '/nope');
    expect(lines).toEqual(['## Health (/nope)', '- chitin dir:        MISSING']);
  });

  it('renders one row per surface', () => {
    const r = mkReport({
      events_total: 4,
      events_by_window: { 'claude-code': 3, 'gh-actions': 1 },
    });
    const lines = renderHealth(r, '/tmp/x');
    expect(lines.some((l) => l.includes('events / claude-code'))).toBe(true);
    expect(lines.some((l) => l.includes('events / gh-actions'))).toBe(true);
  });

  it('renders the four core metrics', () => {
    const r = mkReport({
      events_total: 9,
      hook_failure_count: 2,
      schema_drift_count: 1,
      orphaned_chains: 3,
    });
    const lines = renderHealth(r, '/tmp/x');
    expect(lines).toContain('- events total:      9');
    expect(lines).toContain('- hook failures:     2');
    expect(lines).toContain('- schema drift:      1');
    expect(lines).toContain('- orphaned chains:   3');
  });
});
