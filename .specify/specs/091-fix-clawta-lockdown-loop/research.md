# Phase 0 Research: Honor `continue:false` from the chitin governance gate

**Feature**: 091-fix-clawta-lockdown-loop
**Date**: 2026-05-23
**Status**: Bug locus identified with high confidence (single consumer file, two functions). Upstream openclaw contract is the one remaining unknown — flagged as R1 for implementation phase.

## Decision summary

| ID | Decision | Confidence |
|---|---|---|
| D1 | Bug is in `apps/openclaw-plugin-governance/` (in-repo) | High — three precise sites identified |
| D2 | Kernel emission is correct (FR-005 holds) | High — `format_test.go:102-140` covers it |
| D3 | Codex CLI's PreToolUse hooks are NOT available — fix must go through openclaw plugin | High — historical observation 5266 (2026-05-20) |
| D4 | Stop signal propagation surface = openclaw plugin's `before_tool_call` return value | Medium — depends on R1 |
| D5 | Reentrancy marker scope = per `sessionId` (per agent + cwd + process) | High — matches existing session-id construction at `index.mjs:58` |
| D6 | N-bounded continuation cap = 3 (matches kernel-side lockdown threshold) | Medium — operator may tune |
| D7 | `stop_signal_ignored` telemetry event emitted via the kernel's chain (not plugin direct) | High — preserves §1 (kernel is only chain-writer) |

## Bug locus — three precise sites

### Site 1: `apps/openclaw-plugin-governance/src/chitin-bridge.mjs:26-30`

The `GateDecision` JSDoc typedef is missing the `continue` field:

```js
/**
 * @typedef {object} GateDecision
 * @property {boolean} allow
 * @property {string} [reason]
 * @property {string} [ruleId]
 * @property {Record<string, unknown>} [params]
 */
```

**Decision**: extend typedef with `@property {boolean} [continue]` and `@property {string} [stopReason]`. Cited type alone doesn't fix runtime — but the parser change in Site 2 must agree with the type.

**Rationale**: keeps the JS surface honest. Any consumer reading `decision.continue` shouldn't have to defensively probe.

**Alternatives considered**: skip typedef change, change only runtime. Rejected — silently divergent types lead to drift; the typedef + parser must move together.

### Site 2: `apps/openclaw-plugin-governance/src/chitin-bridge.mjs:295-319` (`parseRouterDecision`)

The current parser:

```js
const j = JSON.parse(firstLine);
if (j.decision === 'block') {
  return {
    allow: false,
    reason: typeof j.reason === 'string' ? j.reason : 'denied by chitin router',
    ruleId: 'router_block',     // hardcoded — discards j.rule_id
  };
}
```

`j.continue` is parsed but never read. `j.rule_id` from the kernel is overwritten with a hardcoded `'router_block'` string.

**Decision**: extract both fields. The corrected parser returns:

```js
return {
  allow: false,
  reason: typeof j.reason === 'string' ? j.reason : 'denied by chitin router',
  ruleId: typeof j.rule_id === 'string' ? j.rule_id : 'router_block',
  continue: j.continue === false ? false : undefined,
  stopReason: typeof j.stopReason === 'string' ? j.stopReason : undefined,
};
```

**Rationale**: faithful read of the kernel's contract. The kernel emits `continue: false` only on hard stops; the parser must surface that field unchanged.

**Alternatives considered**:
- Default `continue` to `true` when absent — rejected. The kernel intentionally omits the field for soft denies (per `format_test.go:157-175`); treating absent as `true` is correct, but we encode it as `undefined` so the consumer can distinguish.
- Always set `continue: true` for non-lockdown denies — rejected. Adds inferred state that the kernel didn't emit.

**Companion change**: `evaluateHookGate()` (the exec-shaped path at the top of the same file) needs the same treatment for consistency — it parses a similar JSON.

### Site 3: `apps/openclaw-plugin-governance/src/index.mjs:48-81` (`before_tool_call`)

The consumer never checks `decision.continue` and never maintains reentrancy state:

```js
api.on('before_tool_call', async (event, ctx) => {
  // ... evaluate ...
  if (decision.allow) { return ...; }
  if (cfg.mode === 'observe') { ... return undefined; }
  log.warn(`chitin denied ...`);
  return { block: true, blockReason: decision.reason ?? 'denied by chitin policy' };
});
```

**Decision**: extend the handler to (a) propagate `continue: false` as a stop signal in the return value, (b) maintain a per-session `stop_hook_active` marker (FR-008), (c) maintain a per-session forced-continuation counter (FR-009).

```js
// module-scoped state, keyed by sessionId
const stopHookActive = new Map();    // sessionId → boolean
const forcedContinuations = new Map(); // sessionId → number
const FORCED_CONTINUATION_CAP = 3;

api.on('before_tool_call', async (event, ctx) => {
  const sessionId = `openclaw-${ctx.agentId ?? 'plugin'}-${process.pid}`;

  // FR-008: if stop_hook_active for this session, exit immediately
  if (stopHookActive.get(sessionId)) {
    return { block: true, blockReason: 'chitin: stop signal previously emitted; agent loop must terminate', stop: true };
  }

  const decision = await evaluate(...);

  if (decision.allow) { return ...; }
  if (cfg.mode === 'observe') { ... }

  // FR-007: honor continue:false
  if (decision.continue === false) {
    stopHookActive.set(sessionId, true);
    log.error(`chitin stop-signal: ${decision.stopReason ?? decision.reason}`);
    return {
      block: true,
      blockReason: decision.stopReason ?? decision.reason ?? 'denied by chitin policy',
      stop: true,  // pending upstream verification — see R1
    };
  }

  // FR-009: track forced continuations on regular denies
  const count = (forcedContinuations.get(sessionId) ?? 0) + 1;
  forcedContinuations.set(sessionId, count);
  if (count >= FORCED_CONTINUATION_CAP) {
    stopHookActive.set(sessionId, true);
    log.error(`chitin: ${count} forced continuations for session ${sessionId} — marking session failed`);
    // emit stop_signal_ignored telemetry via kernel (separate kernel subprocess call)
    await emitStopSignalIgnored({ sessionId, agentId: ctx.agentId, count, lastRule: decision.ruleId });
    return { block: true, blockReason: 'chitin: forced-continuation cap exceeded', stop: true };
  }

  log.warn(`chitin denied tool=... reason=...`);
  return { block: true, blockReason: decision.reason ?? '...' };
});
```

**Rationale**: keeps logic in one place; preserves backwards compat (the new `stop` field is additive; old openclaw versions ignore unknown return keys).

**Alternatives considered**:
- Implement stop signal as an exception thrown from the handler — rejected. Openclaw's plugin loader catches handler exceptions and may convert them to errors, losing the signal intent.
- Process.exit() the plugin — rejected. Kills the whole gateway including unrelated sessions.
- Sentinel file written to disk — rejected as primary; reserved as R1 fallback.

## D3 — Why Codex appServer can't use Claude Code stop hooks

Historical observation 5266 (2026-05-20): "Codex appServer (Clawta's runtime) lacks Pre-ToolUse hook mechanism available in Codex CLI."

**Implication**: The Anthropic-published fix from [Claude Code issue #55754](https://github.com/anthropics/claude-code/issues/55754) — `stop_hook_active` checked by the agent harness — doesn't directly apply because Clawta isn't running Claude Code. It's running through openclaw's plugin loader against the Codex appServer.

**Decision**: implement the **equivalent pattern** in the openclaw plugin layer:
- The `stop_hook_active` semantic stays the same (reentrancy guard)
- The data lives in plugin module-scope state instead of the agent harness's session state
- The check happens at every `before_tool_call`, not at the harness loop boundary
- After the cap, telemetry emits to the chain so the orchestrator side can route the failure

## R1 — Upstream openclaw return-contract gap (UNKNOWN)

**Risk**: openclaw's `before_tool_call` return shape may not honor a `stop: true` field. If the openclaw harness only reads `{ block, blockReason }`, the plugin's stop signal is dropped.

**Mitigation plan in priority order**:
1. Implementation phase reads openclaw's @types or peer-dep definitions to confirm the shape.
2. If `stop` is supported → done (option A).
3. If `stop` is NOT supported but openclaw monitors stderr/log lines:
   - Emit `chitin-stop-signal-ignored sessionId=<sid> rule=<rule>` to logger.error (always)
   - Document the pattern as the "wire signal" for operators
   - Gateway-side outer watcher monitors for this exact log line and terminates the session
4. If openclaw has neither mechanism:
   - File upstream PR to add a `stop` field to the return contract
   - Ship the plugin-side fix anyway (the log emission is still useful for operator visibility)
   - Mark FR-007 as partially complete pending upstream merge

**Decision today**: write the plugin code as if option A succeeds. Implementation phase validates and falls back as needed.

## R2 — Reentrancy state lifetime

**Decision**: per-`sessionId` (string key = `openclaw-${agentId}-${pid}`). Matches the existing session-id at `index.mjs:58`. Lives in plugin module scope; resets on plugin reload, which is acceptable (kernel-side lockdown counter handles cross-restart detection).

**Alternatives considered**:
- Per-PID — too coarse; one bad agent locks the whole gateway
- Per-tool — too fine; allows the same agent to retry different tools after lockdown
- Per-agentId — close but ignores PID; agents that respawn would inherit lockdown

## R3 — Continuation counter cap value

**Decision**: N=3 default. Matches the kernel-side lockdown rule's threshold (3 deny events within 90s). Operator can tune via config.

**Alternatives considered**:
- N=1 (one strike) — too aggressive for soft denies
- N=10 — too lenient; defeats the cap's purpose
- Configurable per-rule — defer to a follow-up if needed

## R4 — Non-lockdown denies and absent `continue` field

**Decision**: `decision.continue === false` is the ONLY trigger for stop-hook activation. `undefined` (absent) does NOT trigger; the agent may retry per existing behavior. This matches the kernel's intent at `format_test.go:157-175`.

The FR-009 counter still increments on regular denies — different mechanism, same protection.

## Cross-references

- Anthropic published-fix reference: [anthropics/claude-code#55754](https://github.com/anthropics/claude-code/issues/55754)
- Companion strategy doc: `docs/strategy/chitin-orchestrator-industry-alignment-2026-05-22.md` (Priority 1)
- Constitution §7 (just landed via PR #925): `.specify/memory/constitution.md`
- Kernel emission site: `go/execution-kernel/internal/driver/claudecode/format.go:52-54`
- Kernel emission tests: `go/execution-kernel/internal/driver/claudecode/format_test.go:102-140`
- Bug locus files: `apps/openclaw-plugin-governance/src/{chitin-bridge.mjs,index.mjs}`
