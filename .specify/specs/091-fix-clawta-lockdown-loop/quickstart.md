# Quickstart: Verify spec 091 (continue:false honored)

**Feature**: 091-fix-clawta-lockdown-loop

## Pre-flight

```bash
git checkout feat/091-fix-clawta-lockdown-loop
git pull
cd apps/openclaw-plugin-governance
pnpm install
```

## Unit verification (T-tests added by /speckit-tasks)

```bash
cd apps/openclaw-plugin-governance
pnpm test
```

Expect new test cases passing:

- ✅ `parseRouterDecision extracts continue:false from lockdown JSON`
- ✅ `before_tool_call returns stop:true when decision.continue === false`
- ✅ `stop_hook_active sticky once set — subsequent calls short-circuit`
- ✅ `forced-continuation counter caps at 3 — emits stop_signal_ignored`
- ✅ `regression: continue:undefined (soft deny) does NOT set stop`
- ✅ `regression: soft denies still allow agent retry below cap`

## SC-001 — One deny, one stop (controlled lockdown test)

Manual smoke against a live openclaw-gateway with the plugin loaded:

```bash
# Trigger a known-lockdown action against Clawta (via Discord DM or a controlled tool call)
# Then inspect the chain:
grep -h '"agent_instance_id":"clawta"' ~/.chitin/events-*.jsonl | tail -20 | jq -c '{event_type, ts, payload:.payload.rule_id // .payload.reason}'
```

Expect:
- Exactly ONE `event_type: "decision"` event with `decision: "block"` and `rule_id: "lockdown"`
- Zero `event_type: "lockdown_loop_detected"` events (the rule didn't fire 3+ times)
- A `session_close` (or equivalent) event terminating the session

## SC-002 — 48h quiescence (post-merge observability)

```bash
# Run 48h post-merge on a production-equivalent operator box; then:
grep -h '"event_type":"lockdown_loop_detected"' ~/.chitin/events-*.jsonl \
  | grep '"agent":"clawta"' \
  | wc -l
# Expected: 0 (or ≤2 if harness restart timing matters)
```

## SC-003 — Token budget reclaimed (observational)

Compare a representative Clawta session that previously hit a lockdown loop (pre-merge) to the same scenario post-merge:

```bash
# Pre-merge baseline (already captured):
#   ~30k tokens consumed before hitting lockdown_loop_detected
# Post-merge:
#   ~10k tokens consumed; session terminates at the FIRST lockdown deny
```

Mechanism for the measurement depends on whether token telemetry exists for openclaw sessions — if not, this SC is observational (operator confirms via session-length change).

## SC-004 — Operator visibility preserved

```bash
jq 'select(.event_type=="decision" and .payload.decision=="block")' ~/.chitin/events-*.jsonl | head -5
```

Expect: deny events still appear in the chain. The fix doesn't suppress the deny; it just stops the cascade after one.

## R1 — Upstream openclaw `stop` field verification

Implementation phase task. Verify before merging:

```bash
# Look for the return-shape declaration in openclaw's types
find node_modules/openclaw -name '*.d.ts' | xargs grep -l 'before_tool_call\|BeforeToolCall' | head -3
# If found, inspect for a `stop?: boolean` field
```

Document the result in the PR body:
- **Case A** (`stop` field present): "Fix is complete; openclaw honors `stop: true`."
- **Case B** (`stop` field absent): "Fix is partial; plugin-side stop guard activates but openclaw harness loop continues. Fallback `log.error` pattern is the operator's signal. Upstream PR queued at <link>."

## Failure modes the verification catches

| Symptom | Likely cause |
|---|---|
| `parseRouterDecision extracts continue` test fails | The parser still hardcodes `ruleId: 'router_block'` and drops `continue`. Restore from `contracts/kernel-decision-shape.md`. |
| `stop_hook_active sticky` test fails | The Map isn't module-scoped (re-instantiated per call). Check that `stopHookActive` is declared at top-level of `index.mjs`. |
| `forced-continuation counter` test fails | Counter not incremented on regular denies, or cap mis-applied. Check the `continue === false` branch vs the soft-deny branch. |
| SC-002 still shows lockdown_loop_detected events | The plugin is fixed but openclaw's harness ignores `stop` AND the operator hasn't wired the fallback monitor. Verify R1 disposition. |
| Soft-deny tests regress | The parser changed `continue: undefined` to `continue: true` or similar — kernel's intent is "absent === undefined". Restore. |
