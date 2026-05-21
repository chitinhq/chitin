package workflows

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// Spec 076 replay/determinism tests for SchedulerWorkflow (FR-005, SC-001):
// replaying a tick over the same DAG and node states must yield identical
// dispatch decisions. Each test mocks every side effect — driver selection,
// the per-node child work unit, board projection, telemetry — so the
// scheduler runs hermetically and its decisions are a pure function of the
// DAG. The TestWorkflowEnvironment drives a replay-equivalent execution and
// panics on any non-determinism; running the scheduler many times over and
// asserting identical output is the direct proof of SC-001.

// schedDeps captures the dispatch decisions one scheduler execution made — the
// observable output the determinism assertion compares.
type schedDeps struct {
	// dispatchOrder is the sequence of node ids dispatched as child work
	// units, in the order the scheduler started them.
	dispatchOrder []string
	// driverFor records which driver each dispatched node was routed to.
	driverFor map[string]string
	// finalStatus is every node's terminal status keyed by node id.
	finalStatus map[string]string
}

// linearDAG builds a fixed three-node chain a -> b -> c (c depends on b, b
// depends on a) plus one independent root d, all StatusPending. The fixed
// structure makes the predicted dispatch order knowable.
func linearDAG() ([]dag.Node, []dag.Edge) {
	nodes := []dag.Node{
		{ID: "a", SpecRef: "076", Capability: "code.implement", Priority: 10,
			TargetRepo: "/repo/chitin", BaseRef: "main", WorktreeRequired: true},
		{ID: "b", SpecRef: "076", Capability: "code.implement", Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main", WorktreeRequired: true},
		{ID: "c", SpecRef: "076", Capability: "code.review", Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main", WorktreeRequired: true},
		{ID: "d", SpecRef: "076", Capability: "code.implement", Priority: 1,
			TargetRepo: "/repo/chitin", BaseRef: "main", WorktreeRequired: true},
	}
	edges := []dag.Edge{
		{From: "b", To: "a"}, // b depends on a
		{From: "c", To: "b"}, // c depends on b
	}
	return nodes, edges
}

// runScheduler executes SchedulerWorkflow once in a fresh test environment
// over the given DAG, mocking every side effect, and returns the dispatch
// decisions it made. unroutable names capabilities for which SelectDriver
// reports blocked-unroutable; failNodes names work units whose child returns
// a failure.
func runScheduler(
	t *testing.T,
	nodes []dag.Node, edges []dag.Edge,
	unroutable map[string]bool, failNodes map[string]bool,
) schedDeps {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	deps := schedDeps{
		driverFor:   map[string]string{},
		finalStatus: map[string]string{},
	}

	// Mock SelectDriver: deterministic routing by capability. A capability in
	// `unroutable` yields a blocked-unroutable result; everything else routes
	// to a fixed per-capability driver id.
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.SelectDriverInput) (activities.SelectDriverResult, error) {
			if unroutable[in.Capability] {
				return activities.SelectDriverResult{
					Unroutable:        true,
					MissingCapability: in.Capability,
					Reason:            "no driver for " + in.Capability,
				}, nil
			}
			return activities.SelectDriverResult{
				DriverID: "driver-" + in.Capability,
				Reason:   "selected driver-" + in.Capability,
			}, nil
		},
		activityOpts("SelectDriver"),
	)

	// Mock the per-node child work unit: it returns a deterministic
	// success/failure. OnWorkflow intercepts the child so no worktree or
	// driver activity runs. The mock's first parameter is workflow.Context —
	// a child workflow, not a plain activity. Dispatch ORDER is NOT recorded
	// here: child workflow bodies run cooperatively in the test env, so the
	// order they execute is not the order the scheduler dispatched them. The
	// authoritative dispatch order is the scheduler's own per-tick telemetry,
	// captured from the EmitTickTelemetry mock below.
	env.OnWorkflow(WorkUnitWorkflow, mock.Anything, mock.Anything).Return(
		func(_ workflow.Context, in WorkUnitInput) (WorkUnitResult, error) {
			succeeded := !failNodes[in.Node.ID]
			status := "succeeded"
			if !succeeded {
				status = "failed"
			}
			return WorkUnitResult{
				NodeID:    in.Node.ID,
				DriverID:  in.DriverID,
				Succeeded: succeeded,
				Status:    status,
			}, nil
		})

	// Mock the write-only side effects — board projection and telemetry. The
	// telemetry mock is the authoritative record of the scheduler's dispatch
	// decisions: tickRec.Dispatched is appended in the scheduler's own
	// deterministic frontier order, on the tick the dispatch happened.
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.BoardProjectionInput) error { return nil },
		activityOpts("ProjectToBoard"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, rec activities.TickRecord) error {
			for _, d := range rec.Dispatched {
				deps.dispatchOrder = append(deps.dispatchOrder, d.NodeID)
				deps.driverFor[d.NodeID] = d.DriverID
			}
			return nil
		},
		activityOpts("EmitTickTelemetry"),
	)

	env.ExecuteWorkflow(SchedulerWorkflow, SchedulerInput{
		RunID: "test-run",
		Nodes: nodes,
		Edges: edges,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatalf("scheduler workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("scheduler workflow errored: %v", err)
	}

	var status SchedulerStatus
	if err := env.GetWorkflowResult(&status); err != nil {
		t.Fatalf("decoding scheduler result: %v", err)
	}
	deps.finalStatus = status.NodeStatus
	return deps
}

// activityOpts is the registration option naming an activity by its string
// name — the name the scheduler workflow dispatches to.
func activityOpts(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}

// TestScheduler_DeterministicDispatch proves FR-005 / SC-001: 100 runs of the
// scheduler over the same DAG produce the identical dispatch order, the
// identical driver routing, and the identical final node statuses.
func TestScheduler_DeterministicDispatch(t *testing.T) {
	nodes, edges := linearDAG()

	first := runScheduler(t, nodes, edges, nil, nil)

	// The predicted order: d (independent) and a (root) are runnable on tick
	// 1; the frontier orders them priority-desc then id-asc — a(10) before
	// d(1). b becomes runnable after a, c after b.
	wantOrder := []string{"a", "d", "b", "c"}
	if !equalSeq(first.dispatchOrder, wantOrder) {
		t.Fatalf("dispatch order = %v, want %v", first.dispatchOrder, wantOrder)
	}

	for i := 0; i < 100; i++ {
		got := runScheduler(t, nodes, edges, nil, nil)
		if !equalSeq(got.dispatchOrder, first.dispatchOrder) {
			t.Fatalf("run %d dispatch order drifted: %v != %v",
				i, got.dispatchOrder, first.dispatchOrder)
		}
		if !equalMap(got.driverFor, first.driverFor) {
			t.Fatalf("run %d driver routing drifted: %v != %v",
				i, got.driverFor, first.driverFor)
		}
		if !equalMap(got.finalStatus, first.finalStatus) {
			t.Fatalf("run %d final statuses drifted: %v != %v",
				i, got.finalStatus, first.finalStatus)
		}
	}

	// Every node reached done — the happy path completes the whole DAG.
	for _, id := range []string{"a", "b", "c", "d"} {
		if first.finalStatus[id] != "done" {
			t.Errorf("node %s final status = %q, want done", id, first.finalStatus[id])
		}
	}
}

// TestScheduler_BlockedUnroutable proves FR-010 / acceptance scenario 4: a
// node whose capability no driver satisfies is marked blocked-unroutable, and
// the rest of the frontier still proceeds.
func TestScheduler_BlockedUnroutable(t *testing.T) {
	nodes, edges := linearDAG()
	// No driver satisfies code.review — node c is the only review node.
	got := runScheduler(t, nodes, edges, map[string]bool{"code.review": true}, nil)

	if got.finalStatus["c"] != "blocked-unroutable" {
		t.Errorf("node c final status = %q, want blocked-unroutable", got.finalStatus["c"])
	}
	// a, b, d are code.implement — they still run and complete.
	for _, id := range []string{"a", "b", "d"} {
		if got.finalStatus[id] != "done" {
			t.Errorf("node %s final status = %q, want done — the rest of the frontier must proceed",
				id, got.finalStatus[id])
		}
	}
	// c was never dispatched.
	for _, id := range got.dispatchOrder {
		if id == "c" {
			t.Errorf("blocked-unroutable node c must not be dispatched; order=%v", got.dispatchOrder)
		}
	}
}

// TestScheduler_DependencyFailurePropagates proves FR-011 / the "dependency
// permanently fails" edge case: when a node fails, every transitive dependent
// is marked blocked-dependency-failed and never runs.
func TestScheduler_DependencyFailurePropagates(t *testing.T) {
	nodes, edges := linearDAG()
	// Node a fails — b depends on a, c depends on b, so both are blocked.
	got := runScheduler(t, nodes, edges, nil, map[string]bool{"a": true})

	if got.finalStatus["a"] != "failed" {
		t.Errorf("node a final status = %q, want failed", got.finalStatus["a"])
	}
	for _, id := range []string{"b", "c"} {
		if got.finalStatus[id] != "blocked-dependency-failed" {
			t.Errorf("node %s final status = %q, want blocked-dependency-failed",
				id, got.finalStatus[id])
		}
	}
	// d is independent of a — it still completes.
	if got.finalStatus["d"] != "done" {
		t.Errorf("node d final status = %q, want done — d is independent of the failure",
			got.finalStatus["d"])
	}
	// Neither b nor c was ever dispatched.
	for _, id := range got.dispatchOrder {
		if id == "b" || id == "c" {
			t.Errorf("blocked node %s must not be dispatched; order=%v", id, got.dispatchOrder)
		}
	}
}

// TestScheduler_ExactlyOnceDispatch proves FR-009 / SC-004: each node is
// dispatched at most once — the dispatch order contains no duplicates.
func TestScheduler_ExactlyOnceDispatch(t *testing.T) {
	nodes, edges := linearDAG()
	got := runScheduler(t, nodes, edges, nil, nil)

	seen := map[string]int{}
	for _, id := range got.dispatchOrder {
		seen[id]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("node %s dispatched %d times, want exactly 1 (exactly-once)", id, count)
		}
	}
}

// TestScheduler_RejectsCyclicDAG proves FR-002 / SC-003: a scheduler handed a
// cyclic DAG refuses to run.
func TestScheduler_RejectsCyclicDAG(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// a -> b -> a is a cycle.
	nodes := []dag.Node{
		{ID: "a", Capability: "code.implement"},
		{ID: "b", Capability: "code.implement"},
	}
	edges := []dag.Edge{{From: "a", To: "b"}, {From: "b", To: "a"}}

	env.ExecuteWorkflow(SchedulerWorkflow, SchedulerInput{
		RunID: "cyclic", Nodes: nodes, Edges: edges,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err == nil {
		t.Fatal("scheduler must reject a cyclic DAG, got nil error")
	}
}

// TestScheduler_AppendJoinsGraph proves FR-012 / US2 acceptance scenario 1:
// an append signal splices a new node into the running DAG, and the appended
// node becomes runnable only after its declared dependency completes.
func TestScheduler_AppendJoinsGraph(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Start with a single node `a`. The append adds `e` depending on `a`.
	nodes := []dag.Node{
		{ID: "a", SpecRef: "076", Capability: "code.implement", Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main"},
	}

	dispatched := map[string]bool{}

	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.SelectDriverInput) (activities.SelectDriverResult, error) {
			return activities.SelectDriverResult{DriverID: "driver-" + in.Capability}, nil
		},
		activityOpts("SelectDriver"),
	)
	env.OnWorkflow(WorkUnitWorkflow, mock.Anything, mock.Anything).Return(
		func(_ workflow.Context, in WorkUnitInput) (WorkUnitResult, error) {
			return WorkUnitResult{NodeID: in.Node.ID, DriverID: in.DriverID,
				Succeeded: true, Status: "succeeded"}, nil
		})
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.BoardProjectionInput) error { return nil },
		activityOpts("ProjectToBoard"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, rec activities.TickRecord) error {
			for _, d := range rec.Dispatched {
				dispatched[d.NodeID] = true
			}
			return nil
		},
		activityOpts("EmitTickTelemetry"),
	)

	// Send the append shortly after start — while `a` is still in flight.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(AppendSignalName, appendSignal{
			Nodes: []dag.Node{
				{ID: "e", SpecRef: "076", Capability: "code.implement", Priority: 5,
					TargetRepo: "/repo/chitin", BaseRef: "main"},
			},
			Edges: []dag.Edge{{From: "e", To: "a"}}, // e depends on a
		})
	}, 0)

	env.ExecuteWorkflow(SchedulerWorkflow, SchedulerInput{
		RunID: "append-run", Nodes: nodes,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow errored: %v", err)
	}

	var status SchedulerStatus
	if err := env.GetWorkflowResult(&status); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	// The appended node joined the graph and ran after its dependency.
	if !dispatched["e"] {
		t.Error("appended node e was never dispatched")
	}
	if status.NodeStatus["e"] != "done" {
		t.Errorf("appended node e final status = %q, want done", status.NodeStatus["e"])
	}
}

// TestScheduler_RejectsCyclicAppend proves FR-012 / US2 acceptance scenario 2:
// an append that would introduce a cycle is rejected, and the scheduler
// continues unaffected — its original DAG still completes.
func TestScheduler_RejectsCyclicAppend(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// a -> b (b depends on a). A cyclic append would add edge a depends_on b.
	nodes := []dag.Node{
		{ID: "a", SpecRef: "076", Capability: "code.implement", Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main"},
		{ID: "b", SpecRef: "076", Capability: "code.implement", Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main"},
	}
	edges := []dag.Edge{{From: "b", To: "a"}}

	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.SelectDriverInput) (activities.SelectDriverResult, error) {
			return activities.SelectDriverResult{DriverID: "driver-" + in.Capability}, nil
		},
		activityOpts("SelectDriver"),
	)
	env.OnWorkflow(WorkUnitWorkflow, mock.Anything, mock.Anything).Return(
		func(_ workflow.Context, in WorkUnitInput) (WorkUnitResult, error) {
			return WorkUnitResult{NodeID: in.Node.ID, DriverID: in.DriverID,
				Succeeded: true, Status: "succeeded"}, nil
		})
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.BoardProjectionInput) error { return nil },
		activityOpts("ProjectToBoard"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.TickRecord) error { return nil },
		activityOpts("EmitTickTelemetry"),
	)

	// The cyclic append: edge a depends_on b closes the a<->b loop.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(AppendSignalName, appendSignal{
			Edges: []dag.Edge{{From: "a", To: "b"}},
		})
	}, 0)

	env.ExecuteWorkflow(SchedulerWorkflow, SchedulerInput{
		RunID: "cyclic-append", Nodes: nodes, Edges: edges,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	// The scheduler continues unaffected — the cyclic append is rejected, not
	// fatal, and the original a -> b DAG runs to completion.
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("a cyclic append must be rejected, not fail the workflow: %v", err)
	}
	var status SchedulerStatus
	if err := env.GetWorkflowResult(&status); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	for _, id := range []string{"a", "b"} {
		if status.NodeStatus[id] != "done" {
			t.Errorf("node %s final status = %q, want done — scheduler must be unaffected by the rejected append",
				id, status.NodeStatus[id])
		}
	}
}

// TestScheduler_SurfacesStalledGraph proves FR-016 / the "every node blocked"
// edge case: when a node is blocked-unroutable and another node depends on
// it, that dependent can never become runnable; with nothing runnable and
// nothing running, the scheduler surfaces an explicit stalled state and ends
// the run rather than spinning forever.
func TestScheduler_SurfacesStalledGraph(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// b is code.review (unroutable). c depends on b — c can never run because
	// b never reaches done.
	nodes := []dag.Node{
		{ID: "b", SpecRef: "076", Capability: "code.review", Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main"},
		{ID: "c", SpecRef: "076", Capability: "code.implement", Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main"},
	}
	edges := []dag.Edge{{From: "c", To: "b"}} // c depends on b

	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.SelectDriverInput) (activities.SelectDriverResult, error) {
			if in.Capability == "code.review" {
				return activities.SelectDriverResult{
					Unroutable: true, MissingCapability: in.Capability,
					Reason: "no review driver",
				}, nil
			}
			return activities.SelectDriverResult{DriverID: "driver-" + in.Capability}, nil
		},
		activityOpts("SelectDriver"),
	)
	env.OnWorkflow(WorkUnitWorkflow, mock.Anything, mock.Anything).Return(
		func(_ workflow.Context, in WorkUnitInput) (WorkUnitResult, error) {
			return WorkUnitResult{NodeID: in.Node.ID, DriverID: in.DriverID,
				Succeeded: true, Status: "succeeded"}, nil
		})
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.BoardProjectionInput) error { return nil },
		activityOpts("ProjectToBoard"),
	)
	stalledSeen := false
	env.RegisterActivityWithOptions(
		func(_ context.Context, rec activities.TickRecord) error {
			if rec.Stalled {
				stalledSeen = true
			}
			return nil
		},
		activityOpts("EmitTickTelemetry"),
	)

	env.ExecuteWorkflow(SchedulerWorkflow, SchedulerInput{
		RunID: "stall-run", Nodes: nodes, Edges: edges,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete — a stalled scheduler must end the run, not spin")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow errored: %v", err)
	}
	if !stalledSeen {
		t.Error("scheduler did not surface a stalled state in its telemetry")
	}
	var status SchedulerStatus
	if err := env.GetWorkflowResult(&status); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	if !status.Stalled {
		t.Errorf("final status must report Stalled=true; got %+v", status)
	}
	if status.NodeStatus["b"] != "blocked-unroutable" {
		t.Errorf("node b status = %q, want blocked-unroutable", status.NodeStatus["b"])
	}
	if status.NodeStatus["c"] != "pending" && status.NodeStatus["c"] != "runnable" {
		t.Errorf("node c status = %q, want pending/runnable — it never ran", status.NodeStatus["c"])
	}
}

// TestScheduler_DeterministicNodeBypassesDriverSelection proves spec 076
// FR-017 / User Story 4 acceptance scenarios 1 & 2: a runnable node of kind
// deterministic is dispatched WITHOUT driver selection — SelectDriver never
// runs for it, zero token cost — while an agent node in the same frontier is
// routed to a driver exactly as before.
//
// The child WorkUnitWorkflow is mocked (as in every scheduler test) so this
// test isolates the SCHEDULER's dispatch decision. That a deterministic node's
// work unit actually runs RunDeterministicStep is proven separately, at the
// WorkUnitWorkflow level, in work_unit_test.go.
func TestScheduler_DeterministicNodeBypassesDriverSelection(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Two roots: an agent node `impl` and a deterministic node `fmt` (gofmt).
	// `test` is a deterministic node depending on `impl` — a `go test` step
	// that runs once the agent work lands.
	nodes := []dag.Node{
		{ID: "impl", SpecRef: "076", Kind: dag.NodeKindAgent, Capability: "code.implement",
			Priority: 10, TargetRepo: "/repo/chitin", BaseRef: "main", WorktreeRequired: true},
		{ID: "fmt", SpecRef: "076", Kind: dag.NodeKindDeterministic,
			Command: "gofmt", Args: []string{"-l", "."}, Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main", WorktreeRequired: true},
		{ID: "test", SpecRef: "076", Kind: dag.NodeKindDeterministic,
			Command: "go", Args: []string{"test", "./..."}, Priority: 1,
			TargetRepo: "/repo/chitin", BaseRef: "main", WorktreeRequired: true},
	}
	edges := []dag.Edge{{From: "test", To: "impl"}} // test depends on impl

	// selectCalls records every node SelectDriver was invoked for — a
	// deterministic node must NEVER appear here (FR-017: no driver, no tokens).
	var selectCalls []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.SelectDriverInput) (activities.SelectDriverResult, error) {
			selectCalls = append(selectCalls, in.NodeID)
			return activities.SelectDriverResult{DriverID: "driver-" + in.Capability,
				Reason: "selected driver-" + in.Capability}, nil
		},
		activityOpts("SelectDriver"),
	)

	// dispatchedKind records, per dispatched node, whether the work unit it
	// was handed carried a driver id (agent) or none (deterministic).
	dispatchedDriver := map[string]string{}
	dispatchSeen := map[string]bool{}
	env.OnWorkflow(WorkUnitWorkflow, mock.Anything, mock.Anything).Return(
		func(_ workflow.Context, in WorkUnitInput) (WorkUnitResult, error) {
			dispatchedDriver[in.Node.ID] = in.DriverID
			dispatchSeen[in.Node.ID] = true
			return WorkUnitResult{NodeID: in.Node.ID, DriverID: in.DriverID,
				Succeeded: true, Status: "succeeded"}, nil
		})
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.BoardProjectionInput) error { return nil },
		activityOpts("ProjectToBoard"),
	)
	dispatchReason := map[string]string{}
	env.RegisterActivityWithOptions(
		func(_ context.Context, rec activities.TickRecord) error {
			for _, d := range rec.Dispatched {
				dispatchReason[d.NodeID] = d.SelectionReason
			}
			return nil
		},
		activityOpts("EmitTickTelemetry"),
	)

	env.ExecuteWorkflow(SchedulerWorkflow, SchedulerInput{
		RunID: "det-run", Nodes: nodes, Edges: edges,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("scheduler workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("scheduler workflow errored: %v", err)
	}

	// Driver selection ran for exactly the agent node — never a deterministic
	// one (FR-017: zero token cost for mechanical work).
	if len(selectCalls) != 1 || selectCalls[0] != "impl" {
		t.Errorf("SelectDriver called for %v, want exactly [impl] — deterministic nodes must not be routed",
			selectCalls)
	}

	// All three nodes were dispatched.
	for _, id := range []string{"impl", "fmt", "test"} {
		if !dispatchSeen[id] {
			t.Errorf("node %s was never dispatched", id)
		}
	}
	// The agent node's work unit carries a driver id; each deterministic
	// node's carries none — it runs a mechanical step instead.
	if dispatchedDriver["impl"] == "" {
		t.Error("agent node impl must be dispatched with a driver id")
	}
	for _, id := range []string{"fmt", "test"} {
		if dispatchedDriver[id] != "" {
			t.Errorf("deterministic node %s dispatched with driver %q, want empty", id, dispatchedDriver[id])
		}
	}
	// The dispatch telemetry names the deterministic step rather than a driver.
	if got := dispatchReason["fmt"]; got == "" || !strings.Contains(got, "deterministic step") {
		t.Errorf("deterministic node fmt selection reason = %q, want it to name the deterministic step", got)
	}

	// Every node — agent and deterministic — reached done.
	var status SchedulerStatus
	if err := env.GetWorkflowResult(&status); err != nil {
		t.Fatalf("decoding scheduler result: %v", err)
	}
	for _, id := range []string{"impl", "fmt", "test"} {
		if status.NodeStatus[id] != "done" {
			t.Errorf("node %s final status = %q, want done", id, status.NodeStatus[id])
		}
	}
}

// TestScheduler_DeterministicStepFailurePropagates proves spec 076 FR-017
// acceptance scenario 3: a deterministic node whose work unit fails is marked
// failed and blocks its dependents, identically to a failed agent node. The
// child work unit is mocked to fail the deterministic node `lint`.
func TestScheduler_DeterministicStepFailurePropagates(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// `lint` is a deterministic node; `impl` (agent) depends on it.
	nodes := []dag.Node{
		{ID: "lint", SpecRef: "076", Kind: dag.NodeKindDeterministic,
			Command: "golangci-lint", Args: []string{"run"}, Priority: 5,
			TargetRepo: "/repo/chitin", BaseRef: "main"},
		{ID: "impl", SpecRef: "076", Kind: dag.NodeKindAgent, Capability: "code.implement",
			Priority: 5, TargetRepo: "/repo/chitin", BaseRef: "main"},
	}
	edges := []dag.Edge{{From: "impl", To: "lint"}} // impl depends on lint

	env.RegisterActivityWithOptions(
		func(_ context.Context, in activities.SelectDriverInput) (activities.SelectDriverResult, error) {
			return activities.SelectDriverResult{DriverID: "driver-" + in.Capability}, nil
		},
		activityOpts("SelectDriver"),
	)
	// The deterministic node's work unit fails — a real failed mechanical step.
	env.OnWorkflow(WorkUnitWorkflow, mock.Anything, mock.Anything).Return(
		func(_ workflow.Context, in WorkUnitInput) (WorkUnitResult, error) {
			succeeded := in.Node.ID != "lint"
			status := "succeeded"
			if !succeeded {
				status = "failed"
			}
			return WorkUnitResult{NodeID: in.Node.ID, DriverID: in.DriverID,
				Succeeded: succeeded, Status: status}, nil
		})
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.BoardProjectionInput) error { return nil },
		activityOpts("ProjectToBoard"),
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.TickRecord) error { return nil },
		activityOpts("EmitTickTelemetry"),
	)

	env.ExecuteWorkflow(SchedulerWorkflow, SchedulerInput{
		RunID: "det-fail-run", Nodes: nodes, Edges: edges,
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("scheduler workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("scheduler workflow errored: %v", err)
	}

	var status SchedulerStatus
	if err := env.GetWorkflowResult(&status); err != nil {
		t.Fatalf("decoding scheduler result: %v", err)
	}
	if status.NodeStatus["lint"] != "failed" {
		t.Errorf("deterministic node lint status = %q, want failed", status.NodeStatus["lint"])
	}
	if status.NodeStatus["impl"] != "blocked-dependency-failed" {
		t.Errorf("node impl status = %q, want blocked-dependency-failed — a failed deterministic node blocks dependents",
			status.NodeStatus["impl"])
	}
}

// equalSeq reports whether two string slices are element-wise equal.
func equalSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalMap reports whether two string maps are equal.
func equalMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}
