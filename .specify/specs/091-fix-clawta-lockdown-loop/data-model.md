# Data Model: Honor `continue:false`

**Feature**: 091-fix-clawta-lockdown-loop
**Date**: 2026-05-23

This is the structural decomposition of the data the fix touches — wire formats (kernel ↔ plugin, plugin ↔ openclaw harness), in-memory state, and telemetry events.

## Entity 1 — Kernel decision JSON (input to the plugin)

Emitted by `go/execution-kernel/internal/driver/claudecode/format.go` on every `before_tool_call`. Unchanged by this spec; documented here for completeness.

| Field | Type | Required | Notes |
|---|---|---|---|
| `decision` | `"allow"\|"block"` | yes | Top-level verdict |
| `reason` | string | conditional | Required when `decision === "block"` |
| `rule_id` | string | conditional | The rule that triggered. Currently dropped by the plugin (Site 2 bug); fixed by spec 091. |
| `continue` | boolean | conditional | Present iff rule is `"lockdown"`. `false` = hard stop; absent for soft denies. |
| `stopReason` | string | conditional | Human-readable stop reason. Co-occurs with `continue: false`. |

**Source of truth**: `format.go:52-54`. Tests: `format_test.go:102-140` (lockdown) and `format_test.go:157-175` (regular deny — confirms `continue` is intentionally absent).

## Entity 2 — `GateDecision` (extended)

In `apps/openclaw-plugin-governance/src/chitin-bridge.mjs`. The plugin-internal representation after parsing the kernel JSON.

| Field | Type | Required | Notes |
|---|---|---|---|
| `allow` | boolean | yes | Pre-existing |
| `reason` | string | optional | Pre-existing |
| `ruleId` | string | optional | **CHANGED**: now read from `j.rule_id`, not hardcoded |
| `params` | object | optional | Pre-existing |
| `continue` | `false \| undefined` | **NEW** | Mirrors kernel's `continue: false`; undefined when absent |
| `stopReason` | string | **NEW** | Mirrors kernel's `stopReason`; undefined when absent |

**State transitions**: `parseRouterDecision()` (and `evaluateHookGate()` for the exec-shaped path) construct this object from the kernel's JSON. No intermediate states.

## Entity 3 — Plugin reentrancy state

Module-scoped Maps in `apps/openclaw-plugin-governance/src/index.mjs`. Keyed by `sessionId`.

```js
const stopHookActive = new Map();        // sessionId → boolean
const forcedContinuations = new Map();   // sessionId → number
```

Where `sessionId = `openclaw-${ctx.agentId ?? 'plugin'}-${process.pid}` (matches existing line 58).

| State | Set when | Read when | Cleared when |
|---|---|---|---|
| `stopHookActive[sid] = true` | `decision.continue === false` returns from kernel; OR `forcedContinuations[sid] >= 3` | every `before_tool_call` (first check) | plugin reload (process restart) |
| `forcedContinuations[sid] = n+1` | every block-decision where `continue !== false` | FR-009 cap check | plugin reload |

**Lifetime**: process-scoped. The kernel-side lockdown counter handles cross-restart durability (existing mechanism).

**State invariants**:
- Once `stopHookActive[sid]` is true, it never becomes false within the same process
- `forcedContinuations[sid]` is monotonically increasing within a session
- Setting `stopHookActive[sid] = true` is the terminal state; subsequent `before_tool_call`s for that session short-circuit to a block-with-stop response

## Entity 4 — Plugin → openclaw harness return shape (extended)

The handler's return value from `before_tool_call`. Pre-existing fields preserved; one new field added.

| Field | Type | Required | Notes |
|---|---|---|---|
| `block` | boolean | yes (when blocking) | Pre-existing |
| `blockReason` | string | conditional | Pre-existing |
| `params` | object | optional | Pre-existing (for allow + param rewrite) |
| `stop` | boolean | **NEW** | `true` when the agent loop should terminate. **R1 caveat**: behavior depends on openclaw supporting this field. |

**Fallback if openclaw doesn't honor `stop`**: emit `log.error("chitin-stop-signal sessionId=<sid> rule=<rule>")` so the gateway's outer wrapper (or operator-side monitoring) can catch it. This is the documented degrade path; functionally inferior but visible.

## Entity 5 — `stop_signal_ignored` chain event (FR-009 telemetry)

Emitted to the chitin chain when `forcedContinuations[sid] >= FORCED_CONTINUATION_CAP`. Emitted via a kernel subprocess call (preserves §1: kernel is only chain-writer).

| Field | Type | Notes |
|---|---|---|
| `event_type` | `"stop_signal_ignored"` | New event type for this fix |
| `agent` | string | The agentId that hit the cap |
| `session_id` | string | The plugin-side sessionId |
| `continuation_count` | number | The cap value reached (defaults to 3) |
| `last_rule_id` | string | The last deny rule that incremented the counter |
| `ts` | ISO 8601 | Emission timestamp |
| `chain_id` / `seq` / `prev_hash` / `this_hash` | (chain frame) | Standard chain framing |

**Emission mechanism**: `await emitStopSignalIgnored(...)` calls `chitin-kernel emit` (existing subprocess pattern, mirrors how `chitin-kernel gate evaluate` is invoked).

## Sequence — happy path (lockdown deny stops the loop)

```
1. Agent in session sid attempts tool call X
2. Plugin checks stopHookActive[sid] → false; proceeds
3. Plugin calls kernel: chitin-kernel router evaluate
4. Kernel evaluates → lockdown deny → JSON: {decision:"block", continue:false, stopReason:"..."}
5. Plugin parseRouterDecision → GateDecision { allow:false, continue:false, stopReason:"..." }
6. Handler sees decision.continue === false
   - Set stopHookActive[sid] = true
   - log.error("chitin stop-signal: ...")
   - Return { block:true, blockReason:"...", stop:true }
7. Openclaw harness sees stop:true → terminates the agent loop
8. (Or fallback: openclaw ignores stop, sees the log.error, outer watcher catches it)
```

## Sequence — N-bounded continuation (FR-009)

```
1-5. Regular deny (continue absent); forcedContinuations[sid] = 1
6. Handler increments counter, returns { block:true, blockReason }
7. Agent retries with similar action; deny again; counter = 2
... 
8. Counter = 3, hits cap
   - Set stopHookActive[sid] = true
   - log.error("chitin: 3 forced continuations ...")
   - await emitStopSignalIgnored(...)  ← chain event emitted
   - Return { block:true, blockReason:"forced-continuation cap exceeded", stop:true }
9. Next call for sid → stopHookActive guard short-circuits
```

## Sequence — soft deny (no change in behavior — regression-prevention)

```
1-5. Regular deny (continue absent); counter = 1
6. Handler returns { block:true, blockReason }
7. Agent retries; counter = 2
... agent may eventually succeed (e.g., uses a different param). Counter doesn't decrement, but if cap isn't hit, behavior is unchanged from today.
```

This sequence is critical to preserve. FR-004 explicitly says soft-block behavior must not change.

## Validation rules

For the implementation to satisfy the FRs:

| Invariant | How to check |
|---|---|
| `decision.continue === false` ⇒ next `before_tool_call` for same `sessionId` short-circuits | Trigger lockdown deny; assert next handler call returns `{ stop:true }` immediately without subprocess invocation |
| `forcedContinuations[sid] >= 3` ⇒ stop-signal-ignored event emitted | Trigger 3 sequential regular denies; assert one chain event of `event_type === "stop_signal_ignored"` |
| `decision.continue === undefined` ⇒ stop-hook NOT activated | Trigger one regular deny; assert subsequent call proceeds normally |
| `decision.continue === true` (soft block) ⇒ stop-hook NOT activated | Forge a kernel response with `continue:true`; assert no activation |
| `stopHookActive[sid]` once true is sticky | Set via lockdown; subsequent calls return `{ stop:true }` even with allow-shaped tools |
