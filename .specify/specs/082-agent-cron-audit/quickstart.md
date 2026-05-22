# Quickstart: Safe Cron-Registry Mutation Runbook

**Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

The procedure every Phase B / Phase C registry mutation follows. The registries
are live files edited while their agents may be running — the protocol below
makes every change backed-up, staged, and restart-safe (research.md Decision 3).

## 0. Preconditions

- Know the target registry: `~/.hermes/cron/jobs.json` (Hermes) or
  `~/.openclaw/cron/jobs.json` (OpenClaw).
- Have the job's decision-log record in hand — `disposition`, `job_id`,
  `registration_source`, and (for `migrate`) `replacement_workflow`.

## 1. Snapshot the registry (always first)

```bash
cp ~/.hermes/cron/jobs.json \
   ~/.hermes/cron/jobs.json.bak-082-audit-$(date +%Y%m%d-%H%M%S)
```

(Equivalent for `~/.openclaw/cron/jobs.json`.) Rollback = restore this file.

## 2. Re-confirm the snapshot counts

Re-list the registry and confirm it still holds the expected job set (30 Hermes
/ 9 OpenClaw) before mutating — the audit snapshot is dated, so counts are
re-confirmed at execution time (spec Assumptions).

```bash
hermes cron list          # Hermes
```

## 3. Disable before delete (FR-011)

Never delete an enabled job in one step. First disable it, then let one
scheduled cycle pass and confirm nothing depended on it:

```bash
hermes cron pause <job_id>       # Hermes  — confirmed 2026-05-21
openclaw cron disable <job_id>   # OpenClaw — confirmed 2026-05-21
```

For a `disable` disposition, stop here.

## 4. Delete

For a `delete` disposition, after the job has been disabled and observed:

```bash
hermes cron delete <job_id>      # Hermes  (aliases: remove, rm)
openclaw cron rm <job_id>        # OpenClaw — confirmed 2026-05-21
```

For the `clawta-stale-worker-watchdog` triplicate: delete the three
`glm-agent`-owned records **by id** (the name is duplicated), keep one.

## 5. Restart-durability check (FR-005, SC-004)

A registry-row delete is **not** durable if an agent re-registers the job on
start. After a delete:

1. Restart the owning agent.
2. Re-list the registry.
3. If the deleted job reappeared → its `registration_source` is
   `agent-managed`. Correct that startup/role path, then repeat from step 1.
4. The delete is durable only once the job stays gone across a restart.

## 6. Migration gate (Phase C only — FR-004, FR-009)

For a `migrate` job, before disabling the cron:

1. Confirm the `replacement_workflow` is **proven** — it has run beside the
   cron and produced the same outcome (research.md Decision 4).
2. Disable the cron in the **same change** that confirms the workflow owns the
   work — no window where both run, none where neither runs.
3. Verify the work still happens, exactly once, from the workflow.
4. Delete the cron registration later, once the workflow has soaked.

If the replacement workflow is not yet proven, **stop** — leave the job
`migrate`-pending; do not delete into a coverage gap.

## 7. Rollback

Any step gone wrong — restore the snapshot from step 1 and restart the agent:

```bash
cp ~/.hermes/cron/jobs.json.bak-082-audit-<timestamp> \
   ~/.hermes/cron/jobs.json
```

## Acceptance verification

- `hermes cron list` → OpenClaw registry shows exactly one
  `clawta-stale-worker-watchdog` (SC-002).
- No job is `enabled` with a perpetual `error` last-status (SC-003).
- Every executed delete survives an agent restart (SC-004).
- No logical job observed running from both a cron and a workflow (SC-005).
