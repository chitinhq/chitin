package activities

import (
	"context"
	"fmt"
	"log"
)

// NodeTransition is one node-state change the scheduler projects to the
// Chitin Board read-model (spec 076 FR-014). It is a write-only record: the
// board reflects scheduler state, it never drives it (spec 070 FR-016).
type NodeTransition struct {
	// NodeID is the DAG node whose status changed.
	NodeID string `json:"node_id"`
	// SpecRef is the source spec the node derives from — e.g. "076".
	SpecRef string `json:"spec_ref"`
	// TaskRef is the task within the spec — e.g. "T009"; may be empty.
	TaskRef string `json:"task_ref"`
	// FromStatus is the node's previous status, as its spec wire name —
	// empty when the node had no prior projected status.
	FromStatus string `json:"from_status"`
	// ToStatus is the node's new status, as its spec wire name.
	ToStatus string `json:"to_status"`
	// Capability is the node's required capability tag — carried for board
	// display.
	Capability string `json:"capability"`
	// TargetRepo is the repository the node's work unit operates on.
	TargetRepo string `json:"target_repo"`
}

// BoardProjectionInput is the typed input to the ProjectToBoard activity — a
// batch of node-state transitions from one scheduler tick.
type BoardProjectionInput struct {
	// SchedulerRunID identifies the scheduler run the transitions belong to,
	// so the board can group a run's nodes.
	SchedulerRunID string `json:"scheduler_run_id"`
	// Transitions is the batch of node-state changes observed on this tick.
	// It is ordered by the scheduler (node id ascending) so projection is
	// deterministic.
	Transitions []NodeTransition `json:"transitions"`
}

// BoardProjector is the write-only sink for node-state transitions — the
// seam between the scheduler and the Chitin Board read-model (spec 076
// FR-014). It is an INTERFACE rather than a concrete board client because
// the kernel's board lives in a different Go module (internal/kanban) that
// the orchestrator module cannot import directly today; defining the seam
// here lets the scheduler ship without blocking on that cross-module wiring.
//
// An implementation MUST be write-only: Project records the transitions and
// returns. It MUST NOT be consulted to decide what the scheduler runs next —
// the runnable frontier is computed purely from the DAG and node states
// (spec 070 FR-016).
type BoardProjector interface {
	// Project records a batch of node-state transitions to the board
	// read-model. It returns an error only on a genuine write fault; a
	// projection fault must never stall the scheduler — the caller logs and
	// continues.
	Project(ctx context.Context, in BoardProjectionInput) error
}

// logBoardProjector is the default BoardProjector: it logs each transition
// rather than writing the real board.
//
// TODO(spec 076 FR-014, cross-module): replace logBoardProjector with a
// concrete projector that writes the Chitin Board. The board read-model
// lives in the kernel's `internal/kanban` package, a SEPARATE Go module from
// this orchestrator module, so a direct in-process write is not currently
// importable. The intended wiring is one of: (a) the kernel exposes a small
// board-write client as its own importable module; (b) the orchestrator
// writes board rows over the kernel's HTTP/RPC surface; or (c) the projector
// appends to the chitin chain and the kernel projects the board from it.
// Until that decision lands, the scheduler is fully functional against this
// logging projector — projection is a read-model side effect, never on the
// scheduling critical path.
type logBoardProjector struct{}

// Project logs each node-state transition. It never returns an error — a
// logging sink cannot fault — so the scheduler never stalls on projection.
func (logBoardProjector) Project(_ context.Context, in BoardProjectionInput) error {
	for _, t := range in.Transitions {
		from := t.FromStatus
		if from == "" {
			from = "(none)"
		}
		log.Printf(
			"board-projection: run=%s node=%s spec=%s task=%s %s -> %s cap=%s repo=%s",
			in.SchedulerRunID, t.NodeID, t.SpecRef, t.TaskRef, from, t.ToStatus, t.Capability, t.TargetRepo,
		)
	}
	return nil
}

// NewLogBoardProjector returns the default logging BoardProjector — the
// stand-in used until the cross-module board-write seam is wired (see the
// TODO on logBoardProjector).
func NewLogBoardProjector() BoardProjector { return logBoardProjector{} }

// BoardProjection is the ProjectToBoard activity (spec 076 FR-014).
// Projecting node-state transitions to the board read-model is a SIDE EFFECT
// — a write to an external store — so it MUST run in an activity, never in
// workflow code. The activity is bound to a BoardProjector at worker-host
// startup.
type BoardProjection struct {
	// projector is the write-only board sink. It is never read by the
	// scheduler — the board reflects scheduler state, never drives it.
	projector BoardProjector
}

// NewBoardProjection returns a ProjectToBoard activity bound to projector.
// A nil projector falls back to the logging projector so the activity is
// always usable.
func NewBoardProjection(projector BoardProjector) *BoardProjection {
	if projector == nil {
		projector = NewLogBoardProjector()
	}
	return &BoardProjection{projector: projector}
}

// ActivityName is the stable Temporal activity name ProjectToBoard registers
// under and the scheduler workflow dispatches to.
func (a *BoardProjection) ActivityName() string { return "ProjectToBoard" }

// Execute projects one tick's batch of node-state transitions to the board
// read-model. It is the activity function registered with the Temporal
// worker. Projection is write-only (spec 070 FR-016); the result is never
// fed back into scheduling.
func (a *BoardProjection) Execute(ctx context.Context, in BoardProjectionInput) error {
	if a.projector == nil {
		return fmt.Errorf("activities: ProjectToBoard has no BoardProjector bound")
	}
	if len(in.Transitions) == 0 {
		return nil
	}
	if err := a.projector.Project(ctx, in); err != nil {
		return fmt.Errorf("activities: ProjectToBoard for run %s: %w", in.SchedulerRunID, err)
	}
	return nil
}
