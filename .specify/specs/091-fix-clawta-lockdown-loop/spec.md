# Feature Specification: Honor `continue:false` from the chitin governance gate

**Feature Branch**: `feat/091-fix-clawta-lockdown-loop`

**Created**: 2026-05-22

**Status**: Draft

**Input**: User description: "Clawta hits governance denies and keeps retrying instead of stopping. The chitin gate emits the deny with `continue:false` — the agent harness/driver is supposed to honor that signal and terminate the agent loop. Today it doesn't. The kernel's lockdown rule fires repeatedly (3+ denies within 90 seconds), which trips `lockdown_loop_detected` and the chain logs: 'primary continue:false stop signal likely not honored — operator should investigate harness/driver.' This bug means: a deny that should be a single hard stop becomes a runaway loop that floods the chain, eats resources, and obscures the original violation."

## User Scenarios & Testing *(mandatory)*

### User Story 1 — A governance deny stops the agent (Priority: P1)

A Clawta session attempts a tool call. The chitin governance gate denies it with `decision: block, continue: false, stopReason: <rule_id>`. The agent harness reads `continue: false` and **terminates the agent loop immediately**. The chain records ONE deny event for this session. The kernel's lockdown counter never reaches the threshold, because the rule doesn't fire a second time within the window.

**Why this priority**: this IS the bug. Every other concern (lockdown-loop telemetry noise, doubled chain events, exhausted token budget on retries, obscured root cause) is downstream of this single mismatch between the kernel's contract (`continue:false` means STOP) and the harness behavior (continues anyway).

**Independent test**: trigger a known-deny scenario (Clawta attempts an `exec` that hits a governance rule with `continue:false`). Within one minute, observe the chain events file for that session: exactly ONE deny event for the offending rule, no `lockdown_loop_detected` event, the session marked completed/terminated. Run the same test against an agent that DOES honor the signal as a baseline — outcome should be byte-comparable (one deny, no loop).

---

### Edge Cases

- **A rule that emits `continue:true` (soft block)** — the harness keeps running. This spec doesn't change that path; only the `continue:false` path is affected.
- **The harness receives the deny but can't parse `continue:false` (e.g., older driver version)** — should fail-closed, not fail-open. Treat unparseable as STOP, not as CONTINUE.
- **Multiple denies in a single tool-call attempt (e.g., the driver retries internally before surfacing one to the chain)** — the harness's internal retry MUST also honor `continue:false` from the FIRST deny; not just the final surfaced one.
- **A deny arrives while the agent is mid-step (between tool calls)** — the harness still terminates at the next step boundary; no in-progress step continues past `continue:false`.
- **The bug exists in driver X but not driver Y** — the spec covers whichever driver(s) Clawta uses today. Plan phase identifies the specific driver(s); fix applies to those.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: When the chitin governance gate emits a decision with `decision: "block"` and `continue: false`, the agent harness/driver receiving that decision MUST terminate the agent loop before issuing any subsequent tool call. No retry; no fallback; no "let's try a different parameter and see if THAT works."
- **FR-002**: A terminated session MUST close cleanly: the chain receives a `session_close` (or equivalent) event with reason linked to the originating deny. No half-closed session, no orphaned worker process.
- **FR-003**: An unparseable or malformed deny payload MUST fail-closed — treat as `continue: false`, terminate the loop, log the parse failure. Fail-open (continuing on parse error) is forbidden.
- **FR-004**: This fix MUST NOT change the behavior of `continue: true` (soft block) decisions. Only the `continue: false` (hard stop) path is affected.
- **FR-005**: The kernel's emission of `continue: false` is already correct (verified during investigation — chain events show the signal is being sent). This spec covers the consumer (harness/driver) side; no kernel changes required.
- **FR-006**: After this fix lands, the `lockdown_loop_detected` event type MUST become rare — a chain query for `event_type == "lockdown_loop_detected"` over a week of normal operation should return zero matches under normal conditions (the rule remains armed as a safety net, but should never trip when harnesses behave correctly).

### Success Criteria *(mandatory)*

- **SC-001 (One deny, one stop)**: in a controlled test, a `continue:false` deny against Clawta produces exactly one deny event in the chain followed by a session-close event. No second deny for the same rule. No `lockdown_loop_detected`.
- **SC-002 (Lockdown counter quiescent)**: over 48 hours of operator-normal use post-fix, `grep -h '"event_type":"lockdown_loop_detected"' ~/.chitin/events-*.jsonl | grep '"agent":"clawta"' | wc -l` returns 0 (or near-0; allow a couple if harness restart timing matters).
- **SC-003 (Token budget reclaimed)**: a session that used to burn 3× the tokens hitting the lockdown loop now burns 1× and terminates. Measured by comparing token-counts in before/after sessions for the same triggering scenario, if such telemetry is available; otherwise observational.
- **SC-004 (Operator visibility preserved)**: the operator can still see the originating deny in the chain (the deny isn't lost in the termination flow). `jq 'select(.event_type=="decision" and .payload.decision=="block")' ~/.chitin/events-*.jsonl` continues to show single denies.

## Assumptions

- The chitin governance gate's emission of `continue: false` is correct and unchanged (verified — chain events show `continue: false` being sent properly).
- The bug is in the **consumer** side: whatever harness/driver Clawta uses receives the deny and the `continue:false` flag but doesn't terminate the loop. Plan-phase identifies the specific driver (Claude Code? openclaw internal? something else?) and the fix lands in that driver's code OR in an openclaw-side plugin that wraps the driver's loop.
- This fix may not live in the chitin repo — if the driver is external (e.g., Claude Code itself, or an openclaw plugin in a separate repo), the chitin-side deliverable is (a) document the contract clearly, (b) file the fix against the driver repo, (c) add a chitin-side smoke test that exercises the contract.
- Clawta is the symptom-surface today, but any agent on any driver that ignores `continue:false` would exhibit the same bug. The fix benefits the platform, not just Clawta.

### Scope

**In scope**:
- Identifying which driver/harness Clawta uses (plan-phase research)
- Honoring `continue:false` in that driver/harness — terminate the agent loop
- Clean session-close on termination (FR-002)
- Fail-closed on parse errors (FR-003)
- A repeatable smoke test that exercises the contract end-to-end
- Documenting the deny contract in `docs/` (`continue:false` means STOP, period)

**Out of scope**:
- Changes to the chitin governance gate itself (already correct)
- Changes to the `continue:true` (soft block) path
- The Discord channel-ingress (spec 090) — orthogonal
- The mention-listener cull (spec 088) — orthogonal
- A user-facing "ask the operator" handshake on deny — this spec terminates, not negotiates

### Dependencies

- **Investigation predecessor**: today's investigation (2026-05-22) which surfaced 6 `lockdown_loop_detected` events for Clawta over recent days, with the chain payload literally stating "primary continue:false stop signal likely not honored — operator should investigate harness/driver."
- **Constitution**: this fix preserves §1 (kernel is the only gate authority) — the gate's decision stays load-bearing; the harness just stops disobeying it.
- **External dependency risk**: if the bug is in an upstream driver (e.g., Claude Code, an openclaw plugin), the fix's velocity depends on the upstream's responsiveness. Plan-phase identifies the locus and surfaces this risk as a known-blocker if applicable.
