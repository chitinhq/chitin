# Tasks: Honor `continue:false` from the chitin governance gate

**Feature**: 091-fix-clawta-lockdown-loop
**Branch**: `feat/091-fix-clawta-lockdown-loop`
**Plan**: [plan.md](plan.md) · **Spec**: [spec.md](spec.md) · **Research**: [research.md](research.md)

## Overview

Total tasks: **8** (1 setup + 1 foundational + 5 US1 + 1 polish)
Estimated effort: **~45-90 minutes** end-to-end, depending on R1 (upstream openclaw contract).

Bug locus is in `apps/openclaw-plugin-governance/` (three precise sites). Kernel side is verified correct (FR-005). The fix extends the JS plugin's decision typedef, parser, and `before_tool_call` handler.

## Phase 1: Setup

- [ ] T001 Verify upstream openclaw `before_tool_call` return-shape contract: check whether `{ block:true, stop:true }` is a supported field. Read `apps/openclaw-plugin-governance/node_modules/openclaw/**/*.d.ts` for the handler return type. Record the verdict (Case A: supported / Case B: not supported / Case C: undocumented) in PR #923 body and in research.md R1. If Case B/C, proceed with the fallback `log.error` pattern as the documented degrade path.

## Phase 2: Foundational

- [ ] T002 Extend the `GateDecision` JSDoc typedef at `apps/openclaw-plugin-governance/src/chitin-bridge.mjs:26-30` to add `@property {boolean} [continue]` and `@property {string} [stopReason]`. This is the precondition for the parser updates in Phase 3; the parser and handler will rely on the typedef. Single-file edit; no behavioral change.

## Phase 3: User Story 1 — A governance deny stops the agent (P1)

**Story goal**: After this phase completes, a `continue:false` deny from the kernel causes exactly ONE deny event in the chain followed by session termination. The agent loop does not retry. The `lockdown_loop_detected` counter does not trip.

**Independent test**: trigger a lockdown-triggering tool call against Clawta; observe the chain shows exactly one `decision` event with `rule_id=lockdown` followed by no further denies for the same rule in that session window.

- [ ] T003 [US1] Update `parseRouterDecision()` at `apps/openclaw-plugin-governance/src/chitin-bridge.mjs:295-319` to extract `j.continue` and `j.rule_id` from the kernel's JSON. The block branch now returns `{ allow: false, reason, ruleId: typeof j.rule_id === 'string' ? j.rule_id : 'router_block', continue: j.continue === false ? false : undefined, stopReason: typeof j.stopReason === 'string' ? j.stopReason : undefined }`. Apply the same treatment to `evaluateHookGate()` higher in the same file for consistency (exec-shaped tool path).

- [ ] T004 [US1] In `apps/openclaw-plugin-governance/src/index.mjs`, add two module-scoped Maps at the top of the file: `const stopHookActive = new Map()` (sessionId → boolean) and `const forcedContinuations = new Map()` (sessionId → number). Add `const FORCED_CONTINUATION_CAP = 3`. These hold per-session state used in T005.

- [ ] T005 [US1] Rewrite the `before_tool_call` handler at `apps/openclaw-plugin-governance/src/index.mjs:48-81` to (a) first-check `stopHookActive.get(sessionId)` and short-circuit with `{ block:true, blockReason:'chitin: stop signal previously emitted; agent loop must terminate', stop:true }` if active, (b) on a block-decision with `decision.continue === false`, set `stopHookActive.set(sessionId, true)`, emit `log.error('chitin-stop-signal sessionId=<sid> rule=<rule> reason=<reason>')`, and return `{ block:true, blockReason: decision.stopReason ?? decision.reason, stop:true }`, (c) on a block-decision with `continue !== false`, increment `forcedContinuations.get(sessionId)`; if it reaches the cap, set stopHookActive AND emit a `stop_signal_ignored` chain event via a new helper `emitStopSignalIgnored()` (added in T006). The sessionId remains the existing `openclaw-${ctx.agentId ?? 'plugin'}-${process.pid}` string at line 58.

- [ ] T006 [US1] Add a helper function `emitStopSignalIgnored({ sessionId, agentId, count, lastRuleId, lastReason, firstDenyTs })` at the bottom of `apps/openclaw-plugin-governance/src/index.mjs` that spawns `chitin-kernel emit` (or the existing equivalent) with a JSON event payload matching `contracts/stop-signal-ignored-event.md`. Use `child_process.spawn` analogous to how `chitin-bridge.mjs` invokes the kernel. Preserves §1 (kernel-only chain-writer). The helper logs `log.error` on subprocess failure but does not throw — telemetry-emission failure must not corrupt the deny path.

- [ ] T007 [US1] Add tests to `apps/openclaw-plugin-governance/test/bridge.test.ts`: (1) `parseRouterDecision extracts continue:false from lockdown JSON`, (2) `parseRouterDecision returns continue: undefined for regular denies`, (3) `parseRouterDecision reads j.rule_id instead of hardcoding 'router_block'`. Plus tests in a new file `apps/openclaw-plugin-governance/test/lockdown-loop.test.ts`: (4) `before_tool_call returns stop:true when decision.continue === false`, (5) `stop_hook_active sticky once set — subsequent calls short-circuit`, (6) `forced-continuation counter caps at 3 — emits stop_signal_ignored`, (7) `regression: regular deny does not set stop`, (8) `regression: soft denies still allow agent retry below cap`. Use vitest mocks to forge kernel responses; don't invoke the real binary in unit tests.

## Phase 4: Polish & Cross-Cutting

- [ ] T008 Run the full test suite (`pnpm -F @chitinhq/openclaw-plugin-governance test` from repo root, or `cd apps/openclaw-plugin-governance && pnpm test`); confirm all new tests pass and existing tests don't regress. Stage all changes, commit on `feat/091-fix-clawta-lockdown-loop` with a commit message linking spec 091 and citing the three FRs implemented (FR-007/008/009). Push (PR #923 updates). Update PR #923 body with: (a) R1 case verdict from T001, (b) summary of the four sites changed, (c) test results, (d) note that operator-side smoke (SC-001/002/003) will be run post-merge.

## Dependency graph

```
T001 (verify upstream)
  │
  ▼
T002 (typedef) ──────────► T003 (parser) ──┐
                                            ├─► T005 (handler) ──► T007 (tests) ──► T008 (PR)
                            T004 (state) ──┘            │
                                                         └─► T006 (telemetry helper)
```

- T002 blocks T003 (parser uses the new typedef fields).
- T003 blocks T005 (handler reads `decision.continue` which the parser must now emit).
- T004 blocks T005 (handler uses the Maps).
- T005 blocks T006 (handler invokes `emitStopSignalIgnored()` — but the helper can be added in parallel).
- T007 blocks T008 (PR body should reflect test results).

T001 is independent and can be done at any time before T008.

## Parallel execution examples

- **T002 + T004** can run in parallel — different files, no shared state.
- **T006 (helper) + T007 (tests for non-telemetry parts)** can run in parallel.

## Implementation strategy

**MVP scope**: T001 → T002 → T003 → T005 (without T006 telemetry) → T007 (without telemetry tests) → T008. This delivers FR-007 fully and FR-008 fully; FR-009 is partial (the cap activates the reentrancy guard, but the `stop_signal_ignored` event isn't emitted).

**Full scope**: all 8 tasks. Adds the FR-009 telemetry event.

The MVP is mergeable on its own; the telemetry can land as a follow-up if needed. But the work is small enough (one helper + a few tests) that splitting it isn't worth the ceremony — do all 8.

## Format validation

All 8 tasks above conform to the required checklist format:
- ✅ Every task starts with `- [ ]`
- ✅ Sequential IDs T001-T008
- ✅ User-story-phase tasks have `[US1]` labels; Setup (T001), Foundational (T002), Polish (T008) have no story label
- ✅ Each task includes the precise file path being modified
- ✅ Descriptions specify the exact code change, line ranges where applicable, and the FR(s) the task addresses
