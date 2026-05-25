# Runbook: spec 113 PR comment-respond loop

The factory's autonomous review-iteration loop. When Copilot reviews a chitin-authored PR, the orchestrator re-invokes the authoring driver against the PR branch and pushes a fixup commit — no operator action required between webhook delivery and `git push`.

Shipped 2026-05-25 as US1 MVP (single-round; multi-round + comment-thread replies deferred).

## What the loop does

```
GitHub Copilot reviews chitin/wu/* PR
                │
                ▼
factory-listen :8765/webhook/pr
                │
                ▼
dispatchPRIteration ──► PRIterationWorkflow (Temporal)
                            │
                            ▼
                        IteratePRReview activity
                            │
       ┌────────────────────┼────────────────────┐
       ▼                    ▼                    ▼
worktree.Manager.    gh api fetch         driver invoke
   Checkout          review + comments    with prompt
                            │
                            ▼
                  git commit + push --force-with-lease
                            │
                            ▼
                  emit pr_iteration_completed
```

## When it fires

The trigger requires ALL of:

- Event type: `pull_request_review`
- Action: `submitted`
- Head branch: matches `^chitin/wu/.*` (factory-authored)
- Reviewer login: in the strict Copilot allowlist (`copilot-pull-request-reviewer`, `copilot`, `github-copilot[bot]`, case-insensitive)
- Review state: `commented` or `changes_requested`

Non-allowlisted reviewers (humans, other bots) silently no-op. Spec 113 US3 (deferred) will add explicit human-reviewer escalation.

## How to observe a round

**Live, on the operator host:**

```bash
# Tail the orchestrator journal for iteration workflow activity
journalctl --user -u chitin-orchestrator -f | grep -E "PRIteration|IteratePRReview"

# Watch the factory-listen request log
tail -f ~/.cache/chitin/factory-listen.jsonl | jq -c 'select(.route == "/webhook/pr")'

# Check chain events for completed rounds
grep "pr_iteration_completed" ~/.chitin/events-*.jsonl | jq -c '.payload'
```

**Via gh:**

```bash
# Confirm the fixup commit landed
gh pr view <N> --json commits --jq '.commits[-1].messageHeadline'
# expected: "review fix (round 1): address review #<M>"

# Confirm the PR's head moved
gh pr view <N> --json headRefOid --jq '.headRefOid[0:9]'
```

## How to trigger one manually

If you want to validate the loop end-to-end without waiting for a fresh Copilot review:

```bash
# 1. Find a recent pull_request_review delivery for an open chitin/wu/* PR
gh api "repos/chitinhq/chitin/hooks/630558210/deliveries?per_page=20" \
  --jq '.[] | select(.event == "pull_request_review")'

# 2. Redeliver it
gh api -X POST "repos/chitinhq/chitin/hooks/630558210/deliveries/<DELIVERY_ID>/attempts"

# 3. Watch the workflow fire (~2-10 min — driver invocation is the long leg)
journalctl --user -u chitin-orchestrator --since "1 min ago" | grep iteration-pr
```

The deterministic WorkflowID `iteration-pr-<N>-review-<M>` means duplicate webhook deliveries dedup via Temporal `REJECT_DUPLICATE` — safe to redeliver repeatedly.

## How to verify it actually worked

Check three things in order:

1. **Workflow started:** `journalctl --user -u chitin-orchestrator | grep "PRIterationWorkflow.*iteration-pr-<N>"` shows the activity invocation.
2. **Driver produced changes:** PR has a new commit with subject `review fix (round 1): address review #<M>`.
3. **Chain event fired:** `grep "pr_iteration_completed" ~/.chitin/events-*.jsonl | grep '"pr_number":<N>'` returns a JSON line with `pushed_fixup: true` and a `fixup_sha`.

If all three are present, the round completed successfully.

## Outcomes and what they mean

| Result | What it means | Operator action |
|---|---|---|
| `pushed_fixup: true` | Driver made changes, committed, force-pushed | None — autopilot |
| `pushed_fixup: false`, explanation = "driver returned success but produced no changes" | Driver chose not to change anything (intentional decline OR couldn't address the comments) | Inspect PR; consider manual fix or close |
| `pushed_fixup: false`, explanation = "review carried no body and no line comments" | Copilot approved with no comments | None — nothing to address |
| `pushed_fixup: false`, explanation = "worktree checkout failed: ..." | Branch fetch issue; usually transient (remote not yet updated) | Re-trigger via webhook redelivery |
| `pushed_fixup: false`, explanation = "force-push lost lease or failed" | Operator pushed to the branch concurrently | Pull operator's work, decide which fixup to keep |
| `pushed_fixup: false`, explanation = "driver returned non-success status ..." | Driver invocation faulted | Inspect orchestrator journal for the underlying driver error |

## v1 known limits (deferred to follow-ups)

- **Single round only**: a second Copilot review on the same PR fires a fresh workflow with a fresh ReviewID. No 1..N-up-to-cap iteration in v1.
- **No comment-thread replies**: driver output is committed as a fixup; no per-thread reply posting yet.
- **No per-spec iteration_cap**: single hardcoded round count.
- **No human-reviewer escalation event**: non-Copilot reviewers silently skip. Spec 113 US3 adds the explicit event.
- **No internal re-review** for rounds where Copilot doesn't re-review the fixup: spec 116 (multi-driver re-review) is the planned closer.

## Troubleshooting

**Webhook arrived but no workflow started:**

```bash
# Check that factory-listen received the event
tail -5 ~/.cache/chitin/factory-listen.jsonl | jq -c .

# Look for "skipped_reason" in the response
# Common values:
#   "event_type_ignored"   ← old spec 099 path; spec 113 may still have fired separately
#   "pr_iteration:non_factory_branch" ← branch doesn't match chitin/wu/*
#   "pr_iteration:invalid_input"       ← missing required field
```

If `pr_iteration_dispatched` is `false` and there's no `pr_iteration_*` reason, the eligibility filter rejected the event (reviewer not allowlisted, or branch doesn't match `chitin/wu/*`).

**Workflow started but activity never returned:**

```bash
# Find the in-flight driver subprocess
ps aux | grep -E "claude|codex" | grep co-iterate

# If alive: driver is still working (typically 3-10 min)
# If dead: check the orchestrator journal for the activity error
journalctl --user -u chitin-orchestrator --since "10 min ago" | grep "ActivityType IteratePRReview" -A 3
```

**Driver wrote changes but push failed:**

The activity uses `git push --force-with-lease`, which refuses to overwrite a concurrent push. If you pushed to the PR branch while iteration was in flight, the lease is lost and the fixup is dropped (chain event records the failure). The activity does NOT retry — re-trigger the webhook redelivery if you want another attempt.

**Discord ping is missing:**

Spec 113's iteration completion does NOT ping Discord — only escalations do (sibling rebase failures live, others in spec 116). A clean fixup completion is intentionally quiet — autopilot's job is to not spam.

## Related

- Spec 112 US2 sibling-rebase: [`docs/runbooks/`](.) (TBD)
- Spec 094 dialectic review: handles the INITIAL review verdict; spec 113 handles iteration on top of it
- Spec 116 multi-driver re-review (drafted, not implemented): closes the round-2+ gap when Copilot doesn't re-review
- Spec 114 operator queue (drafted, not implemented): single command that surfaces only PRs needing operator attention
