package workflows

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/chitinhq/chitin/go/orchestrator/activities"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// SchedulerInput is the typed input to SchedulerWorkflow — the work-unit DAG
// to schedule, in its SERIALIZABLE form.
//
// The dag.DAG type keeps its node map and edge slice unexported, so a *dag.DAG
// cannot itself round-trip through Temporal's payload codec (a signal, a
// Continue-As-New). The scheduler therefore carries the DAG as exported
// slices — Nodes and Edges — and rebuilds a *dag.DAG from them at the start of
// every tick (buildDAG). Nodes already carry their Status, so a Continue-As-New
// that hands the updated slices forward loses no node-state progress.
type SchedulerInput struct {
	// RunID identifies this scheduler run — stable across every
	// Continue-As-New of the run. It correlates board projection and tick
	// telemetry.
	RunID string `json:"run_id"`
	// Nodes is every node in the DAG, carrying its current Status. On the
	// first run every node is dag.StatusPending; a Continue-As-New hands the
	// updated nodes forward.
	Nodes []dag.Node `json:"nodes"`
	// Edges is every depends_on edge in the DAG.
	Edges []dag.Edge `json:"edges"`
	// Tick is the tick counter carried forward across Continue-As-New, so the
	// telemetry tick numbers are monotonic for the whole run.
	Tick int `json:"tick"`
}

// appendSignal is the payload of the "append" signal — discovered work to
// splice into the running DAG (spec 076 FR-012, User Story 2).
type appendSignal struct {
	// Nodes is the discovered nodes to add. Each must have a DAG-unique id;
	// a node whose id already exists is skipped.
	Nodes []dag.Node `json:"nodes"`
	// Edges is the discovered depends_on edges to add.
	Edges []dag.Edge `json:"edges"`
}

// SchedulerStatus is the query-able state of the scheduler — what an operator
// or a test reads via the "status" query handler.
type SchedulerStatus struct {
	// RunID is the scheduler run.
	RunID string `json:"run_id"`
	// Tick is the most recently completed tick number.
	Tick int `json:"tick"`
	// NodeStatus maps every node id to its current status wire name.
	NodeStatus map[string]string `json:"node_status"`
	// Frontier is the most recent tick's runnable frontier, ordered.
	Frontier []string `json:"frontier"`
	// Running is the node ids whose child work units are in flight.
	Running []string `json:"running"`
	// Stalled is true if the DAG is stalled — no runnable, no running, undone
	// nodes remain (spec 076 FR-016).
	Stalled bool `json:"stalled"`
	// Complete is true if every node has reached a terminal status.
	Complete bool `json:"complete"`
}

const (
	// AppendSignalName is the signal an orchestrator sends to splice
	// discovered work into a running scheduler (spec 076 FR-012).
	AppendSignalName = "append"
	// StatusQueryName is the query handler name exposing SchedulerStatus.
	StatusQueryName = "status"

	// continueAsNewTickThreshold is the tick count after which the scheduler
	// drains its in-flight children and Continue-As-News to bound history
	// (spec 076 FR-006). Draining first guarantees no in-flight dispatch is
	// lost across the boundary.
	continueAsNewTickThreshold = 500

	// tickInterval is the workflow-deterministic pause between ticks when the
	// DAG has neither runnable nor newly-completed work to react to. It uses
	// workflow.Sleep — never time.Sleep — so it stays replay-deterministic.
	tickInterval = 30 * time.Second

	// oversizedAppendThreshold is the node count past which a single append
	// is flagged for a spec amendment rather than silently absorbed
	// (spec 076 FR-012, acceptance scenario 3).
	oversizedAppendThreshold = 10

	// selectActivityTimeout bounds the SelectDriver activity — a fast
	// in-memory registry lookup plus driver Ready probes.
	selectActivityTimeout = 1 * time.Minute
	// projectionActivityTimeout bounds the board-projection and telemetry
	// activities — write-only side effects off the critical path.
	projectionActivityTimeout = 1 * time.Minute
)

// SchedulerWorkflow is the durable Spec-DAG Scheduler (spec 076). It walks a
// work-unit DAG: on every tick it computes the runnable frontier, orders it
// deterministically, routes each runnable node to a capability-matched driver,
// and dispatches each as a child WorkUnitWorkflow. It replaces the kanban
// pull-loop — work order is DERIVED from the graph, never pulled from a board.
//
// Determinism (spec 076 FR-005, SC-007): SchedulerWorkflow is strictly
// deterministic. It reads time only through workflow.Now and pauses only
// through workflow.Sleep — never time.Now / time.Sleep. Every side effect —
// driver selection, worktree create/teardown, the driver invocation, board
// projection, telemetry — runs in an activity. All ordering comes from the
// pure dag library (dag.Frontier / dag.Order), never from Go map iteration.
// Replaying a tick over the same node states yields identical dispatch
// decisions.
//
// Continue-As-New (spec 076 FR-006): after continueAsNewTickThreshold ticks
// the workflow drains its in-flight children, then Continue-As-News carrying
// the updated node/edge slices forward. Draining first means no in-flight
// dispatch is lost across the boundary.
//
// Exactly-once dispatch (spec 076 FR-009): a node is dispatched only out of
// the runnable frontier, which by construction excludes any node already
// dag.StatusRunning. The scheduler sets a node to dag.StatusRunning the
// instant it dispatches it, so a later tick never re-dispatches it.
func SchedulerWorkflow(ctx workflow.Context, in SchedulerInput) (SchedulerStatus, error) {
	logger := workflow.GetLogger(ctx)

	// state is the mutable per-run scheduler state; it is the value handed
	// forward on Continue-As-New.
	state := newSchedulerState(in)

	// running tracks the in-flight child work units by node id, so a tick can
	// react to completions and the drain-before-CAN waits for exactly them.
	running := map[string]workflow.ChildWorkflowFuture{}

	// pendingAppends accumulates appends received between tick boundaries; the
	// signal handler goroutine queues them, the tick loop drains them. It is a
	// plain slice — safe because workflow.Go goroutines are cooperatively,
	// single-threaded scheduled, so there is no concurrent mutation.
	var pendingAppends []appendSignal

	// wakeCh lets the append-signal handler wake an idle tick loop. The signal
	// handler goroutine is the SOLE consumer of the append signal channel
	// (never waitForProgress) so a signal is never split between two
	// receivers; after queueing an append it sends a non-blocking nudge here.
	wakeCh := workflow.NewBufferedChannel(ctx, 1)

	// --- the append signal handler -----------------------------------------
	appendCh := workflow.GetSignalChannel(ctx, AppendSignalName)
	workflow.Go(ctx, func(gctx workflow.Context) {
		for {
			var sig appendSignal
			if !appendCh.Receive(gctx, &sig) {
				return // channel closed — workflow finishing.
			}
			pendingAppends = append(pendingAppends, sig)
			// Nudge the tick loop if it is idle. The channel is buffered with
			// capacity 1 and the send is non-blocking, so a burst of appends
			// coalesces into a single wakeup — the loop drains all of
			// pendingAppends on its next pass regardless.
			wakeCh.SendAsync(struct{}{})
		}
	})

	// --- the status query handler ------------------------------------------
	if err := workflow.SetQueryHandler(ctx, StatusQueryName, func() (SchedulerStatus, error) {
		return state.snapshot(runningIDs(running)), nil
	}); err != nil {
		return SchedulerStatus{}, fmt.Errorf("scheduler: registering status query: %w", err)
	}

	// Validate the initial DAG once — a cyclic graph never runs (spec 076
	// FR-002). The adapter (spec 077) compiles the DAG; the scheduler
	// re-checks defensively before its first tick.
	if d, err := state.buildDAG(); err != nil {
		return SchedulerStatus{}, err
	} else if err := d.Acyclic(); err != nil {
		return SchedulerStatus{}, temporal.NewNonRetryableApplicationError(
			"scheduler: refusing to run a non-acyclic DAG", "CyclicDAG", err)
	}

	// --- the tick loop ------------------------------------------------------
	for {
		// Drain queued appends into the DAG before computing the frontier, so
		// this tick's frontier already accounts for discovered work
		// (spec 076 FR-012, acceptance scenario 1).
		state.applyAppends(ctx, &pendingAppends)

		tickRec, transitions, err := state.runTick(ctx, running)
		if err != nil {
			return SchedulerStatus{}, err
		}

		// Project node-state transitions to the board read-model (write-only)
		// and emit per-tick telemetry. Neither is on the scheduling critical
		// path: a projection or telemetry fault is logged, never fatal.
		state.project(ctx, transitions)
		state.emitTelemetry(ctx, tickRec)

		// Surface every node that became blocked-unroutable this tick to the
		// human notification channel (spec 080 US2) — write-only, best-effort.
		for _, id := range tickRec.BlockedUnroutable {
			emitNotification(ctx, activities.NotificationEvent{
				Kind:    activities.NotifyNodeBlocked,
				RunID:   state.runID,
				NodeID:  id,
				Summary: "blocked-unroutable — no ready driver satisfies the node's capability",
			})
		}

		// Terminal: every node settled — the run is complete.
		if state.complete() {
			logger.Info("scheduler: DAG complete", "run", state.runID, "tick", state.tick)
			emitNotification(ctx, activities.NotificationEvent{
				Kind:    activities.NotifyRunTerminal,
				RunID:   state.runID,
				Summary: fmt.Sprintf("run complete at tick %d — every node settled", state.tick),
			})
			return state.snapshot(runningIDs(running)), nil
		}

		// Stalled: nothing runnable, nothing running, undone nodes remain.
		// Surface it as an explicit state and end the run (spec 076 FR-016)
		// rather than spinning forever.
		if tickRec.Stalled {
			logger.Warn("scheduler: DAG stalled — no runnable and no running nodes remain",
				"run", state.runID, "tick", state.tick)
			emitNotification(ctx, activities.NotificationEvent{
				Kind:    activities.NotifyRunTerminal,
				RunID:   state.runID,
				Summary: fmt.Sprintf("run STALLED at tick %d — no runnable or running nodes remain", state.tick),
			})
			return state.snapshot(runningIDs(running)), nil
		}

		// Bound history: once the tick budget is spent, drain the in-flight
		// children, fold their results in, then Continue-As-New carrying the
		// updated node/edge slices forward (spec 076 FR-006). Draining first
		// means the next run starts with no orphaned in-flight dispatch.
		if state.tick >= continueAsNewTickThreshold {
			drainRec, drainTransitions := state.drain(ctx, running)
			state.project(ctx, drainTransitions)
			state.emitTelemetry(ctx, drainRec)
			logger.Info("scheduler: Continue-As-New to bound history",
				"run", state.runID, "tick", state.tick)
			return SchedulerStatus{}, workflow.NewContinueAsNewError(ctx, SchedulerWorkflow, state.toInput())
		}

		// Wait for the next thing to react to: a child completion, an append
		// signal (via the wake channel), or — if neither — a bounded
		// deterministic pause so the scheduler does not spin on a DAG that has
		// only in-flight work.
		state.waitForProgress(ctx, running, wakeCh)
	}
}

// schedulerState is the mutable scheduler state for one run — the DAG in its
// serializable form plus the run id and tick counter.
type schedulerState struct {
	runID string
	tick  int
	nodes []dag.Node
	edges []dag.Edge
	// lastFrontier is the most recent tick's ordered frontier node ids, for
	// the status query.
	lastFrontier []string
}

// newSchedulerState builds the per-run state from the workflow input.
func newSchedulerState(in SchedulerInput) *schedulerState {
	runID := in.RunID
	if runID == "" {
		runID = "scheduler-run"
	}
	return &schedulerState{
		runID: runID,
		tick:  in.Tick,
		nodes: append([]dag.Node(nil), in.Nodes...),
		edges: append([]dag.Edge(nil), in.Edges...),
	}
}

// toInput renders the state back to a SchedulerInput for Continue-As-New.
func (s *schedulerState) toInput() SchedulerInput {
	return SchedulerInput{
		RunID: s.runID,
		Nodes: append([]dag.Node(nil), s.nodes...),
		Edges: append([]dag.Edge(nil), s.edges...),
		Tick:  s.tick,
	}
}

// buildDAG reconstructs a *dag.DAG from the serializable node/edge slices.
// The pure dag library owns every graph computation (frontier, ordering,
// acyclic, failure propagation); the scheduler only ever holds the slices.
//
// Node insertion order is the slice order; because every dag accessor sorts,
// the rebuilt DAG is independent of slice order — determinism holds.
func (s *schedulerState) buildDAG() (*dag.DAG, error) {
	d := dag.New()
	for _, n := range s.nodes {
		if err := d.AddNode(n); err != nil {
			return nil, temporal.NewNonRetryableApplicationError(
				"scheduler: malformed DAG node", "MalformedDAG", err)
		}
	}
	for _, e := range s.edges {
		if err := d.AddEdge(e.From, e.To); err != nil {
			return nil, temporal.NewNonRetryableApplicationError(
				"scheduler: malformed DAG edge", "MalformedDAG", err)
		}
	}
	return d, nil
}

// syncFromDAG copies the DAG's canonical nodes (with their updated statuses)
// back into the serializable slice, ordered by id so the slice is
// deterministic and a Continue-As-New carries identical state every replay.
func (s *schedulerState) syncFromDAG(d *dag.DAG) {
	s.nodes = d.Nodes() // dag.Nodes() is sorted by id.
}

// nodeByID returns the index of the node with the given id in s.nodes, or -1.
func (s *schedulerState) nodeIndex(id string) int {
	for i := range s.nodes {
		if s.nodes[i].ID == id {
			return i
		}
	}
	return -1
}

// applyAppends folds every queued append into the DAG. An append that would
// introduce a cycle is REJECTED whole — the candidate graph is validated with
// dag.Acyclic and, on a cycle, none of its nodes/edges are admitted and the
// scheduler continues unaffected (spec 076 FR-012, acceptance scenario 2).
//
// An append larger than oversizedAppendThreshold is still applied (if
// acyclic) but flagged for a spec amendment rather than silently absorbed
// (spec 076 FR-012, acceptance scenario 3).
func (s *schedulerState) applyAppends(ctx workflow.Context, pending *[]appendSignal) {
	if len(*pending) == 0 {
		return
	}
	logger := workflow.GetLogger(ctx)
	queued := *pending
	*pending = nil

	for _, sig := range queued {
		if len(sig.Nodes) == 0 && len(sig.Edges) == 0 {
			continue
		}

		// Build the CANDIDATE graph: current state plus the append. Validate
		// it as a whole; reject the whole append on any structural fault.
		candidate := dag.New()
		ok := true
		for _, n := range s.nodes {
			if err := candidate.AddNode(n); err != nil {
				ok = false
				break
			}
		}
		for _, n := range sig.Nodes {
			if _, exists := candidate.Node(n.ID); exists {
				// A node id already in the DAG — skip the duplicate rather
				// than fail the append; ids are the DAG's primary key.
				continue
			}
			if err := candidate.AddNode(n); err != nil {
				ok = false
				break
			}
		}
		if ok {
			for _, e := range s.edges {
				if err := candidate.AddEdge(e.From, e.To); err != nil {
					ok = false
					break
				}
			}
		}
		if ok {
			for _, e := range sig.Edges {
				if err := candidate.AddEdge(e.From, e.To); err != nil {
					ok = false
					break
				}
			}
		}
		if !ok {
			logger.Warn("scheduler: rejecting malformed append", "run", s.runID)
			continue
		}
		if err := candidate.Acyclic(); err != nil {
			// The append would create a cycle (or a dangling edge) — reject it
			// whole; the scheduler continues unaffected (FR-012).
			logger.Warn("scheduler: rejecting cyclic append", "run", s.runID, "err", err)
			continue
		}

		// Accepted: adopt the candidate graph.
		s.syncFromDAG(candidate)
		s.edges = candidate.Edges()

		if len(sig.Nodes) > oversizedAppendThreshold {
			// Flag, do not block — oversized discovered work needs a spec
			// amendment, but the work is still admitted (FR-012 scenario 3).
			logger.Warn("scheduler: oversized append flagged for spec amendment",
				"run", s.runID, "node_count", len(sig.Nodes),
				"threshold", oversizedAppendThreshold)
		} else {
			logger.Info("scheduler: append accepted", "run", s.runID,
				"nodes", len(sig.Nodes), "edges", len(sig.Edges))
		}
	}
}

// runTick runs one scheduler tick: it rebuilds the DAG, propagates any newly
// failed dependencies, computes and orders the runnable frontier, routes and
// dispatches each runnable node, and records the tick. It returns the tick
// telemetry record and the node-state transitions to project.
//
// runTick mutates s.nodes (statuses) and the running map (new children). It is
// deterministic: the dag library produces every ordering; the only side
// effects — SelectDriver, the child dispatch — are activities / child
// workflows, replay-stable through Temporal history.
func (s *schedulerState) runTick(
	ctx workflow.Context,
	running map[string]workflow.ChildWorkflowFuture,
) (activities.TickRecord, []activities.NodeTransition, error) {
	s.tick++
	logger := workflow.GetLogger(ctx)

	d, err := s.buildDAG()
	if err != nil {
		return activities.TickRecord{}, nil, err
	}

	var transitions []activities.NodeTransition
	record := func(id, from, to string) {
		n, _ := d.Node(id)
		transitions = append(transitions, activities.NodeTransition{
			NodeID:     id,
			SpecRef:    n.SpecRef,
			TaskRef:    n.TaskRef,
			FromStatus: from,
			ToStatus:   to,
			Capability: n.Capability,
			TargetRepo: n.TargetRepo,
		})
	}

	// 1. Fold in completed children: a settled child becomes done or failed,
	//    and a failed node propagates blocked-dependency-failed downstream
	//    (spec 076 FR-011).
	completed := collectCompleted(ctx, running)
	tickRec := activities.TickRecord{SchedulerRunID: s.runID, Tick: s.tick}
	for _, c := range completed {
		from := dag.StatusRunning.String()
		to := dag.StatusDone
		if !c.Succeeded {
			to = dag.StatusFailed
		}
		if err := d.SetStatus(c.NodeID, to); err != nil {
			logger.Error("scheduler: cannot settle completed node", "node", c.NodeID, "err", err)
			continue
		}
		record(c.NodeID, from, to.String())
		tickRec.Completed = append(tickRec.Completed, c.NodeID)

		if to == dag.StatusFailed {
			// Propagate the permanent failure to every transitive dependent.
			blocked := d.PropagateFailure(c.NodeID)
			for _, b := range blocked {
				record(b, dag.StatusPending.String(), dag.StatusBlockedDependencyFailed.String())
				tickRec.BlockedDependencyFailed = append(tickRec.BlockedDependencyFailed, b)
			}
		}
	}

	// 2. Compute the runnable frontier — exactly the nodes every dependency
	//    of which is done; a running node is excluded by construction so a
	//    node is never dispatched twice (spec 076 FR-003, FR-009).
	frontier := d.Frontier() // dag.Frontier is already deterministically ordered.
	s.lastFrontier = nil
	for _, n := range frontier {
		s.lastFrontier = append(s.lastFrontier, n.ID)
	}
	tickRec.Frontier = append([]string(nil), s.lastFrontier...)

	// 3. Route and dispatch each runnable node, in the frontier's
	//    deterministic order (priority desc, then node id).
	//
	//    A node's kind decides HOW it dispatches (spec 076 FR-017): an
	//    NodeKindDeterministic node runs a mechanical step as a plain
	//    activity — driver selection is SKIPPED entirely, so it costs no
	//    agent tokens. An NodeKindAgent node is routed to a capability-
	//    matched driver exactly as before.
	for _, n := range frontier {
		var (
			driverID        string
			selectionReason string
		)
		if n.Kind == dag.NodeKindAgent {
			// Agent node: route it to a driver (spec 076 FR-007). A
			// deterministic node never reaches this branch — no driver
			// selection, no token cost (FR-017).
			sel, selErr := selectDriverFor(ctx, s.runID, n)
			if selErr != nil {
				// A genuine activity fault (not a blocked-unroutable outcome):
				// surface it so the workflow can retry. The frontier is
				// ordered, so a retry resumes deterministically.
				return activities.TickRecord{}, nil, selErr
			}
			if sel.Unroutable {
				// No driver satisfies the node's capability — mark it
				// blocked-unroutable and CONTINUE; the rest of the frontier
				// still proceeds (spec 076 FR-010, acceptance scenario 4).
				if err := d.SetStatus(n.ID, dag.StatusBlockedUnroutable); err != nil {
					logger.Error("scheduler: cannot mark node blocked-unroutable", "node", n.ID, "err", err)
					continue
				}
				record(n.ID, n.Status.String(), dag.StatusBlockedUnroutable.String())
				tickRec.BlockedUnroutable = append(tickRec.BlockedUnroutable, n.ID)
				logger.Warn("scheduler: node blocked-unroutable",
					"node", n.ID, "capability", sel.MissingCapability)
				continue
			}
			driverID = sel.DriverID
			selectionReason = sel.Reason
		} else {
			// Deterministic node: dispatched to RunDeterministicStep, no
			// driver (spec 076 FR-017). The selection reason records that the
			// node bypassed routing — it ran as a mechanical step.
			selectionReason = fmt.Sprintf(
				"deterministic step %q — no driver, no token cost", deterministicStep(n))
		}

		// Mark the node running BEFORE dispatch so a later tick can never
		// re-dispatch it (exactly-once — spec 076 FR-009), then start the
		// child work unit.
		if err := d.SetStatus(n.ID, dag.StatusRunning); err != nil {
			logger.Error("scheduler: cannot mark node running", "node", n.ID, "err", err)
			continue
		}
		record(n.ID, n.Status.String(), dag.StatusRunning.String())

		future := dispatchWorkUnit(ctx, s.runID, n, driverID)
		running[n.ID] = future
		tickRec.Dispatched = append(tickRec.Dispatched, activities.DispatchRecord{
			NodeID:          n.ID,
			DriverID:        driverID,
			SelectionReason: selectionReason,
		})
	}

	// 4. Persist the updated node statuses back to the serializable slice.
	s.syncFromDAG(d)

	// 5. Recompute stall: after dispatching, is anything runnable or running?
	d2, err := s.buildDAG()
	if err != nil {
		return activities.TickRecord{}, nil, err
	}
	tickRec.Stalled = d2.Stalled() && len(running) == 0

	sort.Strings(tickRec.BlockedUnroutable)
	sort.Strings(tickRec.BlockedDependencyFailed)
	sort.Strings(tickRec.Completed)
	return tickRec, transitions, nil
}

// drain waits for every in-flight child to finish and folds the results in.
// It is called immediately before Continue-As-New so the boundary is crossed
// with no orphaned in-flight dispatch (spec 076 FR-006, edge case "history
// limit").
func (s *schedulerState) drain(
	ctx workflow.Context,
	running map[string]workflow.ChildWorkflowFuture,
) (activities.TickRecord, []activities.NodeTransition) {
	logger := workflow.GetLogger(ctx)
	rec := activities.TickRecord{SchedulerRunID: s.runID, Tick: s.tick}
	var transitions []activities.NodeTransition

	d, err := s.buildDAG()
	if err != nil {
		logger.Error("scheduler: cannot rebuild DAG to drain", "err", err)
		return rec, transitions
	}

	// Wait for the remaining children in deterministic id order.
	for _, id := range sortedKeys(running) {
		var res WorkUnitResult
		future := running[id]
		_ = future.Get(ctx, &res) // a child fault is captured in res.Succeeded.
		delete(running, id)

		to := dag.StatusDone
		if !res.Succeeded {
			to = dag.StatusFailed
		}
		if err := d.SetStatus(id, to); err != nil {
			logger.Error("scheduler: cannot settle drained node", "node", id, "err", err)
			continue
		}
		n, _ := d.Node(id)
		transitions = append(transitions, activities.NodeTransition{
			NodeID: id, SpecRef: n.SpecRef, TaskRef: n.TaskRef,
			FromStatus: dag.StatusRunning.String(), ToStatus: to.String(),
			Capability: n.Capability, TargetRepo: n.TargetRepo,
		})
		rec.Completed = append(rec.Completed, id)
		if to == dag.StatusFailed {
			for _, b := range d.PropagateFailure(id) {
				transitions = append(transitions, activities.NodeTransition{
					NodeID: b, FromStatus: dag.StatusPending.String(),
					ToStatus: dag.StatusBlockedDependencyFailed.String(),
				})
				rec.BlockedDependencyFailed = append(rec.BlockedDependencyFailed, b)
			}
		}
	}

	s.syncFromDAG(d)
	sort.Strings(rec.Completed)
	sort.Strings(rec.BlockedDependencyFailed)
	return rec, transitions
}

// waitForProgress blocks the tick loop until there is something to react to:
// an in-flight child completes, an append signal arrives (delivered through
// wakeCh by the signal-handler goroutine), or a bounded deterministic pause
// elapses. The pause uses workflow.NewTimer — never time.Sleep — so the wait
// stays replay-deterministic (spec 076 FR-005).
//
// waitForProgress does NOT receive the append signal channel directly: the
// signal-handler goroutine is the sole consumer of that channel, so a signal
// is never split between two receivers. The bounded timer prevents a busy
// spin when only in-flight work remains while still letting a later append
// wake the loop.
func (s *schedulerState) waitForProgress(
	ctx workflow.Context,
	running map[string]workflow.ChildWorkflowFuture,
	wakeCh workflow.ReceiveChannel,
) {
	sel := workflow.NewSelector(ctx)

	// Wake on any in-flight child completing — iterate in deterministic id
	// order so selector registration is replay-stable.
	for _, id := range sortedKeys(running) {
		sel.AddFuture(running[id], func(workflow.Future) {})
	}
	// Wake on an append nudge from the signal handler.
	sel.AddReceive(wakeCh, func(c workflow.ReceiveChannel, _ bool) {
		var token struct{}
		c.Receive(ctx, &token)
	})
	// Wake on a bounded deterministic timer so the loop never spins.
	timer := workflow.NewTimer(ctx, tickInterval)
	sel.AddFuture(timer, func(workflow.Future) {})

	sel.Select(ctx)
}

// project hands a tick's node-state transitions to the board-projection
// activity (write-only — spec 076 FR-014). A projection fault is logged, not
// fatal — the board is a read-model, never on the scheduling critical path.
func (s *schedulerState) project(ctx workflow.Context, transitions []activities.NodeTransition) {
	if len(transitions) == 0 {
		return
	}
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: projectionActivityTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	err := workflow.ExecuteActivity(actx, "ProjectToBoard", activities.BoardProjectionInput{
		SchedulerRunID: s.runID,
		Transitions:    transitions,
	}).Get(actx, nil)
	if err != nil {
		workflow.GetLogger(ctx).Error("scheduler: board projection failed (non-fatal)",
			"run", s.runID, "err", err)
	}
}

// emitTelemetry hands the per-tick record to the telemetry activity
// (write-only — spec 076 FR-015). A telemetry fault is logged, not fatal.
func (s *schedulerState) emitTelemetry(ctx workflow.Context, rec activities.TickRecord) {
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: projectionActivityTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	err := workflow.ExecuteActivity(actx, "EmitTickTelemetry", rec).Get(actx, nil)
	if err != nil {
		workflow.GetLogger(ctx).Error("scheduler: tick telemetry failed (non-fatal)",
			"run", s.runID, "tick", rec.Tick, "err", err)
	}
}

// complete reports whether every node has reached a terminal status — the
// run's success condition.
func (s *schedulerState) complete() bool {
	for _, n := range s.nodes {
		if !n.Status.Terminal() {
			return false
		}
	}
	return true
}

// snapshot renders the current state as a SchedulerStatus for the query
// handler. runningIDs are the node ids the caller knows are in flight.
func (s *schedulerState) snapshot(running []string) SchedulerStatus {
	st := SchedulerStatus{
		RunID:      s.runID,
		Tick:       s.tick,
		NodeStatus: make(map[string]string, len(s.nodes)),
		Frontier:   append([]string(nil), s.lastFrontier...),
		Running:    running,
		Complete:   s.complete(),
	}
	for _, n := range s.nodes {
		st.NodeStatus[n.ID] = n.Status.String()
	}
	// Stalled: rebuild the DAG and ask the pure library.
	if d, err := s.buildDAG(); err == nil {
		st.Stalled = d.Stalled() && len(running) == 0
	}
	return st
}

// --- dispatch helpers -------------------------------------------------------

// selectDriverFor runs the SelectDriver activity for one runnable node,
// returning the routing outcome. Driver selection is a side effect (it probes
// each driver's readiness) so it MUST be an activity, never workflow code.
func selectDriverFor(
	ctx workflow.Context, runID string, n dag.Node,
) (activities.SelectDriverResult, error) {
	actx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: selectActivityTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})
	var res activities.SelectDriverResult
	err := workflow.ExecuteActivity(actx, "SelectDriver", activities.SelectDriverInput{
		NodeID:     n.ID,
		Capability: n.Capability,
	}).Get(actx, &res)
	if err != nil {
		return activities.SelectDriverResult{}, err
	}
	return res, nil
}

// deterministicStep renders a deterministic node's command and args as a
// single readable string for the tick telemetry's selection reason. A node
// with no command renders as "(none)" — the work unit settles it failed
// (spec 076 FR-017 edge case).
func deterministicStep(n dag.Node) string {
	if n.Command == "" {
		return "(none)"
	}
	if len(n.Args) == 0 {
		return n.Command
	}
	return n.Command + " " + strings.Join(n.Args, " ")
}

// dispatchWorkUnit starts the per-node child WorkUnitWorkflow. The child
// workflow id is deterministic — runID plus node id — so the dispatch is
// idempotent under replay and an operator can find the child by id.
//
// driverID is empty for a NodeKindDeterministic node — the work unit runs the
// node's mechanical step instead of invoking a driver (spec 076 FR-017).
func dispatchWorkUnit(
	ctx workflow.Context, runID string, n dag.Node, driverID string,
) workflow.ChildWorkflowFuture {
	// The child workflow id is deterministic; ParentClosePolicy is left at
	// the SDK default (terminate-on-parent-close), so a Continue-As-New drain
	// — which waits for every child first — never orphans a child.
	cctx := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
		WorkflowID: fmt.Sprintf("%s::wu::%s", runID, n.ID),
	})
	return workflow.ExecuteChildWorkflow(cctx, WorkUnitWorkflow, WorkUnitInput{
		Node:           n,
		DriverID:       driverID,
		SchedulerRunID: runID,
	})
}

// collectCompleted returns the results of every child whose work unit has
// already settled, in deterministic node-id order, removing them from the
// running map. A child that is still in flight is left in the map untouched.
func collectCompleted(
	ctx workflow.Context, running map[string]workflow.ChildWorkflowFuture,
) []WorkUnitResult {
	var out []WorkUnitResult
	for _, id := range sortedKeys(running) {
		future := running[id]
		if !future.IsReady() {
			continue // still in flight — not collected this tick.
		}
		var res WorkUnitResult
		if err := future.Get(ctx, &res); err != nil {
			// A child fault still settles the node — record it as failed.
			res = WorkUnitResult{NodeID: id, Succeeded: false,
				Explanation: fmt.Sprintf("child work unit faulted: %v", err)}
		}
		if res.NodeID == "" {
			res.NodeID = id
		}
		out = append(out, res)
		delete(running, id)
	}
	return out
}

// runningIDs returns the node ids currently in flight, sorted ascending.
func runningIDs(running map[string]workflow.ChildWorkflowFuture) []string {
	return sortedKeys(running)
}

// sortedKeys returns the keys of a node-id-keyed map, sorted ascending — the
// deterministic iteration order the scheduler uses everywhere it must range a
// map (spec 076 FR-005 — never rely on Go map iteration order).
func sortedKeys(m map[string]workflow.ChildWorkflowFuture) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
