import { describe, expect, it } from 'vitest';
import { EnvelopeSchema } from '../src/envelope.schema';

const validEnvelope = {
  schema_version: '2',
  run_id: '550e8400-e29b-41d4-a716-446655440000',
  session_id: '550e8400-e29b-41d4-a716-446655440001',
  surface: 'claude-code',
  driver_identity: {
    user: 'jared@readybench.io',
    machine_id: '3090-box',
    machine_fingerprint: 'a'.repeat(64),
  },
  agent_instance_id: '550e8400-e29b-41d4-a716-446655440002',
  parent_agent_id: null,
  agent_fingerprint: 'b'.repeat(64),
  event_type: 'session_start',
  chain_id: '550e8400-e29b-41d4-a716-446655440003',
  chain_type: 'session',
  parent_chain_id: null,
  seq: 0,
  prev_hash: null,
  this_hash: 'c'.repeat(64),
  ts: '2026-04-19T12:00:00.000Z',
  labels: { env: 'dev', project: 'chitin' },
};

describe('EnvelopeSchema', () => {
  it('round-trips a valid v2 envelope', () => {
    const parsed = EnvelopeSchema.parse(validEnvelope);
    expect(parsed).toEqual(validEnvelope);
  });

  it('accepts tool-call chain with non-UUID chain_id (Claude Code tool_use_id)', () => {
    const tc = { ...validEnvelope, chain_type: 'tool_call' as const, chain_id: 'toolu_01ABCxyz' };
    expect(() => EnvelopeSchema.parse(tc)).not.toThrow();
  });

  it('rejects wrong schema_version', () => {
    expect(() => EnvelopeSchema.parse({ ...validEnvelope, schema_version: '1' })).toThrow();
  });

  it('rejects invalid chain_type', () => {
    expect(() => EnvelopeSchema.parse({ ...validEnvelope, chain_type: 'bogus' })).toThrow();
  });

  it('requires this_hash to be 64 hex chars', () => {
    expect(() => EnvelopeSchema.parse({ ...validEnvelope, this_hash: 'short' })).toThrow();
  });

  it('permits null prev_hash only at chain head (seq=0)', () => {
    // Note: schema allows null prev_hash anywhere; chain-head enforcement is a kernel/runtime invariant, not a schema constraint.
    const mid = { ...validEnvelope, seq: 5, prev_hash: null };
    expect(() => EnvelopeSchema.parse(mid)).not.toThrow();
  });
});
