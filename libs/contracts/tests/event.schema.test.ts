import { describe, it, expect } from 'vitest';
import { EventSchema } from '../src/event.schema.js';
import type { ActionType } from '../src/event.types.js';

const VALID = {
  run_id: '550e8400-e29b-41d4-a716-446655440000',
  session_id: '550e8400-e29b-41d4-a716-446655440001',
  surface: 'claude-code',
  driver: 'claude',
  agent_id: 'agent-xyz',
  tool_name: 'Bash',
  raw_input: { command: 'git status' },
  canonical_form: { tool: 'git', action: 'status' },
  action_type: 'git' as ActionType,
  result: 'success' as const,
  duration_ms: 12,
  error: null,
  ts: '2026-04-19T12:00:00Z',
  metadata: {},
};

describe('EventSchema', () => {
  it('accepts a fully-populated valid event', () => {
    const parsed = EventSchema.parse(VALID);
    expect(parsed.run_id).toBe(VALID.run_id);
    expect(parsed.action_type).toBe('git');
  });

  it('rejects an event with an invalid action_type', () => {
    const bad = { ...VALID, action_type: 'magic' };
    expect(() => EventSchema.parse(bad)).toThrow();
  });

  it('rejects an event with a non-UUID run_id', () => {
    const bad = { ...VALID, run_id: 'not-a-uuid' };
    expect(() => EventSchema.parse(bad)).toThrow();
  });

  it('accepts any surface string (open enum)', () => {
    expect(() => EventSchema.parse({ ...VALID, surface: 'openclaw', driver: 'openclaw' })).not.toThrow();
    expect(() => EventSchema.parse({ ...VALID, surface: 'some-future-surface', driver: 'xyz' })).not.toThrow();
  });

  it('allows error to be null or a string', () => {
    expect(() => EventSchema.parse({ ...VALID, error: null })).not.toThrow();
    expect(() => EventSchema.parse({ ...VALID, error: 'boom', result: 'error' })).not.toThrow();
  });

  it('rejects negative duration_ms', () => {
    expect(() => EventSchema.parse({ ...VALID, duration_ms: -1 })).toThrow();
  });
});
