import { describe, expect, it } from 'vitest';
import { RunManifestSchema } from '../src/run-manifest';

describe('RunManifestSchema', () => {
  it('accepts a valid manifest', () => {
    const manifest = {
      schema_version: '2' as const,
      run_id: '550e8400-e29b-41d4-a716-446655440000',
      session_id: '550e8400-e29b-41d4-a716-446655440001',
      surface: 'third-party-agent',
      driver_identity: {
        user: 'red',
        machine_id: 'workstation',
        machine_fingerprint: 'a'.repeat(64),
      },
      agent_instance_id: '550e8400-e29b-41d4-a716-446655440002',
      parent_agent_id: null,
      agent_fingerprint: 'b'.repeat(64),
      labels: { source: 'sdk' },
    };

    expect(RunManifestSchema.parse(manifest)).toEqual(manifest);
  });

  it('rejects invalid fingerprints', () => {
    expect(() =>
      RunManifestSchema.parse({
        schema_version: '2',
        run_id: '550e8400-e29b-41d4-a716-446655440000',
        session_id: '550e8400-e29b-41d4-a716-446655440001',
        surface: 'third-party-agent',
        driver_identity: {
          user: 'red',
          machine_id: 'workstation',
          machine_fingerprint: 'short',
        },
        agent_instance_id: '550e8400-e29b-41d4-a716-446655440002',
        parent_agent_id: null,
        agent_fingerprint: 'b'.repeat(64),
        labels: {},
      }),
    ).toThrow();
  });
});
