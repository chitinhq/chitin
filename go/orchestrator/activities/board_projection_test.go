package activities

import (
	"context"
	"errors"
	"testing"
)

// recordingProjector is a test BoardProjector that captures every projection
// and can be made to fault.
type recordingProjector struct {
	got     []BoardProjectionInput
	failErr error
}

func (r *recordingProjector) Project(_ context.Context, in BoardProjectionInput) error {
	if r.failErr != nil {
		return r.failErr
	}
	r.got = append(r.got, in)
	return nil
}

// TestBoardProjection_Projects proves FR-014: the activity hands a batch of
// node-state transitions to the bound projector.
func TestBoardProjection_Projects(t *testing.T) {
	rec := &recordingProjector{}
	act := NewBoardProjection(rec)

	in := BoardProjectionInput{
		SchedulerRunID: "run-1",
		Transitions: []NodeTransition{
			{NodeID: "a", FromStatus: "pending", ToStatus: "running", Capability: "code.implement"},
			{NodeID: "b", FromStatus: "running", ToStatus: "done"},
		},
	}
	if err := act.Execute(context.Background(), in); err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(rec.got) != 1 || len(rec.got[0].Transitions) != 2 {
		t.Fatalf("projector received %d batches, want 1 of 2 transitions", len(rec.got))
	}
	if rec.got[0].SchedulerRunID != "run-1" {
		t.Errorf("projected run id = %q, want run-1", rec.got[0].SchedulerRunID)
	}
}

// TestBoardProjection_EmptyBatchIsNoOp proves an empty transition batch does
// not call the projector — there is nothing to project.
func TestBoardProjection_EmptyBatchIsNoOp(t *testing.T) {
	rec := &recordingProjector{}
	act := NewBoardProjection(rec)
	if err := act.Execute(context.Background(), BoardProjectionInput{SchedulerRunID: "run"}); err != nil {
		t.Fatalf("Execute on empty batch: unexpected error: %v", err)
	}
	if len(rec.got) != 0 {
		t.Errorf("projector called %d times on an empty batch, want 0", len(rec.got))
	}
}

// TestBoardProjection_SurfacesProjectorFault proves a genuine projector write
// fault is surfaced so the workflow's retry policy can act.
func TestBoardProjection_SurfacesProjectorFault(t *testing.T) {
	rec := &recordingProjector{failErr: errors.New("board write failed")}
	act := NewBoardProjection(rec)
	err := act.Execute(context.Background(), BoardProjectionInput{
		SchedulerRunID: "run",
		Transitions:    []NodeTransition{{NodeID: "a", ToStatus: "done"}},
	})
	if err == nil {
		t.Fatal("Execute must surface a projector write fault, got nil")
	}
}

// TestBoardProjection_NilProjectorFallsBackToLogging proves the constructor
// defaults a nil projector to the logging projector — the activity is always
// usable even before the cross-module board sink is wired.
func TestBoardProjection_NilProjectorFallsBackToLogging(t *testing.T) {
	act := NewBoardProjection(nil)
	if err := act.Execute(context.Background(), BoardProjectionInput{
		SchedulerRunID: "run",
		Transitions:    []NodeTransition{{NodeID: "a", ToStatus: "done"}},
	}); err != nil {
		t.Fatalf("Execute with default logging projector: unexpected error: %v", err)
	}
}

// TestTickTelemetry_Emits proves FR-015: the activity emits a tick record to
// the bound sink.
func TestTickTelemetry_Emits(t *testing.T) {
	var got []TickRecord
	sink := tickSinkFunc(func(_ context.Context, rec TickRecord) error {
		got = append(got, rec)
		return nil
	})
	act := NewTickTelemetry(sink)
	rec := TickRecord{SchedulerRunID: "run", Tick: 7, Frontier: []string{"a", "b"}}
	if err := act.Execute(context.Background(), rec); err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Tick != 7 {
		t.Fatalf("sink received %d records; want 1 with tick 7", len(got))
	}
}

// TestTickTelemetry_SurfacesSinkFault proves a sink write fault is surfaced.
func TestTickTelemetry_SurfacesSinkFault(t *testing.T) {
	sink := tickSinkFunc(func(_ context.Context, _ TickRecord) error {
		return errors.New("telemetry write failed")
	})
	act := NewTickTelemetry(sink)
	if err := act.Execute(context.Background(), TickRecord{SchedulerRunID: "run"}); err == nil {
		t.Fatal("Execute must surface a sink write fault, got nil")
	}
}

// tickSinkFunc adapts a func to the TickTelemetrySink interface.
type tickSinkFunc func(context.Context, TickRecord) error

func (f tickSinkFunc) Emit(ctx context.Context, rec TickRecord) error { return f(ctx, rec) }
