package review

import (
	"fmt"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// RegisterDeps are the runtime dependencies the dialectic review activities
// (spec 094) need bound at worker-host startup. They are constructed in
// main once Temporal and the driver registry exist, then handed to Register.
//
// Mirrors the SchedulerActivityDeps shape so a future operator reading
// main.go sees one pattern, not two.
type RegisterDeps struct {
	// Registry is the spec-075 driver registry SelectReviewers and
	// DispatchMachineReviewer route against. Required: SelectReviewers
	// returns an activity-level error if it is nil, which would halt the
	// dialectic gate on every PR with a configuration-fault message.
	Registry *driver.Registry
	// Gh is the gh-CLI runner CapturePRSnapshot uses. Optional: a nil
	// runner falls back to the default `exec.CommandContext("gh", ...)`
	// shell-out, which is what production wants. Tests inject a fake.
	Gh GhRunner
}

// Register wires every dialectic review activity into the worker host.
// Mirrors activities.RegisterSchedulerActivities for symmetry; the
// workflow itself (PRReviewWorkflow) is registered separately by
// workflows.Register.
//
// Registers four activities by their declared ActivityName const so the
// PRReviewWorkflow's string-name dispatch (workflow.ExecuteActivity with
// e.g. "SelectReviewers", "CapturePRSnapshot") resolves through Temporal's
// activity registry — no compile-time coupling between workflow and
// activity packages.
//
// Returns an error if a required dep is missing rather than letting the
// worker boot into a state where the dialectic silently fails on every
// PR. The caller (main.go) logs and degrades — currently a hard fail
// matches the spec-097 schedule pattern.
func Register(w worker.Worker, deps RegisterDeps) error {
	if deps.Registry == nil {
		return fmt.Errorf("review.Register: Registry is required (SelectReviewers + DispatchMachineReviewer route against it)")
	}

	// SelectReviewers — pool selection (FR-004-007).
	sr := NewSelectReviewers(deps.Registry)
	w.RegisterActivityWithOptions(sr.Execute, activity.RegisterOptions{Name: sr.ActivityName()})

	// CapturePRSnapshot — first activity the workflow runs (R-SNAP).
	cs := NewCapturePRSnapshot(deps.Gh)
	w.RegisterActivityWithOptions(cs.Execute, activity.RegisterOptions{Name: cs.ActivityName()})

	// DispatchMachineReviewer — per-reviewer driver dispatch (FR-002, R-VTRANSPORT).
	dm := NewDispatchMachineReviewer(deps.Registry)
	w.RegisterActivityWithOptions(dm.Execute, activity.RegisterOptions{Name: dm.ActivityName()})

	// EmitReviewTelemetry — per-invocation telemetry sink (FR-032).
	// Currently a no-op (the actual OTLP emit lands in a follow-up PR);
	// the workflow registers and calls it regardless so the telemetry
	// hook point exists from day one.
	w.RegisterActivityWithOptions(EmitReviewTelemetry, activity.RegisterOptions{Name: EmitReviewTelemetryActivityName})

	return nil
}
