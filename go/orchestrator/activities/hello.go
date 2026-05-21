// Package activities holds the Chitin Orchestrator's side-effecting steps
// (spec 070). Activities — never workflows — touch the board, gh, agents,
// and worktrees. Each is retryable and timeout-bounded.
package activities

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/worker"
)

// Register wires every activity into the worker host.
func Register(w worker.Worker) {
	w.RegisterActivity(Greet)
	w.RegisterActivity(ParseTasks)
}

// Greet is the Phase 0 smoke activity (tasks.md T010) — the activity half of
// HelloWorkflow's round-trip.
func Greet(_ context.Context, name string) (string, error) {
	if name == "" {
		name = "chitin"
	}
	return fmt.Sprintf("Chitin Orchestrator online — hello, %s", name), nil
}
