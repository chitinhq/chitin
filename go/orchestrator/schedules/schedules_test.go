package schedules

import (
	"errors"
	"testing"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/temporal"
)

// Spec 081 US2 tests for the Schedule-backed cron migration pattern. T009
// migrates exactly one cron — swarm-audit — so the Registry holds exactly one
// JobSpec, with the daily cadence of the retired swarm-audit.timer.

// TestRegistry_HasSwarmAudit proves Registry() returns the swarm-audit spec
// with the cadence the retired systemd timer fired on:
// OnCalendar=*-*-* 08:00:00 America/Detroit → cron "0 8 * * *" in
// America/Detroit (spec 081 FR-005 — same cadence as the retired timer).
func TestRegistry_HasSwarmAudit(t *testing.T) {
	reg := Registry()
	if len(reg) != 1 {
		t.Fatalf("Registry() returned %d jobs, want exactly 1 (T009 migrates swarm-audit only); got %+v", len(reg), reg)
	}

	job := reg[0]
	if job.Name != "swarm-audit" {
		t.Errorf("job Name = %q, want %q", job.Name, "swarm-audit")
	}
	if job.Cron != "0 8 * * *" {
		t.Errorf("job Cron = %q, want %q (daily at 08:00)", job.Cron, "0 8 * * *")
	}
	if job.TimeZone != "America/Detroit" {
		t.Errorf("job TimeZone = %q, want %q", job.TimeZone, "America/Detroit")
	}
	if job.Command == "" {
		t.Error("job Command is empty — the activity has no script to run")
	}
	if job.Description == "" {
		t.Error("job Description is empty — the Schedule note would be blank")
	}
}

// TestJobSpec_ScheduleIDIsStable proves ScheduleID is a pure function of the
// job Name — the property that makes EnsureSchedules idempotent: a second
// Create with the same ID collides and is a no-op, never a duplicate.
func TestJobSpec_ScheduleIDIsStable(t *testing.T) {
	job := JobSpec{Name: "swarm-audit"}
	want := "chitin-job-swarm-audit"
	if got := job.ScheduleID(); got != want {
		t.Errorf("ScheduleID() = %q, want %q", got, want)
	}
	// Pure function — same input, same output across calls.
	if job.ScheduleID() != job.ScheduleID() {
		t.Error("ScheduleID() is not deterministic")
	}
}

// TestRegistry_ReturnsFreshSlice proves Registry() never hands out a shared
// mutable global — a caller mutating its result must not corrupt the next
// caller's view of the canonical inventory.
func TestRegistry_ReturnsFreshSlice(t *testing.T) {
	first := Registry()
	first[0].Name = "mutated"
	second := Registry()
	if second[0].Name != "swarm-audit" {
		t.Errorf("Registry() returned a shared slice — mutation leaked; got %q", second[0].Name)
	}
}

// TestIsAlreadyExists proves the idempotency predicate: the Temporal SDK's
// ErrScheduleAlreadyRunning sentinel and a serviceerror.AlreadyExists / a
// *serviceerror.WorkflowExecutionAlreadyStarted are all recognized as the
// already-registered case, while an unrelated error is not — so EnsureSchedules
// treats a re-registration as success but still propagates a real fault.
func TestIsAlreadyExists(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"schedule-already-running sentinel", temporal.ErrScheduleAlreadyRunning, true},
		{"serviceerror already-exists", serviceerror.NewAlreadyExists("dup"), true},
		{"serviceerror workflow already started", serviceerror.NewWorkflowExecutionAlreadyStarted("dup", "id", "run"), true},
		{"unrelated error", errors.New("connection refused"), false},
		{"nil error", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAlreadyExists(tc.err); got != tc.want {
				t.Errorf("isAlreadyExists(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsAlreadyExists_WrappedSentinel proves a sentinel wrapped with fmt.Errorf
// %w is still recognized — errors.Is unwraps it. EnsureSchedules wraps Create
// errors, so this is the realistic wrapping path.
func TestIsAlreadyExists_WrappedSentinel(t *testing.T) {
	wrapped := errors.Join(errors.New("ensuring schedule"), temporal.ErrScheduleAlreadyRunning)
	if !isAlreadyExists(wrapped) {
		t.Error("a sentinel joined into a wrapped error must still be recognized as already-exists")
	}
}
