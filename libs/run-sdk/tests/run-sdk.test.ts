import { describe, expect, it } from 'vitest';
import { EventSchema } from '@chitin/contracts';
import { createRunManifest } from '../src/manifest';
import { createRun } from '../src/run';

function manifest() {
  return createRunManifest({
    surface: 'third-party-agent',
    driver_identity: {
      user: 'red',
      machine_id: 'workstation',
      machine_fingerprint: 'a'.repeat(64),
    },
    agent_fingerprint: 'b'.repeat(64),
    labels: { source: 'sdk' },
  });
}

describe('@chitin/run-sdk', () => {
  it('creates a run manifest with stable defaults', () => {
    const created = manifest();
    expect(created.schema_version).toBe('2');
    expect(created.parent_agent_id).toBeNull();
    expect(created.labels).toEqual({ source: 'sdk' });
  });

  it('emits chain-valid events with seq and prev_hash progression', () => {
    const run = createRun(manifest());

    const started = run.emitEvent({
      eventType: 'session_start',
      payload: {
        cwd: '/tmp',
        client_info: { name: 'sdk-test', version: '1.0.0' },
        model: { name: 'demo', provider: 'demo' },
        system_prompt_hash: '0'.repeat(64),
        tool_allowlist_hash: '1'.repeat(64),
        agent_version: '1.0.0',
      },
    });
    const intended = run.emitEvent({
      eventType: 'intended',
      chainId: 'tool-call-1',
      chainType: 'tool_call',
      payload: {
        tool_name: 'Read',
        raw_input: { path: '/tmp/input.txt' },
        action_type: 'read',
      },
    });
    const ended = run.finalize({
      reason: 'clean',
      totals: {
        turn_count: 1,
        tool_call_count: 1,
        total_input_tokens: 0,
        total_output_tokens: 0,
        total_duration_ms: 20,
      },
    });

    expect(EventSchema.parse(started)).toEqual(started);
    expect(EventSchema.parse(intended)).toEqual(intended);
    expect(EventSchema.parse(ended)).toEqual(ended);

    expect(started.seq).toBe(0);
    expect(started.prev_hash).toBeNull();
    expect(intended.seq).toBe(0);
    expect(intended.parent_chain_id).toBe(run.manifest.session_id);
    expect(ended.seq).toBe(1);
    expect(ended.prev_hash).toBe(started.this_hash);
    expect(run.events).toHaveLength(3);
  });

  it('serializes emitted events as JSONL', () => {
    const run = createRun(manifest());
    run.emitEvent({
      eventType: 'session_start',
      payload: {
        cwd: '/tmp',
        client_info: { name: 'sdk-test', version: '1.0.0' },
        model: { name: 'demo', provider: 'demo' },
        system_prompt_hash: '0'.repeat(64),
        tool_allowlist_hash: '1'.repeat(64),
        agent_version: '1.0.0',
      },
    });

    expect(run.toJSONL()).toContain('"event_type":"session_start"');
  });

  const sessionStartPayload = {
    cwd: '/tmp',
    client_info: { name: 'sdk-test', version: '1.0.0' },
    model: { name: 'demo', provider: 'demo' },
    system_prompt_hash: '0'.repeat(64),
    tool_allowlist_hash: '1'.repeat(64),
    agent_version: '1.0.0',
  };

  it('empty boundary: a run with no events emits empty JSONL', () => {
    const run = createRun(manifest());
    expect(run.events).toHaveLength(0);
    expect(run.toJSONL()).toBe('');
  });

  it('max boundary: a long chain keeps strict seq + prev_hash progression', () => {
    const run = createRun(manifest());
    let prev = run.emitEvent({ eventType: 'session_start', payload: sessionStartPayload });
    for (let i = 1; i < 200; i++) {
      const next = run.emitEvent({ eventType: 'session_start', payload: sessionStartPayload });
      expect(next.seq).toBe(i);
      expect(next.prev_hash).toBe(prev.this_hash);
      prev = next;
    }
    expect(run.events).toHaveLength(200);
  });

  it('error boundary: emitEvent after finalize throws, and events are frozen', () => {
    const run = createRun(manifest());
    const started = run.emitEvent({ eventType: 'session_start', payload: sessionStartPayload });
    // Emitted events are frozen — a mutation must not be able to make
    // toJSONL() diverge from this_hash.
    expect(() => {
      (started as { seq: number }).seq = 999;
    }).toThrow();
    run.finalize({
      reason: 'clean',
      totals: {
        turn_count: 1,
        tool_call_count: 0,
        total_input_tokens: 0,
        total_output_tokens: 0,
        total_duration_ms: 1,
      },
    });
    expect(() =>
      run.emitEvent({ eventType: 'assistant_turn', payload: { text: 'after end' } }),
    ).toThrow(/finalized/);
  });

  it('hashes the schema-normalized payload, not caller-supplied extra keys', () => {
    const run = createRun(manifest());
    const event = run.emitEvent({
      eventType: 'session_start',
      // `bogus` is not in the session_start payload schema — Zod strips it,
      // and this_hash must still verify against the stored (stripped) event.
      payload: { ...sessionStartPayload, bogus: 'should-be-stripped' } as typeof sessionStartPayload,
    });
    expect((event.payload as Record<string, unknown>).bogus).toBeUndefined();
    expect(EventSchema.parse(event)).toEqual(event);
  });
});
