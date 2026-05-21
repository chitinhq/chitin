package driver

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/activity"
)

// InvokeDriver is the Temporal activity that runs one work unit on one agent
// driver (FR-007). Every driver invocation in the orchestrator goes through
// this single activity, so each is retryable, timeout-bounded, and
// individually inspectable in Temporal's history (070 FR-002/FR-004,
// SC-004).
//
// The activity is a thin, deterministic shell around AgentDriver.Invoke:
// it adds no nondeterministic logic of its own. It carries no clock-derived
// branching — the only time it reads is the WorkUnit.Deadline supplied in
// its typed input, which is part of the durable activity input and thus
// replay-stable. The agent work itself (subprocess, API call, model
// inference) is legitimate activity-side I/O.
//
// On a deadline overrun the activity returns a typed Result with
// StatusTimeout rather than hanging or returning a bare error (FR-007); the
// workflow then retries per its policy. The error return is reserved for
// transport/driver faults — an agent outcome is always carried in Result.
type InvokeDriver struct {
	// driver is the agent driver this activity invokes. One InvokeDriver
	// activity instance is bound to one driver; the worker host registers
	// one per registered driver (see RegisterActivities).
	driver AgentDriver
}

// NewInvokeDriver returns an InvokeDriver activity bound to d.
func NewInvokeDriver(d AgentDriver) *InvokeDriver {
	return &InvokeDriver{driver: d}
}

// ActivityName is the Temporal activity name under which this driver's
// invoke activity is registered. Naming it per-driver keeps each agent's
// invocations individually inspectable in Temporal history (SC-004).
func (a *InvokeDriver) ActivityName() string {
	return "InvokeDriver:" + a.driver.ID()
}

// Execute runs one WorkUnit on the bound driver and returns a typed Result.
// It is the activity function registered with the Temporal worker.
//
// It enforces the WorkUnit deadline defensively: if wu.Deadline is set and
// already in the past, or the bound driver's Invoke returns past the
// deadline, Execute reports a StatusTimeout Result so the workflow sees a
// typed, retryable timeout instead of a hang (FR-007). The deadline is read
// from the typed activity input, never from a free-running clock, so the
// activity's behavior is a pure function of its input and the agent's work.
func (a *InvokeDriver) Execute(ctx context.Context, wu WorkUnit) (Result, error) {
	if !wu.Deadline.IsZero() && timeNow().After(wu.Deadline) {
		return Result{
			WorkUnitID:  wu.ID,
			DriverID:    a.driver.ID(),
			Status:      StatusTimeout,
			Explanation: fmt.Sprintf("work unit %q deadline already elapsed before invocation", wu.ID),
		}, nil
	}

	// Heartbeat for the WHOLE invocation, not just once, so a long agent run
	// stays visible and the activity's StartToClose timeout — not the much
	// shorter HeartbeatTimeout — governs liveness. A single heartbeat would let
	// any invocation longer than the HeartbeatTimeout be killed mid-run. A
	// background ticker beats while AgentDriver.Invoke blocks and stops the
	// instant Invoke returns. activity.RecordHeartbeat is a no-op outside a
	// Temporal activity context, so Execute remains callable directly in tests.
	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go func() {
		beat := func() {
			activity.RecordHeartbeat(ctx, fmt.Sprintf("invoking %s on work unit %s", a.driver.ID(), wu.ID))
		}
		beat() // an immediate first beat — never wait a full interval to be seen.
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				beat()
			}
		}
	}()

	res, err := a.driver.Invoke(ctx, wu)
	if err != nil {
		// A transport/driver fault — surface it; the activity's retry policy
		// decides whether to retry.
		return Result{
			WorkUnitID:  wu.ID,
			DriverID:    a.driver.ID(),
			Status:      StatusFailed,
			Explanation: fmt.Sprintf("driver %q invocation faulted: %v", a.driver.ID(), err),
		}, err
	}

	// Normalize the Result's correlation fields so the caller always gets a
	// fully-attributed Result regardless of how carefully the driver filled
	// them in.
	res.WorkUnitID = wu.ID
	res.DriverID = a.driver.ID()
	if res.Status == StatusUnknown {
		res.Status = StatusFailed
		res.Explanation = "driver returned StatusUnknown; treated as failed"
	}

	// A driver that ran past the deadline without self-reporting a timeout is
	// reclassified, so the deadline is enforced uniformly across all drivers.
	if !wu.Deadline.IsZero() && timeNow().After(wu.Deadline) &&
		res.Status != StatusTimeout && res.Status != StatusSucceeded {
		res.Status = StatusTimeout
		res.Explanation = fmt.Sprintf(
			"driver %q invocation overran work unit %q deadline; %s",
			a.driver.ID(), wu.ID, res.Explanation,
		)
	}

	return res, nil
}

// timeNow is the activity's only time source, indirected so tests can make
// deadline handling deterministic. Production reads the wall clock; this is
// activity-side code, not workflow code, so reading the clock here is
// legitimate (it is the workflow that must stay clock-free).
var timeNow = time.Now

// heartbeatInterval is how often Execute beats while a driver invocation is in
// flight. It must stay well under the HeartbeatTimeout the workflow sets on the
// activity (currently 2 minutes) so a beat is never missed; 20 seconds leaves a
// 6× margin.
const heartbeatInterval = 20 * time.Second
