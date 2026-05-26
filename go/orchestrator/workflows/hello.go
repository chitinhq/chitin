// Package workflows holds the Chitin Orchestrator's durable workflows — one
// per migrated cron/script (spec 070). Workflow code is deterministic and
// side-effect-free; all side effects go through activities.
package workflows

import (
	"time"

	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
)

// Register wires every workflow into the worker host.
func Register(w worker.Worker) {
	w.RegisterWorkflow(HelloWorkflow)
	w.RegisterWorkflow(SequenceWorkflow)
	w.RegisterWorkflow(SchedulerWorkflow)
	w.RegisterWorkflow(WorkUnitWorkflow)
	// ScheduledJobWorkflow registers under its function name —
	// "ScheduledJobWorkflow" — which is exactly the type name
	// schedules.EnsureSchedules names as the Schedule's action workflow
	// (spec 081 US2).
	w.RegisterWorkflow(ScheduledJobWorkflow)
	// PRReviewWorkflow is the dialectic review gate (spec 094). It is
	// dispatched directly by chitin-orchestrator pr-review (the CLI
	// landed in a follow-up PR) and as a child of PRMergeWorkflow (spec
	// 093). Its activities are registered separately by review.Register.
	w.RegisterWorkflow(PRReviewWorkflow)
	// SiblingRebaseWorkflow is the spec 112 US2 auto-rebase workflow. The
	// factory-listen merge handler fires one per open sibling PR when a
	// chitin-authored PR merges to main; the workflow invokes the
	// RebaseSiblingPR activity (registered by RegisterSchedulerActivities)
	// and returns its result.
	w.RegisterWorkflow(SiblingRebaseWorkflow)
	// PRIterationWorkflow is the spec 113 US1 PR comment-respond loop. The
	// factory-listen pull_request_review handler fires one per Copilot
	// review on a chitin-authored PR; the workflow invokes the
	// IteratePRReview activity (registered by RegisterSchedulerActivities)
	// and returns its result.
	w.RegisterWorkflow(PRIterationWorkflow)
	// AutoMergeWorkflow consumes the spec 116 ready-to-merge label and
	// performs the mechanical squash merge once CI and mergeability are green.
	w.RegisterWorkflow(AutoMergeWorkflow)
}

// HelloWorkflow is the Phase 0 smoke workflow (tasks.md T010). It proves the
// worker host, the task queue, an activity round-trip, and replay
// determinism — the foundation every later workflow builds on.
func HelloWorkflow(ctx workflow.Context, name string) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
	})

	var greeting string
	if err := workflow.ExecuteActivity(ctx, activities.Greet, name).Get(ctx, &greeting); err != nil {
		return "", err
	}
	return greeting, nil
}
