package workflows

import (
	"bytes"
	"context"
	"testing"

	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/internal/blob"
)

// Spec 076 US3 tests for WorkUnitWorkflow: the same work-unit workflow runs
// over any target repository on any base ref, and each work unit's worktree
// is created from the correct repo at the correct ref (FR-013, SC-006). Every
// side effect — worktree create/teardown, the driver invocation — is mocked
// so the test is hermetic and asserts on the inputs the workflow passed.

// runWorkUnit executes WorkUnitWorkflow once over the given node, capturing
// the CreateWorktree input the workflow passed and confirming teardown ran.
// It returns the captured create-worktree input and the work-unit result.
func runWorkUnit(t *testing.T, node dag.Node, driverID string) (activities.CreateWorktreeInput, WorkUnitResult, bool) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var gotCreate activities.CreateWorktreeInput
	const fakeWorktreePath = "/worktrees/wu-xyz"
	tornDown := false

	// Mock CreateWorktree: capture the repo/ref the workflow asked for.
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.CreateWorktreeInput) (activities.CreateWorktreeResult, error) {
			gotCreate = in
			return activities.CreateWorktreeResult{Path: fakeWorktreePath}, nil
		},
		activityOpts("CreateWorktree"),
	)
	// Mock TeardownWorktree: confirm the workflow tears the worktree down.
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.TeardownWorktreeInput) error {
			if in.Path == fakeWorktreePath {
				tornDown = true
			}
			return nil
		},
		activityOpts("TeardownWorktree"),
	)
	// Mock the per-driver InvokeDriver activity: succeed, echoing the work
	// unit so the workflow's result is well-formed.
	env.RegisterActivityWithOptions(
		func(_ context.Context, wu driver.WorkUnit) (driver.Result, error) {
			return driver.Result{
				WorkUnitID: wu.ID,
				DriverID:   driverID,
				Status:     driver.StatusSucceeded,
			}, nil
		},
		activityOpts("InvokeDriver:"+driverID),
	)

	env.ExecuteWorkflow(WorkUnitWorkflow, WorkUnitInput{
		Node:           node,
		DriverID:       driverID,
		SchedulerRunID: "wu-test-run",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatalf("work-unit workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("work-unit workflow errored: %v", err)
	}
	var res WorkUnitResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decoding work-unit result: %v", err)
	}
	return gotCreate, res, tornDown
}

// TestWorkUnit_PassesDescriptionToDriver proves an agent node's Description
// reaches the driver as the work unit's Context — the instruction the driver
// builds the agent prompt from — and that an empty Description falls back to
// the spec/task ids so the agent is never invoked with no instruction.
func TestWorkUnit_PassesDescriptionToDriver(t *testing.T) {
	cases := []struct {
		name        string
		description string
		wantContext string
	}{
		{"description carried verbatim",
			"Implement the greeting constant in greeting.go",
			"Implement the greeting constant in greeting.go"},
		{"empty description falls back to ids", "", "spec 070 task T001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			const driverID = "driver-impl"
			var gotContext string

			env.RegisterActivityWithOptions(
				func(_ context.Context, _ activities.CreateWorktreeInput) (activities.CreateWorktreeResult, error) {
					return activities.CreateWorktreeResult{Path: "/worktrees/wu-desc"}, nil
				},
				activityOpts("CreateWorktree"),
			)
			env.RegisterActivityWithOptions(
				func(_ context.Context, _ activities.TeardownWorktreeInput) error { return nil },
				activityOpts("TeardownWorktree"),
			)
			env.RegisterActivityWithOptions(
				func(_ context.Context, wu driver.WorkUnit) (driver.Result, error) {
					gotContext = wu.Context
					return driver.Result{WorkUnitID: wu.ID, DriverID: driverID, Status: driver.StatusSucceeded}, nil
				},
				activityOpts("InvokeDriver:"+driverID),
			)

			node := dag.Node{
				ID: "n-desc", SpecRef: "070", TaskRef: "T001",
				Capability: "code.implement", Description: tc.description,
				TargetRepo: "/repos/x", BaseRef: "main", WorktreeRequired: true,
			}
			env.ExecuteWorkflow(WorkUnitWorkflow, WorkUnitInput{
				Node: node, DriverID: driverID, SchedulerRunID: "desc-run",
			})

			if !env.IsWorkflowCompleted() {
				t.Fatalf("work-unit workflow did not complete")
			}
			if err := env.GetWorkflowError(); err != nil {
				t.Fatalf("work-unit workflow errored: %v", err)
			}
			if gotContext != tc.wantContext {
				t.Errorf("driver received Context %q, want %q", gotContext, tc.wantContext)
			}
		})
	}
}

// TestWorkUnit_WorktreeFromNodeRepo proves FR-013 / US3 acceptance scenario 1:
// a work unit's worktree is created from the node's named target repo at its
// named base ref — never a hard-coded repo.
func TestWorkUnit_WorktreeFromNodeRepo(t *testing.T) {
	node := dag.Node{
		ID: "n1", SpecRef: "076", Capability: "code.implement",
		TargetRepo: "/repos/chitin", BaseRef: "main", WorktreeRequired: true,
	}
	got, res, tornDown := runWorkUnit(t, node, "driver-impl")

	if got.TargetRepo != node.TargetRepo {
		t.Errorf("CreateWorktree target repo = %q, want %q", got.TargetRepo, node.TargetRepo)
	}
	if got.BaseRef != node.BaseRef {
		t.Errorf("CreateWorktree base ref = %q, want %q", got.BaseRef, node.BaseRef)
	}
	if got.WorkUnitID != node.ID {
		t.Errorf("CreateWorktree work unit id = %q, want %q", got.WorkUnitID, node.ID)
	}
	if !res.Succeeded {
		t.Errorf("work unit result not successful: %+v", res)
	}
	if !tornDown {
		t.Error("work unit did not tear its worktree down")
	}
}

// TestWorkUnit_TwoRepoIsolation proves FR-013 / US3 acceptance scenario 2,
// SC-006: two work units targeting distinct repos each create a worktree from
// their OWN repo — no work unit observes another repo's checkout.
func TestWorkUnit_TwoRepoIsolation(t *testing.T) {
	chitinNode := dag.Node{
		ID: "chitin-task", SpecRef: "076", Capability: "code.implement",
		TargetRepo: "/repos/chitin", BaseRef: "main", WorktreeRequired: true,
	}
	readybenchNode := dag.Node{
		ID: "readybench-task", SpecRef: "076", Capability: "code.implement",
		TargetRepo: "/repos/readybench", BaseRef: "release/v2", WorktreeRequired: true,
	}

	gotChitin, _, _ := runWorkUnit(t, chitinNode, "driver-impl")
	gotReadyBench, _, _ := runWorkUnit(t, readybenchNode, "driver-impl")

	if gotChitin.TargetRepo != "/repos/chitin" {
		t.Errorf("chitin work unit created a worktree from %q, want /repos/chitin",
			gotChitin.TargetRepo)
	}
	if gotReadyBench.TargetRepo != "/repos/readybench" {
		t.Errorf("readybench work unit created a worktree from %q, want /repos/readybench",
			gotReadyBench.TargetRepo)
	}
	// Distinct repos AND distinct base refs — no cross-contamination.
	if gotChitin.TargetRepo == gotReadyBench.TargetRepo {
		t.Error("two work units targeting distinct repos shared a target repo")
	}
	if gotChitin.BaseRef != "main" || gotReadyBench.BaseRef != "release/v2" {
		t.Errorf("base refs not isolated: chitin=%q readybench=%q",
			gotChitin.BaseRef, gotReadyBench.BaseRef)
	}
}

// TestWorkUnit_DeterministicNodeRunsStep proves spec 076 FR-017 / User Story 4
// acceptance scenario 1 at the WorkUnitWorkflow level: a NodeKindDeterministic
// node's work unit creates a worktree, runs its mechanical command via the
// RunDeterministicStep activity in that worktree — NOT a driver — and tears
// the worktree down. The work unit carries no driver id.
// TestWorkUnit_EmitsSettledNotification proves WorkUnitWorkflow posts a
// work-unit-settled event to the human notification channel via the
// DiscordNotify activity (spec 080 US2).
func TestWorkUnit_EmitsSettledNotification(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var events []activities.NotificationEvent

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.CreateWorktreeInput) (activities.CreateWorktreeResult, error) {
			return activities.CreateWorktreeResult{Path: "/worktrees/wu-notify"}, nil
		},
		activityOpts("CreateWorktree"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.TeardownWorktreeInput) error { return nil },
		activityOpts("TeardownWorktree"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.DeterministicStepInput) (activities.DeterministicStepResult, error) {
			return activities.DeterministicStepResult{NodeID: in.NodeID, Succeeded: true}, nil
		},
		activityOpts("RunDeterministicStep"),
	)
	// Capture every notification the workflow emits.
	env.RegisterActivityWithOptions(
		func(_ context.Context, ev activities.NotificationEvent) error {
			events = append(events, ev)
			return nil
		},
		activityOpts("DiscordNotify"),
	)

	node := dag.Node{
		ID: "n-notify", SpecRef: "080", Kind: dag.NodeKindDeterministic,
		Command: "true", TargetRepo: "/repos/x", BaseRef: "main", WorktreeRequired: true,
	}
	env.ExecuteWorkflow(WorkUnitWorkflow, WorkUnitInput{Node: node, SchedulerRunID: "notify-run"})

	if !env.IsWorkflowCompleted() {
		t.Fatal("work-unit workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("work-unit workflow errored: %v", err)
	}

	var settled *activities.NotificationEvent
	for i := range events {
		if events[i].Kind == activities.NotifyWorkUnitSettled {
			settled = &events[i]
		}
	}
	if settled == nil {
		t.Fatalf("no work-unit-settled notification emitted; got %v", events)
	}
	if settled.RunID != "notify-run" || settled.NodeID != "n-notify" {
		t.Errorf("settled event = %+v, want run notify-run / node n-notify", *settled)
	}
}

func TestWorkUnit_DeterministicNodeRunsStep(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	const fakeWorktreePath = "/worktrees/wu-det"
	tornDown := false
	var gotStep activities.DeterministicStepInput

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.CreateWorktreeInput) (activities.CreateWorktreeResult, error) {
			return activities.CreateWorktreeResult{Path: fakeWorktreePath}, nil
		},
		activityOpts("CreateWorktree"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.TeardownWorktreeInput) error {
			if in.Path == fakeWorktreePath {
				tornDown = true
			}
			return nil
		},
		activityOpts("TeardownWorktree"),
	)
	// Mock RunDeterministicStep: capture the step the work unit asked to run.
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.DeterministicStepInput) (activities.DeterministicStepResult, error) {
			gotStep = in
			return activities.DeterministicStepResult{
				NodeID: in.NodeID, Succeeded: true, ExitCode: 0,
				Output: "formatted", Explanation: "gofmt ran clean",
			}, nil
		},
		activityOpts("RunDeterministicStep"),
	)

	node := dag.Node{
		ID: "fmt", SpecRef: "076", Kind: dag.NodeKindDeterministic,
		Command: "gofmt", Args: []string{"-w", "."},
		TargetRepo: "/repos/chitin", BaseRef: "main", WorktreeRequired: true,
	}
	// A deterministic node's work unit carries NO driver id.
	env.ExecuteWorkflow(WorkUnitWorkflow, WorkUnitInput{
		Node: node, SchedulerRunID: "wu-det-run",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("work-unit workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("deterministic work unit errored: %v", err)
	}
	var res WorkUnitResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decoding result: %v", err)
	}

	// The step ran the node's command in the fresh worktree.
	if gotStep.Command != "gofmt" {
		t.Errorf("RunDeterministicStep command = %q, want gofmt", gotStep.Command)
	}
	if len(gotStep.Args) != 2 || gotStep.Args[0] != "-w" || gotStep.Args[1] != "." {
		t.Errorf("RunDeterministicStep args = %v, want [-w .]", gotStep.Args)
	}
	if gotStep.WorktreePath != fakeWorktreePath {
		t.Errorf("RunDeterministicStep worktree = %q, want %q", gotStep.WorktreePath, fakeWorktreePath)
	}
	if gotStep.NodeID != "fmt" {
		t.Errorf("RunDeterministicStep node id = %q, want fmt", gotStep.NodeID)
	}
	if !res.Succeeded {
		t.Errorf("deterministic work unit result not successful: %+v", res)
	}
	if res.DriverID != "" {
		t.Errorf("deterministic work unit carried driver id %q, want empty", res.DriverID)
	}
	if !tornDown {
		t.Error("deterministic work unit did not tear its worktree down")
	}
}

// TestWorkUnit_DeterministicStepFailure proves a deterministic node whose
// mechanical step exits non-zero settles the work unit unsuccessful, and the
// worktree is still torn down (spec 076 FR-017 acceptance scenario 3).
func TestWorkUnit_DeterministicStepFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	const fakeWorktreePath = "/worktrees/wu-det-fail"
	tornDown := false

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.CreateWorktreeInput) (activities.CreateWorktreeResult, error) {
			return activities.CreateWorktreeResult{Path: fakeWorktreePath}, nil
		},
		activityOpts("CreateWorktree"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.TeardownWorktreeInput) error {
			if in.Path == fakeWorktreePath {
				tornDown = true
			}
			return nil
		},
		activityOpts("TeardownWorktree"),
	)
	// The mechanical step exits non-zero — a real failed step, not a fault.
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.DeterministicStepInput) (activities.DeterministicStepResult, error) {
			return activities.DeterministicStepResult{
				NodeID: in.NodeID, Succeeded: false, ExitCode: 1,
				Explanation: "go test: 2 failures",
			}, nil
		},
		activityOpts("RunDeterministicStep"),
	)

	env.ExecuteWorkflow(WorkUnitWorkflow, WorkUnitInput{
		Node: dag.Node{ID: "test", SpecRef: "076", Kind: dag.NodeKindDeterministic,
			Command: "go", Args: []string{"test", "./..."},
			TargetRepo: "/repos/chitin", BaseRef: "main", WorktreeRequired: true},
		SchedulerRunID: "wu-det-fail-run",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("work-unit workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a failed deterministic step must not fault the workflow: %v", err)
	}
	var res WorkUnitResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	if res.Succeeded {
		t.Error("work unit result must be unsuccessful when the deterministic step fails")
	}
	if !tornDown {
		t.Error("work unit must tear its worktree down even when the deterministic step fails")
	}
}

// TestWorkUnit_TeardownOnDriverFailure proves the worktree is torn down even
// when the driver invocation reports a failure — a failed work unit never
// leaks its worktree (spec 070 FR-013/14).
func TestWorkUnit_TeardownOnDriverFailure(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	const fakeWorktreePath = "/worktrees/wu-fail"
	tornDown := false

	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.CreateWorktreeInput) (activities.CreateWorktreeResult, error) {
			return activities.CreateWorktreeResult{Path: fakeWorktreePath}, nil
		},
		activityOpts("CreateWorktree"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.TeardownWorktreeInput) error {
			if in.Path == fakeWorktreePath {
				tornDown = true
			}
			return nil
		},
		activityOpts("TeardownWorktree"),
	)
	// The driver reports a failure outcome (not a transport fault).
	env.RegisterActivityWithOptions(
		func(_ context.Context, wu driver.WorkUnit) (driver.Result, error) {
			return driver.Result{
				WorkUnitID: wu.ID, DriverID: "driver-impl",
				Status: driver.StatusFailed, Explanation: "agent could not complete",
			}, nil
		},
		activityOpts("InvokeDriver:driver-impl"),
	)

	env.ExecuteWorkflow(WorkUnitWorkflow, WorkUnitInput{
		Node: dag.Node{ID: "n-fail", SpecRef: "076", Capability: "code.implement",
			TargetRepo: "/repos/chitin", BaseRef: "main", WorktreeRequired: true},
		DriverID:       "driver-impl",
		SchedulerRunID: "wu-fail-run",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("work-unit workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a driver-reported failure must not fault the workflow: %v", err)
	}
	var res WorkUnitResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	if res.Succeeded {
		t.Error("work unit result must be unsuccessful when the driver fails")
	}
	if !tornDown {
		t.Error("work unit must tear its worktree down even when the driver fails")
	}
}

func TestWorkUnitBlobOutputRefPayloadStaysSmall(t *testing.T) {
	store, err := blob.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("z"), 3*1024*1024)
	ref, err := store.Put(context.Background(), body)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	const driverID = "driver-impl"
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.CreateWorktreeInput) (activities.CreateWorktreeResult, error) {
			return activities.CreateWorktreeResult{Path: "/worktrees/blob"}, nil
		},
		activityOpts("CreateWorktree"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.TeardownWorktreeInput) error { return nil },
		activityOpts("TeardownWorktree"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, wu driver.WorkUnit) (driver.Result, error) {
			return driver.Result{
				WorkUnitID: wu.ID,
				DriverID:   driverID,
				Status:     driver.StatusFailed,
				OutputRef:  ref.String(),
			}, nil
		},
		activityOpts("InvokeDriver:"+driverID),
	)

	env.ExecuteWorkflow(WorkUnitWorkflow, WorkUnitInput{
		Node: dag.Node{
			ID: "n-blob", SpecRef: "121", Capability: "code.implement",
			TargetRepo: "/repos/chitin", BaseRef: "main", WorktreeRequired: true,
		},
		DriverID:       driverID,
		SchedulerRunID: "blob-run",
	})
	if !env.IsWorkflowCompleted() {
		t.Fatal("work-unit workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("work-unit workflow errored: %v", err)
	}
	var res WorkUnitResult
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	if res.OutputRef != ref.String() {
		t.Fatalf("OutputRef = %q, want %q", res.OutputRef, ref.String())
	}
	payloads, err := converter.GetDefaultDataConverter().ToPayloads(res)
	if err != nil {
		t.Fatalf("serialize result: %v", err)
	}
	var size int
	for _, p := range payloads.Payloads {
		size += len(p.Data)
		for k, v := range p.Metadata {
			size += len(k) + len(v)
		}
	}
	if size >= 4*1024 {
		t.Fatalf("serialized WorkUnitResult payload = %d bytes, want < 4096", size)
	}
}
