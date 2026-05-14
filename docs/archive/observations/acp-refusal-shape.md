# ACP Refusal-Frame Visibility — Spike Resolution

**Date:** 2026-04-29
**Blocker for:** Milestone B (Copilot ACP shim) of cost-governance kernel v3.
**Source plan:** `docs/superpowers/plans/2026-04-29-cost-governance-kernel.md` §"Block on this milestone"
**Question:** "Confirm whether refusal frames are model-visible, or whether the shim must inject a synthetic tool response with the chitin Reason embedded."

## TL;DR

**Refusal IS visible to the Copilot agent and surfaces to its LLM as a generic "tool refused" signal — but the chitin Reason text has no protocol-level path to the model.**

For Milestone B v1, ship the standard ACP refusal path. The Reason text lives in chitin's audit log (`gov-decisions-<date>.jsonl`) and the operator's `envelope tail` view — not in the model context. Defer "embed Reason in synthetic tool response" to a follow-up milestone if a measurable harm from missing Reason is observed in live use.

## Evidence

### 1. ACP wire protocol (Zed Industries canonical schema)

Source: `https://raw.githubusercontent.com/zed-industries/agent-client-protocol/main/schema/schema.json`

**`ToolCallStatus` is a closed enum:**
```json
"oneOf": [
  { "const": "pending",   "description": "issued but not yet executed" },
  { "const": "executing", "description": "currently being executed" },
  { "const": "completed", "description": "completed successfully" },
  { "const": "error",     "description": "failed with an error" }
]
```

There is no `denied` / `refused` / `cancelled` terminal status. After a permission denial, the tool call lands in **`error`** — that's the only available failure terminal.

**`RequestPermissionOutcome` is a two-variant union:**
```json
"oneOf": [
  { "outcome": "cancelled" },
  { "outcome": "selected", "optionId": "<id>" }
]
```

The response carries an `optionId` (chosen from the agent-supplied option list), and no free-form reason field. Whatever text rides back to the model is bounded by what the agent's local code synthesizes from "client picked option X".

**`PermissionOption` is agent-supplied:**
```json
{
  "_meta"?: object | null,
  "optionId": "<id>",
  "name": "<human-readable label>",
  "kind": "allow_once" | "allow_always" | "reject_once" | "reject_always"
}
```

The agent provides options; the client picks one. The client cannot inject a new option with a custom Reason in `name`.

### 2. Copilot SDK behavior on refusal (rung 3/4 spike, 2026-04-24)

`scratch/copilot-spike/rung3-intercept/RESULT.md` and `rung4-gate/RESULT.md` confirmed for the **SDK path** (Go `@github/copilot-sdk`, not raw ACP, but same internal model loop):

- `OnPermissionRequest` returning a non-nil `error` → SDK sends `PermissionDecisionKindDeniedNoApprovalRuleAndCouldNotRequestFromUser` to the CLI subprocess.
- CLI fires `tool.execution_complete` (refused state, no side effect).
- A new `assistant.turn_start` follows — the model received the refusal as a tool result and is composing a follow-up.

This proves model-visibility of the **refused** signal at the Copilot product level. What's not proven by rung 3/4 evidence: whether the `error.Error()` string from `OnPermissionRequest` rides through to the model context, or is consumed inside the SDK shim. The rung result documented the deny string was passed at the SDK boundary but did not assert it surfaces verbatim in the LLM input.

### 3. Architectural constraints on the chitin shim

Topology in Milestone B:
```
openclaw acpx (ACP client)
        ↕  ACP frames over stdio
chitin shim (interceptor)
        ↕  ACP frames over stdio
copilot --acp --stdio (ACP agent, holds the LLM)
```

The shim sits between client and agent. ACP `session/update` notifications flow agent → client only. The shim cannot synthesize agent-bound `session/update` frames to deliver text to the agent's LLM context — the protocol doesn't carry frames in that direction.

The `_meta` field on `RequestPermissionResponse.outcome` is an extension point. Whether Copilot CLI surfaces `_meta.reason` (or any custom key) to its LLM is undefined by the spec and would require live confirmation against the Copilot CLI build the operator is running. Treating `_meta` as a reliable channel is unsafe without a verified contract.

## Resolution and impact on Milestone B

### Decision: ship standard refusal in v1

`internal/driver/copilot/acp/intercept_governance.go` on a deny:

1. Respond to `session/request_permission` with `{outcome: "selected", optionId: "<reject_once-id-from-agent-options>"}`.
2. Write the chitin Reason + Suggestion + CorrectedCommand to `gov-decisions-<date>.jsonl` via `gov.WriteLog` (already wired through `gov.Gate.Evaluate`).
3. The operator sees the Reason via `envelope tail` and on the audit log directly.
4. The Copilot LLM sees a generic "tool refused" — enough to pivot, but not the chitin-specific reason.

### Trade-off accepted

Cost: the Copilot LLM doesn't learn *why* the action was denied, only that it was. Past spike evidence (rung 3) shows the model pivots gracefully on refusal even without a reason — it acknowledges or rephrases. Operator-visible reasoning lands on the audit log, which is where chitin's "governance, not behavior change" thesis wants it anyway.

Benefit: implementation is straightforward, no hand-rolled forgery of `session/update` frames or `_meta` extension games against an undefined contract.

### Deferred: synthetic-tool-response path

If live use of milestone B shows the Copilot LLM looping or producing low-quality follow-ups because it can't read chitin's Reason, follow-up work would explore:

- **Option A — Forge a `session/update tool_call status=error content=[chitin-reason]` frame** synthesized from the agent direction, **before** responding to `session/request_permission`. The agent's own internal SDK should pick up the update and feed `content` to its LLM. This depends on Copilot's internal handling of agent-direction self-frames — unverified.
- **Option B — Use `_meta` extension on the response** with a chitin-specific key. Requires verifying Copilot CLI passes `_meta.reason` (or any custom key) into its LLM input. Depends on a Copilot version we don't control.
- **Option C — Custom MCP tool injection.** Register a chitin MCP "denial-context" tool that the agent invokes pre-tool-call to fetch policy context. Higher implementation cost; pulls chitin into the MCP surface, which is out of v1 scope.

None of A/B/C is required to unblock Milestone B v1. All are reasonable v2 candidates.

## Acceptance

This spike is the unblocker named in the v3 plan:
> **No B-tasks start until this is resolved.**

It is now resolved. Milestone B implementation can proceed using the standard refusal path. `intercept_governance.go` allow/deny semantics are:
- Allow → forward the request unchanged to the agent.
- Deny → respond with `outcome: selected, optionId: <agent-supplied reject_once id>`, write Decision row to audit log. No `_meta` injection, no synthetic `session/update` forgery.

Live verification of model behavior under deny still happens at Milestone B's "Live e2e" task — that test confirms (or refutes) the refusal is acted on as expected, and is the right point to revisit if a reason-injection path is needed.
