package activities

import (
	"context"
	"errors"
	"fmt"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// SelectDriverInput is the typed input to the SelectDriver activity: the work
// unit being routed and the capability tag a driver must declare to run it.
type SelectDriverInput struct {
	// NodeID is the DAG node the selection is for — carried through for
	// correlation in the activity's telemetry and the returned reason.
	NodeID string `json:"node_id"`
	// Capability is the capability tag the runnable node requires; the
	// registry routes on it (spec 076 FR-007).
	Capability string `json:"capability"`
}

// SelectDriverResult is the typed output of the SelectDriver activity — the
// outcome of capability routing for one node.
type SelectDriverResult struct {
	// DriverID is the id of the chosen driver, empty when Unroutable is true.
	DriverID string `json:"driver_id"`
	// Reason is the human-readable selection reason for the audit record
	// (spec 075 FR-005) — or, when Unroutable, why no driver matched.
	Reason string `json:"reason"`
	// Unroutable is true when no registered, ready driver satisfies the
	// required capability (spec 076 FR-010); the scheduler then marks the
	// node blocked-unroutable.
	Unroutable bool `json:"unroutable"`
	// MissingCapability echoes the capability that could not be routed when
	// Unroutable is true; it is empty otherwise.
	MissingCapability string `json:"missing_capability"`
}

// DriverSelector is the SelectDriver activity (spec 076 FR-007). Driver
// selection is a SIDE EFFECT — the registry's Select calls each candidate
// driver's Ready probe, which is live I/O — so it MUST run in an activity,
// never in workflow code. The activity is bound to the orchestrator's driver
// registry at worker-host startup.
//
// Selection is deterministic given a fixed registry state (spec 075 FR-005,
// spec 076 FR-005): the same capability always yields the same driver and
// reason. Determinism across a workflow REPLAY is preserved by Temporal —
// the activity runs once and its result is recorded in history; a replay
// reads the recorded result rather than re-probing.
type DriverSelector struct {
	// registry is the orchestrator's driver registry, loaded at startup.
	registry *driver.Registry
}

// NewDriverSelector returns a SelectDriver activity bound to the given
// registry. The registry must be fully populated before the worker host
// starts polling — Register is not concurrency-safe with Select.
func NewDriverSelector(registry *driver.Registry) *DriverSelector {
	return &DriverSelector{registry: registry}
}

// ActivityName is the stable Temporal activity name SelectDriver registers
// under and the scheduler workflow dispatches to.
func (a *DriverSelector) ActivityName() string { return "SelectDriver" }

// Execute routes one runnable node to a capability-matched driver. It is the
// activity function registered with the Temporal worker.
//
// A node whose capability no ready driver satisfies yields a result with
// Unroutable=true and the missing capability named — never an error — so the
// scheduler can mark exactly that node blocked-unroutable while the rest of
// the frontier proceeds (spec 076 FR-010, acceptance scenario 4). The error
// return is reserved for a misconfigured activity (nil registry).
func (a *DriverSelector) Execute(_ context.Context, in SelectDriverInput) (SelectDriverResult, error) {
	if a.registry == nil {
		return SelectDriverResult{}, fmt.Errorf("activities: SelectDriver has no driver registry bound")
	}

	cap := driver.Capability(in.Capability)
	chosen, reason, err := a.registry.Select(context.Background(), cap)
	if err != nil {
		var unroutable *driver.BlockedUnroutableError
		if errors.As(err, &unroutable) {
			return SelectDriverResult{
				Unroutable:        true,
				MissingCapability: string(unroutable.Capability),
				Reason: fmt.Sprintf(
					"node %s blocked-unroutable: no registered, ready driver satisfies capability %q",
					in.NodeID, unroutable.Capability,
				),
			}, nil
		}
		// Any other error is a genuine activity fault — surface it so the
		// workflow's retry policy can act.
		return SelectDriverResult{}, fmt.Errorf("activities: SelectDriver for node %s: %w", in.NodeID, err)
	}

	return SelectDriverResult{
		DriverID: chosen.ID(),
		Reason:   fmt.Sprintf("node %s: %s", in.NodeID, reason),
	}, nil
}
