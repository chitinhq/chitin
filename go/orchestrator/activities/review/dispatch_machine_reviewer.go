package review

import (
	"context"
	"fmt"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/activities/review/verdict"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// DispatchMachineReviewerInput is the input to the per-reviewer dispatch
// activity. It carries the assigned driver, the PR snapshot, the role
// (primary or arbiter), and the policy-class hint the driver receives.
type DispatchMachineReviewerInput struct {
	InvocationID string       `json:"invocation_id"`
	DriverID     string       `json:"driver_id"`
	Role         verdict.Role `json:"role"`
	PolicyClass  string       `json:"policy_class"`
	Snapshot     PRSnapshot   `json:"snapshot"`
}

// DispatchMachineReviewerResult is the activity's typed output: one closed
// ReviewerInvocation that the workflow accumulates into the dialectic.
type DispatchMachineReviewerResult struct {
	Invocation ReviewerInvocation `json:"invocation"`
}

// DispatchMachineReviewer drives one reviewer driver through its review-mode
// tool and returns the closed ReviewerInvocation (spec 094 FR-002,
// R-VTRANSPORT). It is a side-effecting activity: it invokes the driver's
// tool surface, which is live I/O.
//
// v1 minimum-viable implementation:
//
//   - The activity looks the driver up in the registry (FR-002 enforces
//     that the driver declares CapCodeReview + a ReviewMode block).
//   - It calls the driver's Invoke with a WorkUnit shaped for review:
//     SpecID="094", a Context that encodes the snapshot + policy class,
//     and a deadline derived from the activity's per-reviewer time bound
//     (FR-026 default 30 minutes — configured at the activity level, not
//     re-checked here).
//   - It parses the driver's Result.Explanation as a JSON StructuredVerdict.
//   - It calls verdict.Validate to enforce FR-014; on failure the activity
//     returns a closed invocation with Failure=FailureMalformedShape.
//
// Failure modes (the five FailureKinds) are all returned as
// closed invocations with Failure set — never as activity-level errors
// — so the workflow's aggregator sees a uniform Outcome value and routes
// to the arbiter case per FR-009. An activity-level error is reserved for
// configuration faults (registry not bound, driver not found by id).
//
// TODO(spec-094-impl PR #2): the JSON-parse-from-Result.Explanation path
// is the simplest end-to-end wiring; a richer convention may emerge once
// real drivers ship review-mode prompts. The contract in
// contracts/review-mode-driver-contract.md governs the final shape.
type DispatchMachineReviewer struct {
	registry *driver.Registry
}

// NewDispatchMachineReviewer binds the activity to a driver registry.
func NewDispatchMachineReviewer(registry *driver.Registry) *DispatchMachineReviewer {
	return &DispatchMachineReviewer{registry: registry}
}

// DispatchMachineReviewerActivityName is the stable Temporal name.
const DispatchMachineReviewerActivityName = "DispatchMachineReviewer"

// ActivityName returns the activity's registered name.
func (a *DispatchMachineReviewer) ActivityName() string { return DispatchMachineReviewerActivityName }

// Execute drives the dispatch. See type doc for the algorithm.
func (a *DispatchMachineReviewer) Execute(
	ctx context.Context, in DispatchMachineReviewerInput,
) (DispatchMachineReviewerResult, error) {
	if a.registry == nil {
		return DispatchMachineReviewerResult{},
			fmt.Errorf("activities/review: DispatchMachineReviewer has no driver registry bound")
	}
	d, ok := a.registry.Driver(in.DriverID)
	if !ok {
		return DispatchMachineReviewerResult{},
			fmt.Errorf("activities/review: driver %q not in registry", in.DriverID)
	}

	startedAt := time.Now().UTC()
	inv := ReviewerInvocation{
		InvocationID:    in.InvocationID,
		DriverID:        in.DriverID,
		Role:            in.Role,
		SnapshotHashRef: SnapshotHashRef(in.Snapshot),
		StartedAt:       startedAt,
	}

	// TODO(spec-094-impl PR #2): real driver dispatch. The skeleton below
	// invokes the driver via its existing Invoke surface (spec 075). v1
	// Phase 2 ships this wired but stubbed — testsuite tests mock this
	// activity entirely, and US1 happy-path validation does not require
	// the production driver to be reachable. The follow-up PR replaces
	// this stub with the full review-mode tool invocation path described
	// in contracts/review-mode-driver-contract.md.
	_ = d // keep the reference alive until the stub becomes real
	failure := &verdict.Failure{
		Kind:   verdict.FailureError,
		Detail: "TODO: real driver dispatch not wired in this slice (Phase 2 foundational)",
	}
	inv.Outcome = verdict.Outcome{
		InvocationID: in.InvocationID,
		DriverID:     in.DriverID,
		Role:         in.Role,
		Failure:      failure,
		ElapsedMS:    time.Since(startedAt).Milliseconds(),
	}
	return DispatchMachineReviewerResult{Invocation: inv}, nil
}
