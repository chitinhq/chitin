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

	// Spec 104 PR-A — real driver dispatch.
	//
	// Build a WorkUnit shaped per contracts/review-mode-driver-contract.md:
	// snapshot + policy class marshalled as JSON into WorkUnit.Context.
	// SpecID="094" signals review-mode to the driver (its prompt template
	// recognizes the spec and emits a StructuredVerdict on success). The
	// driver's declared ReviewMode.MaxBytesIn caps the marshalled context
	// size; nil ReviewMode means no driver-declared cap (we pass 0 → no
	// truncation).
	card := d.Card()
	maxBytesIn := 0
	if card.ReviewMode != nil {
		maxBytesIn = card.ReviewMode.MaxBytesIn
	}
	contextJSON, err := marshalReviewContext(in.Snapshot, in.PolicyClass, maxBytesIn)
	if err != nil {
		// Marshalling failure is a configuration fault — surface it as
		// an activity error so the workflow halts the gate with a clear
		// reason rather than producing a malformed dispatch.
		return DispatchMachineReviewerResult{},
			fmt.Errorf("activities/review: marshalReviewContext for %s: %w", in.DriverID, err)
	}

	// Deadline: respect the activity context's deadline if any.
	var deadline time.Time
	if dl, ok := ctx.Deadline(); ok {
		deadline = dl
	}

	wu := driver.WorkUnit{
		ID:       in.InvocationID,
		SpecID:   "094",
		TaskID:   "review",
		Context:  string(contextJSON),
		Deadline: deadline,
	}
	res, invokeErr := d.Invoke(ctx, wu)

	inv.Outcome = translateInvokeResult(in.InvocationID, in.DriverID, in.Role, startedAt, res, invokeErr, ctx.Err())
	return DispatchMachineReviewerResult{Invocation: inv}, nil
}

// translateInvokeResult maps the (Result, invokeErr, ctxErr) triple
// returned by Driver.Invoke into a closed verdict.Outcome — either a
// parsed StructuredVerdict on success or one of four FailureKind values
// on the failure paths (spec 104 FR-010):
//
//   - StatusSucceeded + parseable verdict → Outcome.Verdict
//   - StatusSucceeded + bad JSON         → FailureMalformedJSON
//   - StatusSucceeded + bad shape        → FailureMalformedShape
//   - StatusTimedOut OR ctx.DeadlineExceeded → FailureTimeout
//   - StatusFailed / other / invokeErr   → FailureError
//
// All driver-side problems become Outcome.Failure (never an activity-level
// Go error); only marshalling / registry config faults bubble back to the
// workflow as errors.
func translateInvokeResult(
	invocationID, driverID string,
	role verdict.Role,
	startedAt time.Time,
	res driver.Result,
	invokeErr error,
	ctxErr error,
) verdict.Outcome {
	out := verdict.Outcome{
		InvocationID: invocationID,
		DriverID:     driverID,
		Role:         role,
		ElapsedMS:    time.Since(startedAt).Milliseconds(),
	}

	// Timeout wins first: ctx.DeadlineExceeded OR explicit StatusTimedOut.
	// A deadline overrun is the typed failure the gate cares about, not
	// the raw Go error.
	if ctxErr == context.DeadlineExceeded || res.Status == driver.StatusTimeout {
		out.Failure = &verdict.Failure{
			Kind:   verdict.FailureTimeout,
			Detail: nonEmpty(res.Explanation, "deadline exceeded"),
		}
		return out
	}

	// Other Invoke-level errors → FailureError. Covers kernel-gate
	// rejection, runtime crash, network drop, etc.
	if invokeErr != nil {
		out.Failure = &verdict.Failure{
			Kind:   verdict.FailureError,
			Detail: invokeErr.Error(),
		}
		return out
	}

	switch res.Status {
	case driver.StatusSucceeded:
		v, perr := verdict.ParseStructured([]byte(res.Explanation))
		if perr != nil {
			pe, _ := perr.(*verdict.ParseError)
			kind := verdict.FailureMalformedShape
			if pe != nil && pe.Kind == "malformed_json" {
				kind = verdict.FailureMalformedJSON
			}
			out.Failure = &verdict.Failure{Kind: kind, Detail: perr.Error()}
			return out
		}
		out.Verdict = &v
		return out

	case driver.StatusTimeout:
		out.Failure = &verdict.Failure{
			Kind:   verdict.FailureTimeout,
			Detail: nonEmpty(res.Explanation, "deadline exceeded"),
		}
		return out

	default:
		out.Failure = &verdict.Failure{
			Kind:   verdict.FailureError,
			Detail: nonEmpty(res.Explanation, fmt.Sprintf("driver returned status %v", res.Status)),
		}
		return out
	}
}

// nonEmpty returns s if non-empty, else fallback. Used by failure-detail
// composition so the reviewer driver always sees a meaningful reason.
func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
