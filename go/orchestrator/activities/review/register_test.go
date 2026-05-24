package review

import (
	"context"
	"strings"
	"testing"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/worker"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// fakeWorker captures every RegisterActivityWithOptions call so the test
// can assert on names registered. It implements only the subset of
// worker.Worker that Register touches. The full worker.Worker is huge;
// embedding the interface and overriding the two methods we use keeps
// this small while still satisfying the type.
type fakeWorker struct {
	worker.Worker
	registered []string
}

func (f *fakeWorker) RegisterActivityWithOptions(_ interface{}, opts activity.RegisterOptions) {
	f.registered = append(f.registered, opts.Name)
}

// TestRegister_RegistersAllFourActivities confirms Register binds every
// dialectic review activity by its declared ActivityName const. If any
// name drifts (e.g., someone renames CapturePRSnapshotActivityName without
// updating the workflow's string-name dispatch), the PRReviewWorkflow's
// ExecuteActivity calls would fail at workflow runtime; this test guards
// against that by checking the registered names match the consts.
func TestRegister_RegistersAllFourActivities(t *testing.T) {
	reg := driver.NewRegistry()
	fw := &fakeWorker{}
	if err := Register(fw, RegisterDeps{Registry: reg}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	want := map[string]bool{
		SelectReviewersActivityName:         true,
		CapturePRSnapshotActivityName:       true,
		DispatchMachineReviewerActivityName: true,
		EmitReviewTelemetryActivityName:     true,
	}
	if len(fw.registered) != len(want) {
		t.Fatalf("registered %d activities (%v), want %d (%v)",
			len(fw.registered), fw.registered, len(want), keys(want))
	}
	for _, name := range fw.registered {
		if !want[name] {
			t.Errorf("unexpected activity registered: %q", name)
		}
		delete(want, name)
	}
	if len(want) > 0 {
		t.Errorf("missing activities: %v", keys(want))
	}
}

// TestRegister_RequiresRegistry confirms a nil driver registry is rejected
// at Register time rather than letting the worker boot into a state where
// every dialectic gate halts with a SelectReviewers config-fault error.
func TestRegister_RequiresRegistry(t *testing.T) {
	fw := &fakeWorker{}
	err := Register(fw, RegisterDeps{Registry: nil})
	if err == nil {
		t.Fatal("Register with nil Registry returned nil error; expected rejection")
	}
	if !strings.Contains(err.Error(), "Registry") {
		t.Errorf("error %q does not name the missing dep", err.Error())
	}
	if len(fw.registered) != 0 {
		t.Errorf("partial registration on failure: %v", fw.registered)
	}
}

// TestRegister_NilGhUsesDefault confirms a nil GhRunner in RegisterDeps
// is not an error — CapturePRSnapshot's own constructor falls back to the
// default real `gh` runner. Production main.go relies on this.
func TestRegister_NilGhUsesDefault(t *testing.T) {
	reg := driver.NewRegistry()
	fw := &fakeWorker{}
	if err := Register(fw, RegisterDeps{Registry: reg, Gh: nil}); err != nil {
		t.Fatalf("Register with nil Gh: %v", err)
	}
	// All four still registered.
	if len(fw.registered) != 4 {
		t.Errorf("registered %d, want 4", len(fw.registered))
	}
}

// keys returns the keys of a string-keyed map for error messages.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Compile-time check: SelectReviewers, CapturePRSnapshot, and
// DispatchMachineReviewer all expose ActivityName() that returns the
// declared const. If a future refactor accidentally returns something
// else, this test fails fast.
func TestActivityNamesMatchConsts(t *testing.T) {
	cases := []struct {
		name       string
		got        string
		want       string
	}{
		{"SelectReviewers", NewSelectReviewers(driver.NewRegistry()).ActivityName(), SelectReviewersActivityName},
		{"CapturePRSnapshot", NewCapturePRSnapshot(nil).ActivityName(), CapturePRSnapshotActivityName},
		{"DispatchMachineReviewer", NewDispatchMachineReviewer(driver.NewRegistry()).ActivityName(), DispatchMachineReviewerActivityName},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("ActivityName() = %q, want %q", c.got, c.want)
			}
		})
	}
}

// _ ensures context import is referenced (we use context.Background in
// some flows). Currently no Register-level test needs it; keep this
// guard so a future addition does not break the import block.
var _ = context.Background
