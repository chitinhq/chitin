package workflows

import (
	"context"
	"testing"

	"go.temporal.io/sdk/testsuite"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
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
