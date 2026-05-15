# `@chitin/run-sdk`

Standalone TypeScript SDK for third-party tools that want to emit
chitin-shaped run events without importing the execution kernel.

## Quickstart

```ts
import { createRun, createRunManifest } from '@chitin/run-sdk';

const run = createRun(
  createRunManifest({
    surface: 'third-party-agent',
    driver_identity: {
      user: 'red',
      machine_id: 'workstation',
      machine_fingerprint: 'a'.repeat(64),
    },
    agent_fingerprint: 'b'.repeat(64),
    labels: { source: 'sdk' },
  }),
);

run.emitEvent({
  eventType: 'session_start',
  payload: {
    cwd: process.cwd(),
    client_info: { name: 'demo-tool', version: '1.0.0' },
    model: { name: 'demo-model', provider: 'demo' },
    system_prompt_hash: '0'.repeat(64),
    tool_allowlist_hash: '0'.repeat(64),
    agent_version: '1.0.0',
  },
});

run.emitEvent({
  eventType: 'intended',
  chainId: 'tool-call-1',
  chainType: 'tool_call',
  parentChainId: run.manifest.session_id,
  payload: {
    tool_name: 'Read',
    raw_input: { path: '/tmp/input.txt' },
    action_type: 'read',
  },
});

run.finalize({
  reason: 'clean',
  totals: {
    turn_count: 1,
    tool_call_count: 1,
    total_input_tokens: 0,
    total_output_tokens: 0,
    total_duration_ms: 25,
  },
});

console.log(run.toJSONL());
```
