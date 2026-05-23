# Contract: workflow signal schemas (re-review, override-review)

**Spec reference**: FR-021, FR-022, FR-023 | **Research**: R-RERUN, R-OVERRIDE

Both signals are addressed to the parent `PRMergeWorkflow` (spec 093), NOT to `PRReviewWorkflow`. The parent owns the per-PR control surface; the child review workflow is spawned, cancelled, and respawned by the parent in response to these signals.

---

## Signal: `re-review`

**Purpose**: Operator requests a fresh review pass on a PR whose merge workflow's review gate is `blocked` or `halted`. A re-review captures a new snapshot, dispatches a fresh pair of primaries, and runs a fresh dialectic.

### Payload

```json
{
  "reason": "addressed both blockers in commits abc123..def456"
}
```

- `reason` — Required, non-empty string. Free-text explanation of what the operator did to warrant a re-review (e.g., commit reference, response to a blocker). Recorded in telemetry; not consumed by the workflow logic.

### Parent-workflow handler behaviour

```
on receive re-review(reason):
  state = current_review_gate_state
  case state of
    in-flight:
      # Per spec edge case "Re-review signal arrives during an in-flight review: Ignored"
      emit_telemetry("re-review.dropped", reason="gate in flight")
      return
    blocked | halted:
      emit_telemetry("re-review.accepted", reason=reason)
      spawn_new_PRReviewWorkflow_child(fresh_snapshot)
      return
    passed:
      # Gate already passed; review re-run not meaningful. Operator should let merge proceed
      # or use abort if they need to halt the merge.
      emit_telemetry("re-review.rejected", reason="gate already passed")
      return
```

### Idempotency

Two re-review signals received within the same workflow tick are treated as one (debounce). The second signal emits a `re-review.dropped` telemetry event and is otherwise ignored.

### Effect on workflow history

- The previous `PRReviewWorkflow` child's history is preserved (verdicts are immutable per FR-015/034).
- A new `PRReviewWorkflow` child execution appears in history with a fresh execution ID, a fresh snapshot, and a fresh dialectic.

---

## Signal: `override-review`

**Purpose**: Operator overrides a `blocked` review gate on a non-governance PR. The override does NOT trigger a new review pass — it bypasses the gate by operator authority and lets the parent merge workflow proceed to merge.

### Payload

```json
{
  "reason": "blocker is a stylistic disagreement; spec 094 explicitly allows operator override on non-governance"
}
```

- `reason` — Required, non-empty string. Free-text explanation of why the override is justified. Recorded immutably in telemetry and workflow history; this is the override's audit substrate.

### Parent-workflow handler behaviour

```
on receive override-review(reason):
  if pr_policy_class == "governance":
    # FR-023: governance is never overrideable.
    emit_structured_error_to_signal_sender("governance_no_override", "...")
    emit_telemetry("override-review.rejected", reason="governance class")
    return
  if reason is missing or empty:
    emit_structured_error_to_signal_sender("override_requires_reason", "...")
    emit_telemetry("override-review.rejected", reason="missing reason")
    return
  state = current_review_gate_state
  if state != blocked:
    # Override only makes sense on blocked gates. halted is on a different signal (resume).
    emit_structured_error_to_signal_sender("override_only_valid_on_blocked", "...")
    return
  emit_telemetry("override-review.accepted", operator_reason=reason)
  mark_gate_passed_via_override(reason)
  proceed_to_merge()
```

### Class invariant (FR-023, FR-020)

| Class | Override permitted? |
|---|---|
| `governance` | NO — signal is rejected. |
| `spec-only` | YES — operator may override. |
| `impl` | YES |
| `live-fix` | YES |
| `bookkeeping` | YES |
| `research-docs` | YES |

The governance non-override invariant is encoded in the parent signal handler, NOT in the policy table. This means a policy mutation cannot accidentally allow governance overrides. The invariant is irrevocable per-class.

### Effect on workflow history

The blocked verdicts that caused the gate to be `blocked` remain immutable in history. An additional `override-review.accepted` event appears in history with the operator's `reason`. The gate is then marked `passed (override)`, and merge proceeds. The merge commit's resulting telemetry event records `gate.state = "passed (override)"` so a downstream consumer can tell merged-via-review from merged-via-override.

---

## Signal: `cancel-review` (internal, not operator-facing)

Sent by the parent `PRMergeWorkflow` to a running `PRReviewWorkflow` child when:

- The parent receives a re-review signal (cancel the in-flight child before spawning a new one).
- The parent receives an `abort` signal (cancel the whole merge — child cancellation cascades).
- The PR is closed or marked draft (the periodic mergeability check detected the state change).

Payload:

```json
{ "reason": "re-review" }
```

The child workflow handles cancellation by:

1. Cancelling all in-flight reviewer-dispatch activities (`activity.GetInfo(ctx).ActivityID` plus Temporal's cancellation propagation).
2. Recording a `cancelled` outcome for each cancelled `ReviewerInvocation` in workflow history.
3. Returning a `ReviewGateDecision{State: GateCancelled, Reason: cancel-reason}` to the parent.

Note: `GateCancelled` is a fourth gate-state value used only for this internal cancellation signaling — the operator-facing states remain `passed | blocked | halted`. The parent surfaces `cancelled` as the abort-signal acknowledgement, not as a gate decision.

---

## Operator-facing surface summary

| Signal | Sent by | Parent or child? | Class restriction |
|---|---|---|---|
| `re-review` | operator | parent | none |
| `override-review` | operator | parent | rejected on `governance` |
| `cancel-review` | internal (parent → child) | child | n/a |
| `resume`, `abort`, `approve` | operator | parent | (defined in spec 093) |

The operator surface for these signals is whatever spec 093's CLI subcommand exposes — `chitin-orchestrator merge-queue signal <workflow-id> <signal-name> --reason "..."` is the working candidate. The exact CLI surface is part of spec 093, not this spec.
