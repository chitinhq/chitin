import { describe, expect, it } from 'vitest';
import { buildAdapterContext } from '../src/ctx';

describe('buildAdapterContext', () => {
  it('produces stable envelope base with labels merged', () => {
    const ctx = buildAdapterContext({
      surface: 'claude-code',
      chitinDir: '/tmp/x/.chitin',
      user: 'jared',
      machineID: 'test-box',
      labelsCli: { env: 'dev' },
      labelsProject: { project: 'chitin' },
    });
    expect(ctx.surface).toBe('claude-code');
    expect(ctx.runID).toMatch(/^[0-9a-f-]{36}$/);
    expect(ctx.sessionID).toMatch(/^[0-9a-f-]{36}$/);
    expect(ctx.agentInstanceID).toMatch(/^[0-9a-f-]{36}$/);
    expect(ctx.labels).toEqual({ env: 'dev', project: 'chitin' });
    expect(ctx.driverIdentity.user).toBe('jared');
    expect(ctx.driverIdentity.machine_fingerprint).toMatch(/^[a-f0-9]{64}$/);
    expect(ctx.agentFingerprint).toMatch(/^[a-f0-9]{64}$/);
  });

  it('cli labels override project labels on key conflict', () => {
    const ctx = buildAdapterContext({
      surface: 'claude-code',
      chitinDir: '/tmp/x/.chitin',
      user: 'u', machineID: 'm',
      labelsCli: { env: 'prod' },
      labelsProject: { env: 'dev', project: 'x' },
    });
    expect(ctx.labels.env).toBe('prod');
    expect(ctx.labels.project).toBe('x');
  });
});
