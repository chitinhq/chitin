// pr_iteration_input.go — spec 113 US1 typed input shared by the
// factory-listen dispatcher (cmd/chitin-orchestrator/pr_iteration_dispatch.go,
// task T002) and the workflow function itself (task T004 will land
// PRIterationWorkflow in pr_iteration.go alongside this struct).
//
// Held in its own file so T002 and T004 can land independently without
// stepping on a single pr_iteration.go file.

package workflows

// PRIterationInput is the typed input to PRIterationWorkflow — one
// Copilot (or allowlisted) review on a chitin-authored PR whose head
// branch matches `chitin/wu/*`. One workflow runs per (PR, review)
// pair; the dispatcher uses a deterministic WorkflowID
// `iteration-pr-<PRNumber>-review-<ReviewID>` so a redelivered webhook
// dedups via Temporal's `WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE`.
type PRIterationInput struct {
	// PRNumber is the chitin-authored pull request the review targets.
	PRNumber int `json:"pr_number"`
	// PRBranch is the PR's head ref — the branch the iteration's fixup
	// commit (if any) is force-pushed to.
	PRBranch string `json:"pr_branch"`
	// TargetRepo is the absolute path the worktree Manager mints the
	// per-iteration checkout under (reuses the spec 112 US2 worktree
	// path the same way SiblingRebaseWorkflow does).
	TargetRepo string `json:"target_repo"`
	// DriverID is the id of the driver that originally authored the PR
	// (spec 113 FR-004). May be empty when the dispatcher could not
	// resolve it from chain history; the workflow is responsible for
	// either resolving it itself or escalating with a clear reason.
	DriverID string `json:"driver_id"`
	// ReviewID is the GitHub review id (numeric) — together with
	// PRNumber it uniquely identifies the review the workflow iterates
	// against, and supplies the dedup key for Temporal's
	// REJECT_DUPLICATE policy.
	ReviewID int64 `json:"review_id"`
	// Round is the 1-indexed iteration round this workflow run
	// represents. Dispatcher always starts at 1; the workflow itself
	// increments and re-dispatches up to the configured cap
	// (spec 113 FR-007, default 3).
	Round int `json:"round"`
	// SchedulerRunID is the scheduler run that originally dispatched
	// this PR — the value of the PR's `sched/run/<id>` label
	// (activities.SchedRunLabelPrefix). Carried into chain events for
	// correlation across spec 113 FR-010 telemetry.
	SchedulerRunID string `json:"scheduler_run_id"`
}
