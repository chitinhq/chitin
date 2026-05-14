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
});
