---
description: "Task list — 114 operator escalation surface"
---

- [ ] T001 [P] [US1] Implement `cmd/chitin-orchestrator/queue.go` with flag parsing — `--repo`, `--since`, `--format`, `--reason`. Default repo from `$CHITIN_REPO`; default since `168h`; default format `table`
- [ ] T002 [P] [US1] Implement `internal/queue/scan.go` — read chain events via the existing `chitin-kernel events` JSONL stream (extend if needed) filtered to the escalation event types; build an index `prNumber -> []EscalationEvent`
- [ ] T003 [P] [US1] Implement `internal/queue/live.go` — `gh pr list --json number,title,headRefName,labels,mergeable,updatedAt,reviews --search "is:open" --limit 100`. Decorate each PR with its label-derived spec_ref + most-recent-automated-commit age
- [ ] T004 [US1] Implement `internal/queue/filter.go` — compose live PRs + escalation events into the "needs operator" set per FR-003. Each rule returns a `(matched bool, reason string)` so the table column can show WHY
- [ ] T005 [US1] Implement `internal/queue/format_table.go` — text/tabwriter output with PR#, title (≤60 chars), reason, age, last-auto-action, spec_ref
- [ ] T006 [US1] Implement `internal/queue/format_md.go` — GitHub-flavoured markdown table; PR # as clickable link; emoji prefix per reason kind for scannability
- [ ] T007 [US1] Implement `internal/queue/format_json.go` — one JSON object per PR with all FR-005 fields + the raw triggering escalation event for downstream tooling
- [ ] T008 [US3] Implement `--reason KIND` filter — validate against the closed reason taxonomy (FR-008); error helpfully on unknown kinds
- [ ] T009 [US2] Add a new scheduled job in `go/orchestrator/schedules/operator_digest.go` (mirror existing scheduled-job pattern from spec 081) — runs at 09:00 daily, executes `queue --since 24h --format md` in-process (NOT via subprocess), posts result via `DiscordNotify`
- [ ] T010 [US2] Extend digest to include "since yesterday" delta: count of new escalations today, count of resolved-since-yesterday (PRs that had an escalation event but are now merged or closed), breakdown by reason
- [ ] T011 [US1] Hermetic test in `cmd/chitin-orchestrator/queue_test.go` — fixture chain events + fake gh, assert filter returns the expected escalated set across all reason kinds
- [ ] T012 [US1] Hermetic test for each format — table output is column-aligned, md output is valid markdown table, json output round-trips through `json.Unmarshal`
- [ ] T013 [US2] Hermetic test for the digest job — Temporal testsuite env, stub queue producing a known result, assert the Discord post fires with the right markdown body
- [ ] T014 [US1] Operator runbook `docs/runbooks/spec-114-queue.md` — example invocations, what each reason kind means, how to triage a typical morning's queue
- [ ] T015 [US1] Once spec 113 is deployed, measure SC-001: median queue size over 7 days vs. raw `gh pr list` count; aim for ≥60% reduction
