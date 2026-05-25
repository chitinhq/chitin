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
	// Telemetry is the write-only per-tick telemetry sink for
	// EmitTickTelemetry. A nil Telemetry falls back to the logging sink.
	Telemetry TickTelemetrySink
	// Notifier is the write-only human-notification sink for DiscordNotify
	// (spec 080 US2). A nil Notifier falls back to the logging notifier.
	Notifier Notifier
}

// RegisterSchedulerActivities wires the Spec-DAG Scheduler's activities into
// the worker host. It is the activity half of spec 076: the SchedulerWorkflow
// and WorkUnitWorkflow (registered by the workflows package) dispatch to the
// activity names registered here.
//
// The activities registered:
//
//   - "SelectDriver"          — capability-based driver selection (FR-007).
//   - "CreateWorktree"        — fresh per-node worktree creation (FR-013).
//   - "TeardownWorktree"      — worktree teardown on work-unit completion (FR-008).
//   - "RunDeterministicStep"  — mechanical-step execution for a deterministic
//     node, no driver and no token cost (FR-017).
//   - "DeliverWorkProduct"    — commit, push, and open a PR for a completed
//     agent work unit's worktree (spec 070 PR-out gate).
//   - "DiscordNotify"         — post a work event to the human notification
//     channel (spec 080 US2).
//   - "EmitTickTelemetry"     — per-tick telemetry emission (FR-015).
//   - "InvokeDriver:<id>"     — one per registered driver, for the driver
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

	// RunDeterministicStep carries no startup-bound dependency — a
	// deterministic step is a self-contained mechanical command (FR-017).
	step := NewDeterministicStep()
	w.RegisterActivityWithOptions(step.Execute, registerAs(step.ActivityName()))

	// DeliverWorkProduct carries no startup-bound dependency — committing,
	// pushing, and opening a PR is a self-contained sequence over the worktree.
	deliver := NewDeliverWorkProduct()
	w.RegisterActivityWithOptions(deliver.Execute, registerAs(deliver.ActivityName()))

	// RebaseSiblingPR is the spec 112 US2 auto-rebase activity. It needs
	// the same worktree Manager as Create/Teardown so a sibling-rebase
	// checkout participates in the same teardown + GC lifecycle.
	rebase := NewRebaseSiblingPR(deps.Worktrees)
	w.RegisterActivityWithOptions(rebase.Execute, registerAs(rebase.ActivityName()))

	// IteratePRReview is the spec 113 US1 PR comment-respond activity. It
	// needs the worktree Manager (for the PR-branch checkout via the
	// Manager.Checkout pattern from spec 112 US2) AND the driver registry
	// (to re-invoke the authoring driver by id).
	iterate := NewIteratePRReview(deps.Worktrees, deps.Registry)
	w.RegisterActivityWithOptions(iterate.Execute, registerAs(iterate.ActivityName()))

	// IterateSpecReview is the spec 115 US1 spec-PR comment-respond
	// activity. Same deps as IteratePRReview — worktree Manager for the
	// PR-branch checkout, driver registry so the workflow's SelectDriver
	// step can be resolved by id at invoke time.
	iterateSpec := NewIterateSpecReview(deps.Worktrees, deps.Registry)
	w.RegisterActivityWithOptions(iterateSpec.Execute, registerAs(iterateSpec.ActivityName()))

	// DiscordNotify posts work events to the human notification channel
	// (spec 080 US2). A nil Notifier falls back to the logging notifier.
	notify := NewDiscordNotify(deps.Notifier)
	w.RegisterActivityWithOptions(notify.Execute, registerAs(notify.ActivityName()))

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
