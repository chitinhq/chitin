# Quickstart: Merge Queue Orchestrator

**Audience**: operator (you, or future operators of the chitin-orchestrator)
**Prereqs**: `chitin-orchestrator` worker running; Temporal at `127.0.0.1:7233`; `gh` CLI authenticated for the target repos; `yq` for YAML editing (optional)

This quickstart walks through the canonical workflows and includes copy-pasteable recipes for verifying every success criterion (SC-001 through SC-010) from the spec.

---

## Recipe 1 — Submit the current 7-PR backlog (SC-001)

This is the orchestrator's first real workload and the SC-001 verification.

### Step 1: Stage the queue file

```bash
cat > /tmp/queue-spec092-followups.yaml <<'EOF'
version: 1
label: "Spec 092/091/087-090 backlog — orchestrator's first real workload"
policy_table_version: "1.0.0"
entries:
  - repo: chitinhq/chitin
    pr: 926
    expected_class: research-docs   # docs/strategy/ is NOT a v1.0.0 gov trigger; future docs/strategy/policy/ will be
    note: "Industry alignment research grounding §7."

  - repo: chitinhq/chitin
    pr: 927
    expected_class: spec-only
    depends_on: [0]
    note: "No-driver-bypass invariant — depends on §7 ratification."

  - repo: chitinhq/chitin
    pr: 919
    expected_class: spec-only
    note: "Spec 087 — retire kanban substrate."

  - repo: chitinhq/chitin
    pr: 920
    expected_class: spec-only
    note: "Spec 088 — cull agent-bus mention listeners."

  - repo: chitinhq/chitin
    pr: 921
    expected_class: spec-only
    note: "Spec 089 — retire pre-v2 skills."

  - repo: chitinhq/chitin
    pr: 922
    expected_class: spec-only
    note: "Spec 090 — Discord channel-ingress for @clawta."

  - repo: chitinhq/chitin
    pr: 924
    expected_class: bookkeeping
    note: "Mark spec 068 tasks complete."
EOF
```

### Step 2: Dry-run first

```bash
chitin-orchestrator merge-queue submit --dry-run /tmp/queue-spec092-followups.yaml
```

Expected output: per-entry classification matches the `expected_class` field. Validation PASSED.

### Step 3: Submit

```bash
chitin-orchestrator merge-queue submit /tmp/queue-spec092-followups.yaml
```

Captures the Submission ID and parent workflow ID. Open a tail in another shell:

```bash
temporal workflow show \
  -w merge-queue-${SUBMISSION_ID} \
  --follow
```

### Step 4: Verify completion (no approval needed for this backlog)

Under v1.0.0 policy, none of the 7 entries is governance-class — #926 is research-docs, the rest are spec-only or bookkeeping. No `approve` signals required.

(If the backlog had included a constitution amendment, this step would describe approving it via `temporal workflow signal ... --name approve`.)

### Step 5: Wait for the queue to complete

The remaining entries auto-classify, auto-rebase (#927 depends on #926 landing first; #919–#922 may also need pointer-file rebase as earlier PRs merge), and auto-merge. The parent workflow returns a `QueueResult`.

```bash
temporal workflow describe -w merge-queue-${SUBMISSION_ID} -o json | jq '.result'
```

**SC-001 PASS criterion**: `MergedCount == 7`, `HaltedAtIndex == -1`, every `Entries[i].BranchDeleted == true`.

---

## Recipe 2 — Verify auto-resolution of pointer-file conflict (SC-002)

This recipe sets up a deliberate pointer-file collision and verifies the orchestrator handles it without operator intervention.

### Setup

```bash
# Open two PRs on different branches that both update .specify/feature.json
# and CLAUDE.md (this is the default state of any spec-kit PR).

# Submit a queue containing both, ordered so the second will need to rebase
# after the first lands.
cat > /tmp/queue-pointer-test.yaml <<'EOF'
version: 1
label: "SC-002 verification: pointer-file auto-resolve"
policy_table_version: "1.0.0"
entries:
  - repo: chitinhq/chitin
    pr: ${PR_A}
    expected_class: spec-only
  - repo: chitinhq/chitin
    pr: ${PR_B}
    expected_class: spec-only
    depends_on: [0]
EOF
```

### Verify

```bash
chitin-orchestrator merge-queue submit /tmp/queue-pointer-test.yaml
# ... wait for completion ...

# Inspect the second PR's workflow history — should include an
# auto-resolved rebase event.
temporal workflow show -w merge-pr-chitinhq-chitin-${PR_B} | grep -E "RebaseWithPolicy|Outcome"
```

**SC-002 PASS criterion**: The PR_B workflow's `RebaseWithPolicy` activity returns `Outcome: auto-resolved`; the workflow proceeds to merge without any `paused` step; no human signal was sent.

---

## Recipe 3 — Verify real-conflict halt + operator-resumable gate (SC-003)

This recipe creates a non-pointer conflict and verifies the workflow halts safely and resumes correctly.

### Setup

```bash
# Open PR_X that modifies, say, README.md (or any non-allowlist file).
# Land an unrelated PR_Y that also modifies the same file (creating a
# guaranteed conflict against the new main).
# Submit PR_X in a queue.

cat > /tmp/queue-conflict-test.yaml <<EOF
version: 1
label: "SC-003 verification: real-conflict halt"
policy_table_version: "1.0.0"
entries:
  - repo: chitinhq/chitin
    pr: ${PR_X}
EOF

chitin-orchestrator merge-queue submit /tmp/queue-conflict-test.yaml
```

### Observe halt

```bash
# Workflow should reach `paused` step. Tail for the pause event:
temporal workflow show -w merge-pr-chitinhq-chitin-${PR_X} | grep -E "paused|Outcome: halt"

# Or query the OTLP stream for the paused event:
chitin-orchestrator telemetry tail --filter step=paused --filter pr=${PR_X}
```

### Resume after manual fix

```bash
# Operator clones the branch, resolves the conflict, force-pushes.
git fetch origin
git worktree add /tmp/wt-fix origin/<branch-of-PR_X>
cd /tmp/wt-fix
git rebase origin/main
# ...resolve manually, edit files, git add, git rebase --continue...
git push --force-with-lease

# Signal the workflow to retry.
temporal workflow signal \
  --workflow-id merge-pr-chitinhq-chitin-${PR_X} \
  --name resume
```

**SC-003 PASS criterion**: After the `resume` signal, the workflow re-enters mergeability check, finds the PR now clean, proceeds to merge. Workflow history shows the pause/resume cycle.

### Alternative: abort

```bash
temporal workflow signal \
  --workflow-id merge-pr-chitinhq-chitin-${PR_X} \
  --name abort \
  --input '{"reason": "operator decision: hold this PR for review"}'
```

**Alternative SC-003 PASS criterion**: workflow returns `EntryResult{Status: aborted}`; parent halts the queue at this position.

---

## Recipe 4 — Verify governance gate (SC-004)

Submit a queue containing a governance-class PR. Confirm the workflow blocks regardless of CI state.

```bash
# Submit a PR that touches .specify/memory/constitution.md (e.g. open a
# new amendment PR for testing). Verify the workflow blocks even though
# all checks are green and you submitted the queue.

# The workflow ID for inspection:
WORKFLOW_ID="merge-pr-chitinhq-chitin-${PR_GOV}"

# Confirm step is waiting-approval:
temporal workflow describe -w ${WORKFLOW_ID} \
  --search-attribute step | grep waiting-approval

# Wait 5 minutes without signalling — workflow stays paused.
sleep 300

# Confirm still paused:
temporal workflow describe -w ${WORKFLOW_ID} --search-attribute step | grep waiting-approval

# Approve:
temporal workflow signal \
  --workflow-id ${WORKFLOW_ID} \
  --name approve \
  --input '{"approver": "jpleva91", "note": "Approved for SC-004 test."}'

# Confirm proceeds to merge:
sleep 30
temporal workflow describe -w ${WORKFLOW_ID} | grep "ExecutionStatus.*Completed"
```

**SC-004 PASS criterion**: workflow remained in `waiting-approval` for 5 minutes without action; only the `approve` signal caused the merge to proceed.

---

## Recipe 5 — Verify lease-protected force-push (SC-005)

Verify that a force-push race fails safely.

```bash
# Submit a queue containing PR_Z that needs rebase.
# Before the orchestrator finishes its rebase, push an unrelated commit
# to the same branch from another shell:

# Shell 1: submit the queue (this will trigger a rebase activity)
chitin-orchestrator merge-queue submit /tmp/queue-with-PRZ.yaml

# Shell 2 (during the rebase activity, ~30s window):
cd /tmp/some-other-checkout
git fetch
git checkout <branch-of-PR_Z>
git commit --allow-empty -m "race: push from operator"
git push

# Back to Shell 1: the orchestrator's force-push-with-lease should fail.
# Workflow history should show ForcePushWithLease activity returning
# Pushed: false with Reason "remote moved unexpectedly".
temporal workflow show -w merge-pr-chitinhq-chitin-${PR_Z} | grep -E "ForcePushWithLease|remote moved"
```

**SC-005 PASS criterion**: workflow detected the race, did NOT overwrite the remote, entered `paused` state for operator inspection.

---

## Recipe 6 — Verify worker-restart resilience (SC-006)

```bash
# Submit a 3-PR queue.
chitin-orchestrator merge-queue submit /tmp/queue-3pr-restart-test.yaml

# Note the submission ID. Wait for one PR to merge.
sleep 60

# Kill the worker mid-queue:
sudo systemctl restart chitin-orchestrator

# Verify queue resumes from where it left off:
temporal workflow describe -w merge-queue-${SUBMISSION_ID} \
  --search-attribute current_entry_index

# Wait for completion.
temporal workflow describe -w merge-queue-${SUBMISSION_ID} -o json | jq '.result'
```

**SC-006 PASS criterion**: `QueueResult.MergedCount == 3` after restart; no PR appears merged twice in `git log` against `main`.

---

## Recipe 7 — Verify branch deletion (SC-007)

After Recipe 1 completes:

```bash
# Pull latest main
cd /home/red/workspace/chitin && git fetch --prune

# List remote branches that should have been deleted
for pr in 926 927 919 920 921 922 924; do
  branch=$(gh pr view $pr --json headRefName --jq '.headRefName')
  if git ls-remote --exit-code origin $branch >/dev/null 2>&1; then
    echo "STILL EXISTS: $branch (PR #$pr)"
  else
    echo "deleted: $branch (PR #$pr)"
  fi
done
```

**SC-007 PASS criterion**: every line is `deleted`; the queue summary's `Entries[*].BranchDeleted` is true for every merged entry.

---

## Recipe 8 — Verify telemetry stream reconstruction (SC-008)

```bash
# Tail OTLP events for a recent submission
chitin-orchestrator telemetry tail \
  --filter submission_id=${SUBMISSION_ID} \
  --since "1h ago" \
  -o json | jq '[.[] | {time, step, pr, reason}]'

# Expected output: one event per state transition, per PR. Reconstructable
# sequence: submitted → classified → (waiting-approval → approved) →
# rebasing → pushing → waiting-checks → merging → done.
```

**SC-008 PASS criterion**: the OTLP stream contains a complete chronological sequence for every queue position; final state matches `QueueResult`.

---

## Recipe 9 — Verify operator inspection of in-flight queue (SC-009)

```bash
# While a queue is in flight:
WORKFLOW_ID="merge-queue-${SUBMISSION_ID}"

# Single command returns position, state, last reason:
temporal workflow describe -w ${WORKFLOW_ID} \
  --search-attribute current_entry_index \
  --search-attribute current_step \
  --search-attribute last_reason
```

**SC-009 PASS criterion**: one command returns all three. (The implementation uses Temporal's custom search attributes upsert primitive to make these queryable.)

---

## Recipe 10 — Verify orchestrator overhead under 10% (SC-010)

```bash
# Submit a 5-PR queue with no expected conflicts and all checks green.
START=$(date +%s)
chitin-orchestrator merge-queue submit /tmp/queue-5pr-perf-test.yaml
# ... wait for completion ...
END=$(date +%s)
TOTAL=$((END - START))

# Extract per-PR CI durations from the merged PRs.
CI_TIME=0
for pr in $(jq '.entries[].pr' /tmp/queue-5pr-perf-test.yaml); do
  pr_ci=$(gh pr view $pr -R chitinhq/chitin --json statusCheckRollup \
    --jq '[.statusCheckRollup[] | select(.completedAt) | (
      ((.completedAt | fromdateiso8601) - (.startedAt | fromdateiso8601))
    )] | max // 0')
  CI_TIME=$((CI_TIME + pr_ci))
done

# Orchestrator overhead = TOTAL - CI_TIME, expressed as % of TOTAL.
OVERHEAD=$((TOTAL - CI_TIME))
PCT=$(echo "scale=2; $OVERHEAD * 100 / $TOTAL" | bc)
echo "Total: ${TOTAL}s, CI: ${CI_TIME}s, Orchestrator overhead: ${OVERHEAD}s (${PCT}%)"
```

**SC-010 PASS criterion**: `${PCT} < 10`.

---

## Common failure modes and recovery

### "ALREADY_EXISTS: workflow merge-pr-chitinhq-chitin-NNN is already running"

Another submission already started a merge for the same PR. Either wait for it or query its state:

```bash
temporal workflow describe -w merge-pr-chitinhq-chitin-NNN
```

### Workflow stuck in `Unknown` mergeability state

GitHub's mergeability computation can lag. The workflow polls every 5s with backoff. If it persists beyond 5 minutes, abort and resubmit.

### "policy_table_version mismatch"

Operator's YAML references a different policy table version than the running binary. Either update the YAML or pass `--override-policy-version` and document the override in the operator's runbook.

### Worker can't reach Temporal

```bash
# Check Temporal is up:
temporal operator cluster health

# Restart the orchestrator if needed:
sudo systemctl restart chitin-orchestrator

# Re-submission is safe — Temporal workflow IDs are deduplicated.
```

---

## Cleanup

After a queue completes, no operator action is needed for the orchestrator. The Temporal workflow history retention is set by the namespace policy (default 30 days for chitin). Queue summaries appear in OTLP retention windows.

For the local `/tmp/queue-*.yaml` files used in these recipes:

```bash
rm /tmp/queue-*.yaml
```
