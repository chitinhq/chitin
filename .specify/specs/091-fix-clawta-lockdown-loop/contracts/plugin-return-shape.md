# Contract: Plugin → openclaw Handler Return Shape

**Feature**: 091-fix-clawta-lockdown-loop · **Phase**: 1 (Design & Contracts)

The return value of `before_tool_call` handlers registered with `api.on(...)` in `apps/openclaw-plugin-governance/src/index.mjs`.

## Schema

```ts
type BeforeToolCallReturn =
  | undefined                                           // allow (no change)
  | { params: Record<string, unknown> }                 // allow with param rewrite
  | { block: true; blockReason: string }                // block (existing)
  | { block: true; blockReason: string; stop: true };   // block + stop the agent loop (NEW)
```

| Field | Type | When set | Notes |
|---|---|---|---|
| `block` | `true` | when blocking | Pre-existing |
| `blockReason` | string | with `block` | Pre-existing |
| `params` | object | when allowing with rewrite | Pre-existing |
| **`stop`** | `true` | when terminating the agent loop | **NEW (spec 091)** |

## When `stop: true` is set

- `decision.continue === false` from the kernel (hard stop / lockdown)
- `forcedContinuations[sessionId] >= 3` (FR-009 cap)
- `stopHookActive[sessionId] === true` (reentrancy guard — FR-008)

## Compatibility / R1 caveat

**Risk**: openclaw's plugin-loader behavior on the `stop` field is unverified. Two cases:

### Case A — openclaw honors `stop: true`

The harness terminates the agent loop on receipt. Single deny → single termination. ✅ Full fix.

### Case B — openclaw ignores `stop: true`

The harness continues; the agent retries. The plugin-side reentrancy guard (FR-008) ensures subsequent `before_tool_call`s for the same session short-circuit back to `{ block: true, stop: true }`, so the agent is stuck in a fast deny→deny loop instead of a deny→loop→escalation cycle. Worse than Case A but still better than today.

**Fallback signal (always emitted regardless of case)**:

```js
log.error(`chitin-stop-signal sessionId=${sid} rule=${ruleId} reason=${stopReason}`);
```

This appears in openclaw-gateway's journal log. Operators (or an outer watcher) can detect this pattern and force-terminate the session via `systemctl --user restart openclaw-gateway` or a finer-grained mechanism if implemented.

## Implementation phase verification (T1 task)

Before merging, the implementation phase:
1. Reads `node_modules/openclaw/dist/*.d.ts` (or equivalent) to confirm whether the return shape supports `stop`
2. If unclear from types: tests against a live openclaw run with `stop: true` and observes harness behavior
3. Documents the verdict in the PR body (Case A or Case B)
4. If Case B: files an upstream PR against openclaw to add the `stop` field officially, AND keeps the fallback `log.error` pattern

## Invariants

- `stop: true` is always accompanied by `block: true`
- `stop: true` is NEVER set on an `allow` response (allow + stop is meaningless)
- Once `stopHookActive[sid] === true`, ALL subsequent returns for that session include `stop: true`, regardless of the action attempted
- The `stop` field is additive — old openclaw versions that ignore unknown keys are unaffected by its presence

## Source of truth

`apps/openclaw-plugin-governance/src/index.mjs` (post-fix). Tests at `apps/openclaw-plugin-governance/test/bridge.test.ts`.
