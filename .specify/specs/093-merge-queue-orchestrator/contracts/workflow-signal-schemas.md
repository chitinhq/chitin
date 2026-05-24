# Contract: Workflow Signal Schemas

**Producer**: operator via `temporal workflow signal` CLI, future automation
**Consumer**: `PRMergeWorkflow` child workflow
**Reference**: research.md R-SIG

This document is the canonical schema for the three signals `PRMergeWorkflow` accepts. The parent `MergeQueueWorkflow` accepts NO signals — see "Why the parent has no signals" at the bottom.

---

## Signal: `resume`

### Purpose

The operator resolved a problem on the PR (manually fixed a conflict and pushed, or the workflow was waiting on `Blocked`/`Draft` state that the operator has now corrected) and wants the workflow to retry from its last mergeability check.

### Payload

```json
{}
```

Empty object. No fields. Temporal still records the signal event in workflow history, including the time it arrived.

### Effect

| Workflow state on receipt | Behavior |
|---------------------------|----------|
| `paused` (after rebase halt on non-pointer conflict) | Re-fetch `PRSnapshot`; re-enter mergeability loop. |
| `waiting-checks` blocked on FAILURE conclusion | Re-fetch checks; if now passing, proceed; if still failing, pause again. |
| `paused` due to `Blocked` or `Draft` mergeability state | Re-fetch `PRSnapshot`; re-enter mergeability loop. |
| any other state | Recorded but ignored; workflow continues its current activity. |

### Operator invocation

```bash
# Resume by PR number (workflow ID convention)
temporal workflow signal \
  --workflow-id merge-pr-chitinhq-chitin-923 \
  --name resume

# Or with task queue / address overrides if needed
temporal workflow signal \
  --workflow-id merge-pr-chitinhq-chitin-923 \
  --name resume \
  --address localhost:7233
```

---

## Signal: `abort`

### Purpose

The operator wants this PR removed from the queue without merging. The workflow records the entry as aborted; the parent queue halts on this entry (FR-021).

### Payload

```json
{
  "reason": "human-readable string"
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `reason` | string | yes | 1–500 chars. Surfaced in OTLP telemetry and `EntryResult.Reason`. UTF-8. |

### Effect

| Workflow state on receipt | Behavior |
|---------------------------|----------|
| any non-terminal state | Cancel current activity (if any), emit final telemetry, return `EntryResult{Status: aborted, Reason: payload.reason}`. |
| `done` (terminal) | Ignored; workflow already complete. |

### Operator invocation

```bash
temporal workflow signal \
  --workflow-id merge-pr-chitinhq-chitin-923 \
  --name abort \
  --input '{"reason": "operator decided to land #999 first"}'
```

### Effect on parent queue

The parent's `awaitChild` returns the aborted result. The parent sets `HaltedAtIndex = <entry index>`, `HaltReason = "child aborted: <payload.reason>"`, marks all remaining entries as `not-attempted`, emits final telemetry, and terminates.

---

## Signal: `approve`

### Purpose

For governance-class PRs (or any PR where `ClassPolicy.RequiresApproval == true`), the workflow blocks at `waiting-approval` until an `approve` signal arrives. This honors the no-bypass invariant (FR-019): the submitter's identity does NOT exempt the gate; an explicit `approve` signal is required even when the operator is the submitter.

### Payload

```json
{
  "approver": "github-login-or-identifier",
  "note": "optional human-readable rationale"
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `approver` | string | yes | 1–100 chars. Recorded in telemetry as the approving party. May equal the submitter's identifier; the workflow does not check. |
| `note` | string | no | ≤ 500 chars. Surfaced in `EntryResult.Reason` prefix and telemetry. |

### Effect

| Workflow state on receipt | Behavior |
|---------------------------|----------|
| `waiting-approval` | Record approval in telemetry; transition to mergeability check; proceed. |
| any other state | Recorded but ignored (defensive: if a future spec adds mid-flow approval, the slot is reserved). |

### Operator invocation

```bash
# Approve a paused governance PR
temporal workflow signal \
  --workflow-id merge-pr-chitinhq-chitin-925 \
  --name approve \
  --input '{"approver": "jpleva91", "note": "Constitution amendment ratified by Ares + Clawta + operator; greenlit."}'
```

---

## Discovery: how the operator finds a paused workflow

Per FR-020, the workflow must be discoverable without prior knowledge of its ID.

Two paths:

### Path 1 — OTLP telemetry stream

Every state transition emits an OTLP event (FR-024). The operator's dashboard or `chitin-orchestrator telemetry tail --filter step=paused` shows currently-paused workflows with their IDs and pause reasons.

### Path 2 — Temporal list query

```bash
# All open merge-pr workflows
temporal workflow list \
  --query 'WorkflowType="PRMergeWorkflow" AND ExecutionStatus="Running"'

# Paused workflows (workflow ID convention encodes repo + PR for fast scan)
temporal workflow list \
  --query 'WorkflowType="PRMergeWorkflow" AND ExecutionStatus="Running"' \
  -f json | jq '.[] | select(.search_attributes.step == "paused" or .search_attributes.step == "waiting-approval")'
```

To make path 2 work, the workflow MUST register `step` as a custom search attribute and update it on each state transition. The implementation will use the Temporal `UpsertSearchAttributes` workflow primitive.

---

## Why the parent has no signals

Three observations led to the design decision (R-SIG) that no signal mechanism exists on `MergeQueueWorkflow`:

1. **Per-PR scoping**: every operator-affecting decision is about a specific PR. Per-PR workflows are the natural target.
2. **Queue abort is achievable**: signalling `abort` on the currently-running child causes the parent's `awaitChild` to return and the parent to halt (FR-021).
3. **Less signal indirection**: avoids "abort the queue at position N" semantics where the parent has to look up which child to forward to.

If a future requirement emerges to (e.g.) pause the entire queue while keeping the current PR running, that would justify a parent signal in a follow-up spec.

---

## Signal idempotency

All three signals are **idempotent in payload, not in effect**:

- Sending `resume` twice while paused: the first triggers a re-check; the second arrives during the re-check and is ignored (state has moved out of `paused`).
- Sending `abort` twice: the first transitions to terminal `aborted`; the second arrives at a terminal state and is ignored (with a defensive log).
- Sending `approve` twice: the first transitions out of `waiting-approval`; the second is ignored.

In all cases, Temporal records the signal in workflow history so the audit trail is complete.

---

## Signal-vs-update consideration

Temporal also offers `update` (synchronous signal-with-response). v1 uses signals (fire-and-forget) for all three operations because:

- The operator wants to know the workflow accepted the signal; the OTLP event for the resulting state transition is sufficient confirmation.
- Updates require a server-side validation handler and a synchronous reply contract, both of which add scope without enabling a v1 user story.

Future spec may upgrade `approve` to an `update` if multi-party approval (quorum) is added.
