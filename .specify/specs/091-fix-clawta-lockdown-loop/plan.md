# Implementation Plan: Honor `continue:false` from the chitin governance gate

**Branch**: `feat/091-fix-clawta-lockdown-loop` | **Date**: 2026-05-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/091-fix-clawta-lockdown-loop/spec.md`

## Summary

The chitin governance gate correctly emits `{decision:"block", continue:false}` on lockdown denies (verified at `go/execution-kernel/internal/driver/claudecode/format.go:52-54` and tested at `format_test.go:102-140`). The consumer side — the openclaw plugin at `apps/openclaw-plugin-governance/` — discards the signal: its `parseRouterDecision()` extracts the kernel JSON but never reads `j.continue`, and its `before_tool_call` handler returns only `{block: true, blockReason: ...}` without any "stop the agent loop" signal. Result: openclaw's harness blocks the offending tool call, but the agent retries with the same or similar action, hits the same deny, and the kernel's lockdown counter trips a `lockdown_loop_detected` event after 3 occurrences in 90s.

**Codex appServer (Clawta's runtime) lacks the PreToolUse hook mechanism Codex CLI offers.** So the fix can't piggyback on a Claude-Code-style stop-hook — it has to go through the openclaw plugin's `before_tool_call` return contract.

Implementation = three in-repo changes plus one upstream verification:

1. Extend `GateDecision` typedef in `chitin-bridge.mjs` to include `continue?: boolean`.
2. Update `parseRouterDecision()` to extract `j.continue` and `j.rule_id` from the kernel JSON instead of discarding them.
3. Update `before_tool_call` in `index.mjs` to propagate the stop signal AND maintain an in-plugin `stop_hook_active` reentrancy marker (FR-008) + N-bounded forced-continuation counter (FR-009).
4. Verify openclaw's `before_tool_call` return contract supports a stop-the-loop field (or determine the fallback signal mechanism).

## Technical Context

**Language/Version**: JavaScript (ESM, Node ≥ 20) for the openclaw plugin at `apps/openclaw-plugin-governance/`. Go 1.25 for the kernel (read-only confirmation; no kernel changes).

**Primary Dependencies**: `openclaw >= 2026.5.4` (peer dependency); standard Node child_process for the kernel subprocess bridge. No new dependencies introduced.

**Storage**: Per-session in-memory state for the reentrancy marker (FR-008) and continuation counter (FR-009). Persistence not required — when the openclaw process restarts, the marker resets, which is acceptable (the lockdown counter on the chitin-kernel side handles cross-session detection).

**Testing**: Vitest in `apps/openclaw-plugin-governance/test/`. Existing tests (`bridge.test.ts`, `subagent-gate.test.ts`) provide the harness; new test cases needed for FR-007/008/009.

**Target Platform**: Linux operator boxes running `openclaw-gateway.service` with the chitin-governance plugin loaded.

**Project Type**: Node-side gateway plugin + smoke test against the live kernel binary. No new code in `go/`.

**Performance Goals**: The reentrancy marker check is O(1) per tool call; the continuation counter is O(1). Total added latency: <1ms per `before_tool_call`. The actual win is dramatic — sessions that previously burned ~3× tokens to a lockdown loop terminate after one deny.

**Constraints**:
- Cannot bypass the openclaw return-contract — the gateway harness behavior is upstream. **If openclaw's plugin contract doesn't support a stop-the-loop field, the fix is partial: chitin extracts the signal and surfaces it via logger.warn + chain emission, but the loop still happens until openclaw upstream adds the field.**
- Constitution §7 (just-landed amendment) requires that this fix MUST go through the orchestrator as a spec-derived work-unit. This spec IS that work-unit.
- Cannot regress soft-block (`continue:true`) behavior — only the hard-stop path is affected.
- Cannot delete chain events; FR-002 (clean session-close event) is additive.

**Scale/Scope**: Single openclaw plugin package; ~3 files touched (`chitin-bridge.mjs`, `index.mjs`, `test/bridge.test.ts`); ~50 added lines.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluating against `.specify/memory/constitution.md` §1-§6 plus the just-landed §7 (PR #925):

| § | Rule | Verdict | Why |
|---|------|---------|-----|
| 1 | Side-effect boundary — kernel is the only chain-writer | ✅ PASS | Plugin sends data to kernel via subprocess + reads decision JSON; FR-009's `stop_signal_ignored` event is emitted via the kernel's existing emit path, not from the plugin directly. |
| 2 | Worker + worktree discipline | ✅ PASS | Implementation runs in dedicated worktree per §2 / spec 070. |
| 3 | Spec-kit promotion gate | ✅ PASS | This spec exists; PR #923 is open. |
| 4 | Tracked installers | ✅ PASS (vacuous) | The plugin is an Nx-built package, not a swarm/-installer-style script. |
| 5 | Board-aware scripts | ✅ PASS (vacuous) | No kanban interaction. |
| 6 | Swarm tooling is the exception | ✅ PASS | Plugin lives under `apps/`, not `swarm/`. |
| **7** | **The swarm is the orchestrator** | ✅ PASS | This fix targets the harness consumer path so `continue:false` is honored — directly aligned with §7's "no driver bypass" requirement. The fix STRENGTHENS the gate, doesn't relax it. |

**Initial gate verdict**: 7/7 PASS. No complexity tracking entries required.

**Post-design recheck**: stays 7/7. The chosen approach (in-repo plugin edits + upstream verification) does not introduce new layers, frameworks, or bypasses.

## Project Structure

### Documentation (this feature)

```
specs/091-fix-clawta-lockdown-loop/
├── spec.md                       # committed (with FR-007/008/009 from Ares's review)
├── plan.md                       # this file
├── research.md                   # Phase 0 — bug locus + design decisions
├── data-model.md                 # Phase 1 — entities (decision shape, reentrancy state, telemetry)
├── quickstart.md                 # Phase 1 — verification recipe
├── contracts/
│   ├── kernel-decision-shape.md  # the JSON shape the kernel emits
│   ├── plugin-return-shape.md    # the openclaw before_tool_call return shape (extended)
│   └── stop-signal-ignored-event.md  # the chain event shape for FR-009
├── checklists/
│   └── requirements.md           # committed
└── tasks.md                      # Phase 2 — created by /speckit-tasks
```

### Source code (this fix touches)

```
apps/openclaw-plugin-governance/
├── src/
│   ├── chitin-bridge.mjs         # MODIFIED: typedef + parseRouterDecision() + evaluateHookGate()
│   └── index.mjs                 # MODIFIED: before_tool_call handler + reentrancy state
└── test/
    └── bridge.test.ts            # MODIFIED: add tests for continue:false extraction + reentrancy
```

### Files explicitly UNTOUCHED

```
go/execution-kernel/internal/driver/claudecode/format.go      # emission verified correct
go/execution-kernel/internal/driver/claudecode/format_test.go # tests confirm correctness
go/execution-kernel/internal/gov/*                            # gate logic unchanged
```

**Structure Decision**: single-package edit. The fix is bounded to the openclaw plugin. No kernel changes, no orchestrator changes, no console changes.

## Phase 2 Execution Strategy (preview — owned by /speckit-tasks)

Estimated 4-6 tasks, all in a single worktree partition:

1. Extend `GateDecision` typedef (FR-007 prep)
2. Update `parseRouterDecision()` + `evaluateHookGate()` to extract `continue` + `rule_id` (FR-007 core)
3. Implement reentrancy marker (`stop_hook_active`) in `index.mjs` (FR-008)
4. Implement N-bounded forced-continuation counter + `stop_signal_ignored` emission (FR-009)
5. Add tests for all three FRs (FR-007/008/009)
6. Verify openclaw upstream contract — if a "stop-the-loop" return field doesn't exist, fall back to the documented alternative (sentinel logger.warn pattern that openclaw-gateway monitors)

Each task lands in the same worktree partition; one commit per logical change; one PR updating #923.

## Risk flags (handed off to /speckit-tasks)

1. **R1 — Upstream openclaw contract gap**: the most likely blocker. If openclaw's `before_tool_call` return shape doesn't support a stop-the-loop field, the chitin-side fix is partial. Mitigation: emit a documented log pattern (e.g., `log.error("chitin-stop-signal-ignored ...")`) that the gateway's outer wrapper monitors as a fallback, AND file an upstream PR against the openclaw repo.
2. **R2 — Reentrancy state lifetime**: the `stop_hook_active` marker scope (per-tool? per-session? per-agent?). The plan picks per-session-id (computed at line 58 of `index.mjs`); the implementation phase confirms this is the right grain.
3. **R3 — Continuation counter persistence**: FR-009's N-counter resets on plugin reload. Acceptable trade-off — the kernel's chain-side lockdown counter handles cross-restart detection.
4. **R4 — Non-lockdown denies don't carry continue:false**: confirmed at `format_test.go:157-175`. The fix logic must guard against treating absent-`continue` as `continue:false`. Default to "soft block, agent may retry" for any deny that lacks the field explicitly.

## Complexity Tracking

No constitution violations. This section is intentionally empty.
