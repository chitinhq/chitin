package activities

import (
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/worktree"
)

// SchedulerActivityDeps are the runtime dependencies the Spec-DAG Scheduler's
// activities (spec 076) need bound at worker-host startup. They are
// constructed in main once Temporal, the driver registry, and the worktree
// Manager exist, then handed to RegisterSchedulerActivities.
type SchedulerActivityDeps struct {
	// Registry is the spec-075 driver registry the SelectDriver activity
	// routes against. Required.
	Registry *driver.Registry
	// Worktrees is the spec-070 worktree Manager the CreateWorktree and
	// TeardownWorktree activities use. Required.
	Worktrees *worktree.Manager
	// Board is the write-only board read-model sink for ProjectToBoard. A nil
	// Board falls back to the logging projector — see the TODO on
	// logBoardProjector in board_projection.go.
	Board BoardProjector
	// Telemetry is the write-only per-tick telemetry sink for
	// EmitTickTelemetry. A nil Telemetry falls back to the logging sink.
	Telemetry TickTelemetrySink
}

// RegisterSchedulerActivities wires the Spec-DAG Scheduler's activities into
// the worker host. It is the activity half of spec 076: the SchedulerWorkflow
// and WorkUnitWorkflow (registered by the workflows package) dispatch to the
// activity names registered here.
//
// The activities registered:
//
//   - "SelectDriver"      — capability-based driver selection (FR-007).
//   - "CreateWorktree"    — fresh per-node worktree creation (FR-013).
//   - "TeardownWorktree"  — worktree teardown on work-unit completion (FR-008).
//   - "ProjectToBoard"    — node-state projection to the board (FR-014).
//   - "EmitTickTelemetry" — per-tick telemetry emission (FR-015).
//   - "InvokeDriver:<id>" — one per registered driver, for the driver
//     invocation inside WorkUnitWorkflow (spec 075 FR-007).
//
// main calls this once, after constructing deps, alongside the existing
// activities.Register. It is separate from Register because these activities
// carry startup-bound dependencies the Phase-0 smoke activities do not.
func RegisterSchedulerActivities(w worker.Worker, deps SchedulerActivityDeps) {
	selector := NewDriverSelector(deps.Registry)
	w.RegisterActivityWithOptions(selector.Execute, registerAs(selector.ActivityName()))

	wt := NewWorktrees(deps.Worktrees)
	w.RegisterActivityWithOptions(wt.CreateWorktree, registerAs(wt.CreateActivityName()))
	w.RegisterActivityWithOptions(wt.TeardownWorktree, registerAs(wt.TeardownActivityName()))

	board := NewBoardProjection(deps.Board)
	w.RegisterActivityWithOptions(board.Execute, registerAs(board.ActivityName()))

	tel := NewTickTelemetry(deps.Telemetry)
	w.RegisterActivityWithOptions(tel.Execute, registerAs(tel.ActivityName()))

	// One InvokeDriver activity per registered driver, each registered under
	// its per-driver name "InvokeDriver:<id>" so each agent's invocations are
	// individually inspectable in Temporal history.
	if deps.Registry != nil {
		for _, d := range deps.Registry.Drivers() {
			inv := driver.NewInvokeDriver(d)
			w.RegisterActivityWithOptions(inv.Execute, registerAs(inv.ActivityName()))
		}
	}
}

// registerAs is a tiny helper building the activity-registration options for
// a fixed activity name.
func registerAs(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}
