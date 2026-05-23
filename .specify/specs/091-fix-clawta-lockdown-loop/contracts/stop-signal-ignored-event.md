# Contract: `stop_signal_ignored` chain event (FR-009 telemetry)

**Feature**: 091-fix-clawta-lockdown-loop · **Phase**: 1 (Design & Contracts)

New chain event type emitted when the openclaw plugin's forced-continuation counter exceeds the cap (default N=3) for a single session.

## Schema

Within the standard chain frame (chain_id, seq, prev_hash, this_hash, ts, agent_instance_id, etc.):

```json
{
  "event_type": "stop_signal_ignored",
  "agent_instance_id": "<the agent id from openclaw context>",
  "session_id": "openclaw-<agentId>-<pid>",
  "payload": {
    "continuation_count": 3,
    "cap": 3,
    "last_rule_id": "<rule that triggered the final deny>",
    "last_reason": "<human-readable reason from the last deny>",
    "first_deny_ts": "<ISO 8601 timestamp of the first deny in this session>"
  },
  "ts": "<ISO 8601 emission timestamp>"
}
```

| Field | Type | Notes |
|---|---|---|
| `event_type` | `"stop_signal_ignored"` | Constant — distinguishes from `lockdown_loop_detected` |
| `agent_instance_id` | string | Standard chain field |
| `session_id` | string | The plugin-side `sessionId` that hit the cap |
| `payload.continuation_count` | number | How many forced continuations occurred (== cap on emission) |
| `payload.cap` | number | The configured cap (defaults to 3) |
| `payload.last_rule_id` | string | The rule that triggered the final deny |
| `payload.last_reason` | string | The reason from the final deny |
| `payload.first_deny_ts` | ISO 8601 | When the counter started (1st deny) |

## Distinction from `lockdown_loop_detected`

| Event | Emitted by | Trigger | Meaning |
|---|---|---|---|
| `lockdown_loop_detected` | kernel (chitin-kernel) | kernel-side: 3+ deny events for same rule within 90s window | A rule keeps firing — harness might be ignoring stops |
| `stop_signal_ignored` | plugin (via kernel emit subprocess) | plugin-side: forced-continuation counter ≥ cap | The plugin saw 3+ retries despite returning block — the agent harness isn't stopping |

Both can fire for the same incident. `lockdown_loop_detected` is the kernel's "I noticed" signal; `stop_signal_ignored` is the plugin's "I tried to stop it" signal. Operators get two telemetry sources, helping triangulate where the failure lives.

## Emission mechanism

The plugin's `index.mjs` calls a helper:

```js
async function emitStopSignalIgnored(args) {
  const event = {
    event_type: 'stop_signal_ignored',
    agent_instance_id: args.agentId,
    session_id: args.sessionId,
    payload: { ...args.payload },
    ts: new Date().toISOString(),
  };
  // Subprocess to `chitin-kernel emit --event-json -` (or whatever the emit API is)
  // The kernel writes the chain frame and returns.
}
```

This preserves constitution §1 (kernel is only chain-writer). The plugin originates the event but the kernel writes it.

## Operator-facing consumption

After this spec lands, operators can query the chain:

```bash
grep -h '"event_type":"stop_signal_ignored"' ~/.chitin/events-*.jsonl | jq -r '.session_id'
```

This is the empirical signal that the gate-bypass attack is being prevented. Per SC-002 (zero `lockdown_loop_detected` events over 48h normal operation), this event type should similarly trend to zero — but it's a stricter trigger and may surface incidents the kernel-side counter misses.

## Implementation phase verification

T-tests in `bridge.test.ts`:
- Trigger 3 sequential block decisions; assert exactly one `stop_signal_ignored` event emitted
- Trigger 2 sequential blocks then an allow; assert ZERO `stop_signal_ignored` events (counter doesn't reach cap)
- Trigger a lockdown deny (`continue:false`); assert NO `stop_signal_ignored` (different path — stop is set on first deny, cap never reached)

## Source of truth

`apps/openclaw-plugin-governance/src/index.mjs` (post-fix `emitStopSignalIgnored` helper). Event consumed by `~/.chitin/events-*.jsonl` files via the standard chain emit path.
