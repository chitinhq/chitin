name: review-graph
description: |
  Phase 2 §5 review-tier escalation chain — Lobster port of
  apps/temporal-worker/src/review-graph-workflow.ts. Each reviewer tier
  emits a JSON envelope { "escalate": bool, "decision": "...", "findings": [...] }
  and the next tier fires only if the prior one set escalate=true. R3 → R4
  is gated behind operator approval (chitin's "escalate-operator" branch
  in the original workflow).

  Status: prototype. Invokes a stub `chitin-agent-runner` for proving the
  Lobster mechanism works for chitin's review-graph; real wrapping around
  apps/temporal-worker/src/activity.ts:runAgentTurn lands in a follow-up.

args:
  pr_meta_json:
    default: "{}"
    description: PR metadata as a JSON string (repo, number, head_sha, files_changed, etc.)
  entry_id:
    default: ""
    description: Originating backlog entry id (for audit / file scope)
  required_approver:
    default: ""
    description: Operator id required at the R3→R4 approval gate. If empty, gate accepts any approver.

steps:
  - id: R0
    run: chitin-agent-runner --role reviewer --tier R0 --pr-meta "$LOBSTER_ARG_PR_META_JSON" --entry "$LOBSTER_ARG_ENTRY_ID"
    on_error: continue

  - id: R1
    run: chitin-agent-runner --role reviewer --tier R1 --pr-meta "$LOBSTER_ARG_PR_META_JSON" --entry "$LOBSTER_ARG_ENTRY_ID"
    when: "$R0.json.escalate == true"
    on_error: continue

  - id: R2
    run: chitin-agent-runner --role reviewer --tier R2 --pr-meta "$LOBSTER_ARG_PR_META_JSON" --entry "$LOBSTER_ARG_ENTRY_ID"
    when: "$R1.json.escalate == true"
    on_error: continue

  - id: R3
    run: chitin-agent-runner --role reviewer --tier R3 --pr-meta "$LOBSTER_ARG_PR_META_JSON" --entry "$LOBSTER_ARG_ENTRY_ID"
    when: "$R2.json.escalate == true"
    on_error: continue

  - id: gate
    approval:
      prompt: "R3 escalated — fire R4 (final / opus) reviewer?"
      required_approver: "$LOBSTER_ARG_REQUIRED_APPROVER"
    when: "$R3.json.escalate == true"

  - id: R4
    run: chitin-agent-runner --role reviewer --tier R4 --pr-meta "$LOBSTER_ARG_PR_META_JSON" --entry "$LOBSTER_ARG_ENTRY_ID"
    when: "$gate.approved && $R3.json.escalate == true"
    on_error: continue
