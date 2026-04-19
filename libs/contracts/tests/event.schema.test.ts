import { describe, expect, it } from 'vitest';
import { EventSchema } from '../src/event.schema';

const baseEnvelope = {
  schema_version: '2' as const,
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
  chain_id: '550e8400-e29b-41d4-a716-446655440003',
  chain_type: 'session' as const,
  parent_chain_id: null,
  seq: 0,
  prev_hash: null,
  this_hash: 'c'.repeat(64),
  ts: '2026-04-19T12:00:00.000Z',
  labels: {},
};

describe('EventSchema (discriminated by event_type)', () => {
  it('validates a session_start event', () => {
    const e = {
      ...baseEnvelope,
      event_type: 'session_start' as const,
      payload: {
        cwd: '/tmp',
        client_info: { name: 'claude-code', version: '1.0.0' },
        model: { name: 'claude-opus-4-7', provider: 'anthropic' },
        system_prompt_hash: 'd'.repeat(64),
        tool_allowlist_hash: 'e'.repeat(64),
        agent_version: '1.0.0',
      },
    };
    expect(() => EventSchema.parse(e)).not.toThrow();
  });

  it('validates an intended event on a tool_call chain', () => {
    const e = {
      ...baseEnvelope,
      chain_type: 'tool_call' as const,
      chain_id: 'toolu_01ABC',
      parent_chain_id: '550e8400-e29b-41d4-a716-446655440003',
      event_type: 'intended' as const,
      payload: {
        tool_name: 'Read',
        raw_input: { path: '/tmp/x' },
        action_type: 'read' as const,
      },
    };
    expect(() => EventSchema.parse(e)).not.toThrow();
  });

  it('rejects a session_start with intended payload', () => {
    const e = {
      ...baseEnvelope,
      event_type: 'session_start' as const,
      payload: { tool_name: 'Read', raw_input: {}, action_type: 'read' as const },
    };
    expect(() => EventSchema.parse(e)).toThrow();
  });

  it('rejects unknown event_type', () => {
    const e = { ...baseEnvelope, event_type: 'bogus', payload: {} };
    expect(() => EventSchema.parse(e)).toThrow();
  });
});
