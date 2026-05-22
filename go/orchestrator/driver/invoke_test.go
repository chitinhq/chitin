package driver

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestInvokeDriver_HappyPath proves Execute returns the bound driver's Result
// with the work unit's correlation fields filled in. It also exercises the
// periodic-heartbeat goroutine: Execute starts it, the driver returns, and the
// deferred stop drains it — a panic from RecordHeartbeat outside an activity
// context would fail this test.
func TestInvokeDriver_HappyPath(t *testing.T) {
	d := &fakeDriver{
		id:     "drv",
		result: Result{Status: StatusSucceeded, OutputRef: "branch/x", Explanation: "done"},
	}
	res, err := NewInvokeDriver(d).Execute(context.Background(), WorkUnit{ID: "wu-1"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != StatusSucceeded {
		t.Errorf("status = %s, want succeeded", res.Status)
	}
	if res.WorkUnitID != "wu-1" || res.DriverID != "drv" {
		t.Errorf("correlation = (%q,%q), want (wu-1,drv)", res.WorkUnitID, res.DriverID)
	}
}

// TestInvokeDriver_DeadlineAlreadyElapsed proves Execute reports StatusTimeout
// without invoking the driver when the work unit's deadline is already past —
// the "deadline already elapsed before invocation" explanation is produced
// only on that pre-invoke path.
func TestInvokeDriver_DeadlineAlreadyElapsed(t *testing.T) {
	d := &fakeDriver{id: "drv", result: Result{Status: StatusSucceeded}}
	res, err := NewInvokeDriver(d).Execute(context.Background(), WorkUnit{
		ID: "wu-late", Deadline: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != StatusTimeout {
		t.Errorf("status = %s, want timeout", res.Status)
	}
}

// TestInvokeDriver_DriverFault proves a driver transport fault surfaces as a
// StatusFailed Result and a returned error, so the activity's retry policy can
// act on it.
func TestInvokeDriver_DriverFault(t *testing.T) {
	d := &fakeDriver{id: "drv", err: errors.New("transport down")}
	res, err := NewInvokeDriver(d).Execute(context.Background(), WorkUnit{ID: "wu-fault"})
	if err == nil {
		t.Fatal("Execute returned nil error for a driver fault")
	}
	if res.Status != StatusFailed {
		t.Errorf("status = %s, want failed", res.Status)
	}
}

// TestInvokeDriver_StatusUnknownBecomesFailed proves Execute reclassifies a
// driver Result of StatusUnknown as StatusFailed, so the caller never sees an
// unattributed outcome.
func TestInvokeDriver_StatusUnknownBecomesFailed(t *testing.T) {
	d := &fakeDriver{id: "drv", result: Result{Status: StatusUnknown}}
	res, err := NewInvokeDriver(d).Execute(context.Background(), WorkUnit{ID: "wu-x"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Status != StatusFailed {
		t.Errorf("status = %s, want failed", res.Status)
	}
}
