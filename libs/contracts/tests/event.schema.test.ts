import { describe, expect, it } from 'vitest';
import { EventSchema } from '../src/event.schema';
import {
  WebhookReceivedPayloadSchema,
  WebhookFailedPayloadSchema,
  SessionStuckPayloadSchema,
} from '../src/payloads.schema';

const baseEnvelope = {
  schema_version: '2' as const,
  run_id: '550e8400-e29b-41d4-a716-446655440000',
  session_id: '550e8400-e29b-41d4-a716-446655440001',
  surface: 'claude-code',
  driver_identity: {
    user: 'user@example.com',
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

describe('model_turn event', () => {
  const validEnvelopeBase = {
    schema_version: '2' as const,
    run_id: '550e8400-e29b-41d4-a716-446655440000',
    session_id: '550e8400-e29b-41d4-a716-446655440001',
    surface: 'openclaw-gateway',
    driver_identity: {
      user: 'u',
      machine_id: 'm',
      machine_fingerprint: 'a'.repeat(64),
    },
    agent_instance_id: '550e8400-e29b-41d4-a716-446655440002',
    parent_agent_id: null,
    agent_fingerprint: 'b'.repeat(64),
    chain_id: 'otel:0102030405060708090a0b0c0d0e0f10',
    chain_type: 'session' as const,
    parent_chain_id: null,
    seq: 0,
    prev_hash: null,
    this_hash: 'c'.repeat(64),
    ts: '2026-04-20T12:00:00.000Z',
    labels: { source: 'otel', dialect: 'openclaw' },
  };

  it('accepts a full model_turn payload', () => {
    const ev = {
      ...validEnvelopeBase,
      event_type: 'model_turn' as const,
      payload: {
        model_name: 'qwen2.5:0.5b',
        provider: 'ollama',
        input_tokens: 42,
        output_tokens: 17,
        session_id_external: 'sp1-fixture-session',
        duration_ms: 1500,
        cache_read_tokens: 3,
      },
    };
    expect(() => EventSchema.parse(ev)).not.toThrow();
  });

  it('accepts model_turn with only required payload fields', () => {
    const ev = {
      ...validEnvelopeBase,
      event_type: 'model_turn' as const,
      payload: {
        model_name: 'x',
        provider: 'y',
        input_tokens: 0,
        output_tokens: 0,
      },
    };
    expect(() => EventSchema.parse(ev)).not.toThrow();
  });

  it('rejects model_turn with missing required payload field', () => {
    const ev = {
      ...validEnvelopeBase,
      event_type: 'model_turn' as const,
      payload: {
        provider: 'y',
        input_tokens: 0,
        output_tokens: 0,
      },
    };
    expect(() => EventSchema.parse(ev)).toThrow();
  });

  it('rejects model_turn with empty model_name', () => {
    const ev = {
      ...validEnvelopeBase,
      event_type: 'model_turn' as const,
      payload: {
        model_name: '',
        provider: 'y',
        input_tokens: 0,
        output_tokens: 0,
      },
    };
    expect(() => EventSchema.parse(ev)).toThrow();
  });

  it('rejects model_turn with negative input_tokens', () => {
    const ev = {
      ...validEnvelopeBase,
      event_type: 'model_turn' as const,
      payload: {
        model_name: 'x',
        provider: 'y',
        input_tokens: -1,
        output_tokens: 0,
      },
    };
    expect(() => EventSchema.parse(ev)).toThrow();
  });
});

describe('WebhookReceivedPayloadSchema', () => {
  it('accepts valid minimal payload', () => {
    const p = { channel: 'telegram', webhook_type: 'message', duration_ms: 42 };
    expect(WebhookReceivedPayloadSchema.parse(p)).toEqual(p);
  });
  it('accepts optional chat_id', () => {
    const p = { channel: 'telegram', webhook_type: 'message', duration_ms: 42, chat_id: 'abc' };
    expect(WebhookReceivedPayloadSchema.parse(p)).toEqual(p);
  });
  it('rejects missing channel', () => {
    const p = { webhook_type: 'message', duration_ms: 42 };
    expect(() => WebhookReceivedPayloadSchema.parse(p)).toThrow();
  });
  it('rejects negative duration_ms', () => {
    const p = { channel: 'telegram', webhook_type: 'message', duration_ms: -1 };
    expect(() => WebhookReceivedPayloadSchema.parse(p)).toThrow();
  });
});

describe('WebhookFailedPayloadSchema', () => {
  it('accepts valid minimal payload', () => {
    const p = { channel: 'telegram', webhook_type: 'message', error_message: 'boom' };
    expect(WebhookFailedPayloadSchema.parse(p)).toEqual(p);
  });
  it('accepts optional chat_id', () => {
    const p = { channel: 'telegram', webhook_type: 'message', error_message: 'boom', chat_id: 'abc' };
    expect(WebhookFailedPayloadSchema.parse(p)).toEqual(p);
  });
  it('rejects missing error_message', () => {
    const p = { channel: 'telegram', webhook_type: 'message' };
    expect(() => WebhookFailedPayloadSchema.parse(p)).toThrow();
  });
});

describe('SessionStuckPayloadSchema', () => {
  it('accepts valid minimal payload', () => {
    const p = { state: 'awaiting_model', age_ms: 120000 };
    expect(SessionStuckPayloadSchema.parse(p)).toEqual(p);
  });
  it('accepts all optional fields', () => {
    const p = {
      state: 'awaiting_model',
      age_ms: 120000,
      session_id_external: 'sess-123',
      session_key: 'key-abc',
      queue_depth: 5,
    };
    expect(SessionStuckPayloadSchema.parse(p)).toEqual(p);
  });
  it('rejects missing state', () => {
    const p = { age_ms: 120000 };
    expect(() => SessionStuckPayloadSchema.parse(p)).toThrow();
  });
  it('rejects negative age_ms', () => {
    const p = { state: 'awaiting_model', age_ms: -1 };
    expect(() => SessionStuckPayloadSchema.parse(p)).toThrow();
  });
});
