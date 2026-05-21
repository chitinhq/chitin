package activities

import (
	"context"
	"fmt"
	"log"
)

// DispatchRecord is one node dispatched on a scheduler tick — the node and
// the driver it was routed to, with the selection reason.
type DispatchRecord struct {
	// NodeID is the dispatched DAG node.
	NodeID string `json:"node_id"`
	// DriverID is the driver the node was routed to.
	DriverID string `json:"driver_id"`
	// SelectionReason is the human-readable reason the driver was chosen
	// (spec 075 FR-005).
	SelectionReason string `json:"selection_reason"`
}

// TickRecord is the per-tick telemetry the scheduler emits to Chitin
// Telemetry (spec 076 FR-015, Key Entities: Tick Record). It captures, for
// one tick, the frontier the scheduler computed, the nodes it dispatched and
// where, and any nodes it marked blocked — so every scheduling decision is
// inspectable after the fact (spec 070 FR-008).
type TickRecord struct {
	// SchedulerRunID identifies the scheduler run the tick belongs to.
	SchedulerRunID string `json:"scheduler_run_id"`
	// Tick is the monotonically increasing tick counter within the run.
	Tick int `json:"tick"`
	// Frontier is the deterministically-ordered node ids that were runnable
	// on this tick.
	Frontier []string `json:"frontier"`
	// Dispatched is the nodes dispatched on this tick with their drivers and
	// selection reasons.
	Dispatched []DispatchRecord `json:"dispatched"`
	// BlockedUnroutable is the node ids marked blocked-unroutable on this
	// tick — no driver satisfied their capability (spec 076 FR-010).
	BlockedUnroutable []string `json:"blocked_unroutable"`
	// BlockedDependencyFailed is the node ids marked blocked because a
	// dependency permanently failed (spec 076 FR-011).
	BlockedDependencyFailed []string `json:"blocked_dependency_failed"`
	// Completed is the node ids whose child work unit settled on this tick.
	Completed []string `json:"completed"`
	// Stalled is true if, after this tick, no node is runnable or running
	// yet undone nodes remain (spec 076 FR-016).
	Stalled bool `json:"stalled"`
}

// TickTelemetrySink is the write-only sink for per-tick scheduler telemetry.
// It is an INTERFACE so the scheduler does not hard-depend on a concrete
// telemetry transport; the default sink logs.
type TickTelemetrySink interface {
	// Emit records one tick record. It returns an error only on a genuine
	// write fault; a telemetry fault must never stall the scheduler.
	Emit(ctx context.Context, rec TickRecord) error
}

// logTickTelemetrySink is the default TickTelemetrySink: it logs each tick
// record rather than writing a real telemetry store.
//
// TODO(spec 076 FR-015, spec 070 FR-008): replace logTickTelemetrySink with
// a concrete sink that writes Chitin Telemetry. The telemetry export surface
// is owned by spec 070 and is not yet importable from this orchestrator
// module; until it is wired, tick records are logged. Telemetry is a
// write-only side effect, never on the scheduling critical path.
type logTickTelemetrySink struct{}

// Emit logs one tick record. It never returns an error.
func (logTickTelemetrySink) Emit(_ context.Context, rec TickRecord) error {
	log.Printf(
		"tick-telemetry: run=%s tick=%d frontier=%v dispatched=%d unroutable=%v dep-failed=%v completed=%v stalled=%t",
		rec.SchedulerRunID, rec.Tick, rec.Frontier, len(rec.Dispatched),
		rec.BlockedUnroutable, rec.BlockedDependencyFailed, rec.Completed, rec.Stalled,
	)
	return nil
}

// NewLogTickTelemetrySink returns the default logging TickTelemetrySink.
func NewLogTickTelemetrySink() TickTelemetrySink { return logTickTelemetrySink{} }

// TickTelemetry is the EmitTickTelemetry activity (spec 076 FR-015). Emitting
// telemetry is a write to an external store — a SIDE EFFECT — so it MUST run
// in an activity, never in workflow code.
type TickTelemetry struct {
	// sink is the write-only telemetry sink.
	sink TickTelemetrySink
}

// NewTickTelemetry returns an EmitTickTelemetry activity bound to sink. A nil
// sink falls back to the logging sink so the activity is always usable.
func NewTickTelemetry(sink TickTelemetrySink) *TickTelemetry {
	if sink == nil {
		sink = NewLogTickTelemetrySink()
	}
	return &TickTelemetry{sink: sink}
}

// ActivityName is the stable Temporal activity name EmitTickTelemetry
// registers under.
func (a *TickTelemetry) ActivityName() string { return "EmitTickTelemetry" }

// Execute emits one per-tick telemetry record. It is the activity function
// registered with the Temporal worker.
func (a *TickTelemetry) Execute(ctx context.Context, rec TickRecord) error {
	if a.sink == nil {
		return fmt.Errorf("activities: EmitTickTelemetry has no sink bound")
	}
	if err := a.sink.Emit(ctx, rec); err != nil {
		return fmt.Errorf("activities: EmitTickTelemetry for run %s tick %d: %w",
			rec.SchedulerRunID, rec.Tick, err)
	}
	return nil
}
