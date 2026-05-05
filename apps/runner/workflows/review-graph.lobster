name: review-graph
description: |
  Phase 2 §5 review-tier escalation chain — Lobster port of
  apps/temporal-worker/src/review-graph-workflow.ts.

  Tier model (matches REVIEW_TIER_DRIVER in review-graph.ts):
    R0 = Copilot bot's automatic review on PR open. NOT dispatched here —
         its findings arrive via copilot_comments and shape R1's prompt.
    R1 = first dispatched reviewer (Copilot CLI / GPT-4.1). Cheap, fast.
    R2 = mid-tier (Copilot CLI / Sonnet 4.6). Mid-complexity / API surface.
    R3 = top-tier (Opus 4.7). Heavy, last automated tier.
    R4 = operator pickup. NOT dispatched — modeled here as the approval
         gate's decision. operator approving = "I acknowledge, will pick up";
         denying = "override, accept the PR as-is despite R3's escalation".

  Each runner emits { "tier", "escalate", "decision", "findings", "ok" }
  on stdout and always exits 0 (Lobster's `when:` cannot read exit_code).

  Status: prototype. Wired against apps/temporal-worker/src/runner.ts via
  the chitin-agent-runner bin. The starting-tier optimization (skip R1/R2
  when computeStartingTier suggests so) is not modeled — always starts at R1.
  Add per-call when-conditions on R1/R2 if/when needed.

args:
  pr_meta_json:
    default: "{}"
    description: PR metadata as a JSON string (repo, pr_number, pr_url, head_sha, etc.)
  entry_id:
    default: ""
    description: Originating backlog entry id (for prompt + audit)
  entry_file_scope:
    default: ""
    description: Comma-separated files declared in the entry's scope
  repo:
    default: ""
    description: owner/name slug, e.g. chitinhq/chitin
  parent_workflow_id:
    default: standalone
    description: Programmer workflow id this review-graph hangs off
  required_approver:
    default: ""
    description: Operator id required at the R3→operator-pickup gate. Empty = any approver.

steps:
  - id: R1
    run: chitin-agent-runner --role reviewer --tier R1 --pr-meta "$LOBSTER_ARG_PR_META_JSON" --entry-id "$LOBSTER_ARG_ENTRY_ID" --entry-file-scope "$LOBSTER_ARG_ENTRY_FILE_SCOPE" --repo "$LOBSTER_ARG_REPO" --parent-workflow-id "$LOBSTER_ARG_PARENT_WORKFLOW_ID"
    on_error: continue

  - id: R2
    run: chitin-agent-runner --role reviewer --tier R2 --pr-meta "$LOBSTER_ARG_PR_META_JSON" --entry-id "$LOBSTER_ARG_ENTRY_ID" --entry-file-scope "$LOBSTER_ARG_ENTRY_FILE_SCOPE" --repo "$LOBSTER_ARG_REPO" --parent-workflow-id "$LOBSTER_ARG_PARENT_WORKFLOW_ID"
    when: "$R1.json.escalate == true"
    on_error: continue

  - id: R3
    run: chitin-agent-runner --role reviewer --tier R3 --pr-meta "$LOBSTER_ARG_PR_META_JSON" --entry-id "$LOBSTER_ARG_ENTRY_ID" --entry-file-scope "$LOBSTER_ARG_ENTRY_FILE_SCOPE" --repo "$LOBSTER_ARG_REPO" --parent-workflow-id "$LOBSTER_ARG_PARENT_WORKFLOW_ID"
    when: "$R2.json.escalate == true"
    on_error: continue

  - id: operator_pickup
    approval:
      prompt: "R3 escalated — operator pickup required. Approve = acknowledge handoff. Deny = override, accept PR as-is despite R3's escalation."
      required_approver: "$LOBSTER_ARG_REQUIRED_APPROVER"
    when: "$R3.json.escalate == true"
