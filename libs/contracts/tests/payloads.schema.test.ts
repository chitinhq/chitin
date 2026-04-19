import { describe, expect, it } from 'vitest';
import {
  SessionStartPayloadSchema,
  UserPromptPayloadSchema,
  AssistantTurnPayloadSchema,
  CompactionPayloadSchema,
  SessionEndPayloadSchema,
  IntendedPayloadSchema,
  ExecutedPayloadSchema,
  FailedPayloadSchema,
} from '../src/payloads.schema';

describe('SessionStartPayloadSchema', () => {
  it('accepts minimal valid payload', () => {
    const p = {
      cwd: '/home/red/workspace/chitin',
      client_info: { name: 'claude-code', version: '1.0.0' },
      model: { name: 'claude-opus-4-7', provider: 'anthropic' },
      system_prompt_hash: 'a'.repeat(64),
      tool_allowlist_hash: 'b'.repeat(64),
      agent_version: '1.0.0',
    };
    expect(() => SessionStartPayloadSchema.parse(p)).not.toThrow();
  });

  it('accepts subagent session_start with spawning_tool_call_id', () => {
    const p = {
      cwd: '/',
      client_info: { name: 'claude-code', version: '1.0.0' },
      model: { name: 'claude-opus-4-7', provider: 'anthropic' },
      system_prompt_hash: 'a'.repeat(64),
      tool_allowlist_hash: 'b'.repeat(64),
      agent_version: '1.0.0',
      spawning_tool_call_id: 'toolu_01XYZ',
      task_description: 'read files',
      soul_id: 'jokic',
      soul_hash: 'c'.repeat(64),
    };
    expect(() => SessionStartPayloadSchema.parse(p)).not.toThrow();
  });
});

describe('UserPromptPayloadSchema', () => {
  it('accepts text prompt', () => {
    expect(() => UserPromptPayloadSchema.parse({ text: 'hi' })).not.toThrow();
  });

  it('accepts prompt with attachments', () => {
    const p = { text: 'look at this', attachments: [{ kind: 'file', path: '/tmp/x.png' }] };
    expect(() => UserPromptPayloadSchema.parse(p)).not.toThrow();
  });
});

describe('AssistantTurnPayloadSchema', () => {
  it('accepts turn with usage and no thinking', () => {
    const p = {
      text: 'hello',
      model_used: { name: 'claude-opus-4-7', provider: 'anthropic' },
      usage: { input_tokens: 10, output_tokens: 5 },
      ts_start: '2026-04-19T12:00:00Z',
      ts_end: '2026-04-19T12:00:01Z',
    };
    expect(() => AssistantTurnPayloadSchema.parse(p)).not.toThrow();
  });

  it('accepts turn with thinking and cache usage', () => {
    const p = {
      text: 'hello',
      thinking: 'considering...',
      model_used: { name: 'claude-opus-4-7', provider: 'anthropic' },
      usage: {
        input_tokens: 10,
        output_tokens: 5,
        cache_creation_input_tokens: 100,
        cache_read_input_tokens: 200,
        thinking_tokens: 50,
      },
      ts_start: '2026-04-19T12:00:00Z',
      ts_end: '2026-04-19T12:00:01Z',
    };
    expect(() => AssistantTurnPayloadSchema.parse(p)).not.toThrow();
  });
});

describe('CompactionPayloadSchema', () => {
  it('accepts minimal compaction', () => {
    expect(() => CompactionPayloadSchema.parse({ reason: 'context_full' })).not.toThrow();
  });
});

describe('SessionEndPayloadSchema', () => {
  it('accepts clean end with totals', () => {
    const p = {
      reason: 'clean',
      totals: {
        turn_count: 5,
        tool_call_count: 8,
        total_input_tokens: 1000,
        total_output_tokens: 500,
        total_duration_ms: 60000,
      },
    };
    expect(() => SessionEndPayloadSchema.parse(p)).not.toThrow();
  });

  it('accepts orphaned_sweep reason', () => {
    const p = {
      reason: 'orphaned_sweep',
      totals: {
        turn_count: 0,
        tool_call_count: 0,
        total_input_tokens: 0,
        total_output_tokens: 0,
        total_duration_ms: 0,
      },
    };
    expect(() => SessionEndPayloadSchema.parse(p)).not.toThrow();
  });
});

describe('IntendedPayloadSchema', () => {
  it('accepts read action', () => {
    const p = {
      tool_name: 'Read',
      raw_input: { path: '/tmp/x' },
      action_type: 'read',
    };
    expect(() => IntendedPayloadSchema.parse(p)).not.toThrow();
  });

  it('accepts with canonical_form', () => {
    const p = {
      tool_name: 'Bash',
      raw_input: { command: 'ls -la' },
      canonical_form: { argv: ['ls', '-la'] },
      action_type: 'exec',
    };
    expect(() => IntendedPayloadSchema.parse(p)).not.toThrow();
  });

  it('rejects invalid action_type', () => {
    const p = {
      tool_name: 'X',
      raw_input: {},
      action_type: 'bogus',
    };
    expect(() => IntendedPayloadSchema.parse(p)).toThrow();
  });
});

describe('ExecutedPayloadSchema', () => {
  it('accepts minimal executed', () => {
    expect(() => ExecutedPayloadSchema.parse({ duration_ms: 42 })).not.toThrow();
  });

  it('accepts executed with output preview', () => {
    const p = { duration_ms: 42, output_preview: 'hello', output_bytes_total: 5 };
    expect(() => ExecutedPayloadSchema.parse(p)).not.toThrow();
  });
});

describe('FailedPayloadSchema', () => {
  it('accepts minimal failed', () => {
    const p = { duration_ms: 10, error_kind: 'timeout', error: 'exceeded 30s' };
    expect(() => FailedPayloadSchema.parse(p)).not.toThrow();
  });
});
