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
