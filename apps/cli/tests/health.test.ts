import { describe, expect, it } from 'vitest';
import { exitCode, renderReport, type HealthReport } from '../src/commands/health';

function mkReport(overrides: Partial<HealthReport> = {}): HealthReport {
  return {
    events_total: 0,
    events_by_window: {},
    hook_failure_count: 0,
    schema_drift_count: 0,
    orphaned_chains: 0,
    ...overrides,
  };
}

describe('exitCode', () => {
  it('returns 0 when everything is zero', () => {
    expect(exitCode(mkReport())).toBe(0);
  });

  it('returns 0 when only events_total is nonzero', () => {
    expect(exitCode(mkReport({ events_total: 42 }))).toBe(0);
  });

  it('returns 1 on hook failures', () => {
    expect(exitCode(mkReport({ hook_failure_count: 1 }))).toBe(1);
  });

  it('returns 1 on schema drift', () => {
    expect(exitCode(mkReport({ schema_drift_count: 3 }))).toBe(1);
  });

  it('returns 1 when both hook failures and schema drift', () => {
    expect(exitCode(mkReport({ hook_failure_count: 2, schema_drift_count: 5 }))).toBe(1);
  });
});

describe('renderReport', () => {
  it('renders WARN on zero events_total', () => {
    const lines = renderReport(mkReport(), '/tmp/x');
    expect(lines[0]).toBe('chitin health — /tmp/x');
    expect(lines).toContain('[WARN]  events total                 0');
  });

  it('renders PASS on nonzero events_total', () => {
    const lines = renderReport(mkReport({ events_total: 5 }), '/tmp/x');
    expect(lines.some((l) => l.startsWith('[PASS]') && l.includes('events total'))).toBe(true);
  });

  it('renders one row per surface', () => {
    const r = mkReport({
      events_total: 3,
      events_by_window: { 'claude-code': 2, 'gh-actions': 1 },
    });
    const lines = renderReport(r, '/tmp/x');
    expect(lines.some((l) => l.includes('events / claude-code'))).toBe(true);
    expect(lines.some((l) => l.includes('events / gh-actions'))).toBe(true);
  });

  it('renders FAIL on hook_failure_count > 0', () => {
    const lines = renderReport(mkReport({ hook_failure_count: 4 }), '/tmp/x');
    expect(lines.some((l) => l.startsWith('[FAIL]') && l.includes('hook failures'))).toBe(true);
  });

  it('renders FAIL on schema_drift_count > 0', () => {
    const lines = renderReport(mkReport({ schema_drift_count: 2 }), '/tmp/x');
    expect(lines.some((l) => l.startsWith('[FAIL]') && l.includes('schema drift'))).toBe(true);
  });

  it('renders WARN on orphaned_chains > 0', () => {
    const lines = renderReport(mkReport({ orphaned_chains: 1 }), '/tmp/x');
    expect(lines.some((l) => l.startsWith('[WARN]') && l.includes('orphaned chains'))).toBe(true);
  });
});
