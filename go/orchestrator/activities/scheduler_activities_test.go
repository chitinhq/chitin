package activities

import (
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"
)

// fakeWorker captures every RegisterActivityWithOptions call so the
// test can assert on names registered. Mirrors the pattern in
// activities/review/register_test.go — only the registration method is
// exercised, so embedding the worker.Worker interface and overriding
// that one method is enough.
type fakeWorker struct {
	worker.Worker
	registered []string
}

func (f *fakeWorker) RegisterActivityWithOptions(_ interface{}, opts activity.RegisterOptions) {
	f.registered = append(f.registered, opts.Name)
}

// TestRegisterSchedulerActivities_RegistersPostLintViolations pins the
// spec 115 US2 registration so a future scheduler-activities edit that
// drops the wiring fails this test instead of failing silently at
// workflow runtime with "activity PostLintViolations is not registered".
func TestRegisterSchedulerActivities_RegistersPostLintViolations(t *testing.T) {
	fw := &fakeWorker{}
	// Nil Registry / Worktrees / Telemetry / Notifier are tolerated by
	// the activity constructors (they store the pointer; nothing here
	// dereferences). The InvokeDriver loop is gated by Registry != nil.
	RegisterSchedulerActivities(fw, SchedulerActivityDeps{})

	want := NewPostLintViolations().ActivityName()
	for _, name := range fw.registered {
		if name == want {
			return
		}
	}
	t.Errorf("RegisterSchedulerActivities did not register %q; registered=%v", want, fw.registered)
}
