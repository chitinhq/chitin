import { describe, it, expect } from 'vitest';
import { buildHookContext } from '../src/hook-context';

describe('buildHookContext (PR #19 regression guard)', () => {
  it('preserves the caller-supplied sessionID verbatim', () => {
    const ccSessionID = '505c4216-bc0a-49d1-b512-55df4d6563c0';
    const ctx = buildHookContext(ccSessionID, '/tmp/fake-chitindir');
    expect(ctx.sessionID).toBe(ccSessionID);
  });

  it('returns the same sessionID across repeated calls with the same input', () => {
    const ccSessionID = '505c4216-bc0a-49d1-b512-55df4d6563c0';
    const a = buildHookContext(ccSessionID, '/tmp/a');
    const b = buildHookContext(ccSessionID, '/tmp/b');
    expect(a.sessionID).toBe(b.sessionID);
    expect(a.sessionID).toBe(ccSessionID);
  });

  it('produces different runIDs per call (auxiliary identifier, per-process)', () => {
    const a = buildHookContext('s-1', '/tmp/a');
    const b = buildHookContext('s-1', '/tmp/b');
    expect(a.runID).not.toBe(b.runID);
  });

  it('throws when sessionID is empty — refuse to mint a synthetic one', () => {
    expect(() => buildHookContext('', '/tmp/x')).toThrow(/sessionID required/);
  });

  it('sets surface to claude-code and plumbs chitinDir', () => {
    const ctx = buildHookContext('s-2', '/tmp/cdir');
    expect(ctx.surface).toBe('claude-code');
    expect(ctx.chitinDir).toBe('/tmp/cdir');
  });
});
