package loop

import (
	"context"
	"testing"
)

// proposal builds a pending proposal grounded in a finding, for queue tests.
func proposal(id, spec string, f Finding) SpecProposal {
	return SpecProposal{
		ID: id, TargetSpec: spec, Category: CategoryCodeGeneration,
		Changes: []SpecChange{{SpecRef: spec, After: "x"}},
		Finding: f, Status: StatusProposalPending,
	}
}

// TestMemoryQueue_EnqueueAndSnapshot proves a cycle's proposals land in the
// queue, attributable and inspectable (spec 078 FR-013).
func TestMemoryQueue_EnqueueAndSnapshot(t *testing.T) {
	q := NewMemoryProposalSink()
	f := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration)
	err := q.Enqueue(context.Background(), ProposalQueueInput{
		Cycle: 1, Proposals: []SpecProposal{proposal("p-1", "076", f)},
	})
	if err != nil {
		t.Fatalf("Enqueue errored: %v", err)
	}
	if q.Len() != 1 {
		t.Fatalf("queue Len = %d, want 1", q.Len())
	}
	if got := q.Pending(); len(got) != 1 || got[0].ID != "p-1" {
		t.Errorf("Pending = %v, want [p-1]", got)
	}
}

// TestMemoryQueue_RejectsNonPending proves the human-gate boundary at the
// queue: the loop only ever queues pending proposals (spec 078 FR-005). An
// attempt to enqueue a non-pending proposal is refused.
func TestMemoryQueue_RejectsNonPending(t *testing.T) {
	q := NewMemoryProposalSink()
	approved := proposal("p-1", "076", finding(FindingRecurringFailure, "076", "s", CategoryCodeGeneration))
	approved.Status = StatusProposalApproved // not pending — must be refused.
	err := q.Enqueue(context.Background(), ProposalQueueInput{
		Cycle: 1, Proposals: []SpecProposal{approved},
	})
	if err == nil {
		t.Error("enqueueing a non-pending proposal must be refused — the loop only queues pending (FR-005)")
	}
}

// TestMemoryQueue_UpdateOverwritesInPlace proves FR-014 / SC-006: an updated
// proposal (a recurrence) overwrites the existing entry at the SAME id — the
// queue never grows a duplicate.
func TestMemoryQueue_UpdateOverwritesInPlace(t *testing.T) {
	q := NewMemoryProposalSink()
	f1 := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"))
	// Cycle 1 queues the proposal.
	if err := q.Enqueue(context.Background(), ProposalQueueInput{
		Cycle: 1, Proposals: []SpecProposal{proposal("p-x", "076", f1)},
	}); err != nil {
		t.Fatalf("cycle 1 enqueue: %v", err)
	}
	// Cycle 2: the finding recurred with new evidence — same proposal id.
	f2 := f1.MergeEvidence(finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e2", SourceCI, 9, "failure", "sig", "076")))
	updated := proposal("p-x", "076", f2) // SAME id.
	if err := q.Enqueue(context.Background(), ProposalQueueInput{
		Cycle: 2, UpdatedProposals: []SpecProposal{updated},
	}); err != nil {
		t.Fatalf("cycle 2 enqueue: %v", err)
	}

	if q.Len() != 1 {
		t.Fatalf("queue Len = %d after a recurrence, want 1 — no duplicate (SC-006)", q.Len())
	}
	got := q.Snapshot()[0]
	if len(got.Finding.Evidence) != 2 {
		t.Errorf("queued proposal evidence = %d, want 2 — the recurrence's new evidence attached",
			len(got.Finding.Evidence))
	}
}

// TestMemoryQueue_EmptyCycleRecorded proves an empty cycle is a valid,
// recorded outcome — silence is recorded, no proposal queued (spec 078 US3
// acceptance scenario 4; recorded here in the US1 slice).
func TestMemoryQueue_EmptyCycleRecorded(t *testing.T) {
	q := NewMemoryProposalSink()
	if err := q.Enqueue(context.Background(), ProposalQueueInput{
		Cycle: 1, EmptyCycle: true,
	}); err != nil {
		t.Fatalf("empty-cycle enqueue: %v", err)
	}
	if q.Len() != 0 {
		t.Errorf("an empty cycle queues no proposal; Len = %d", q.Len())
	}
	if q.EmptyCycles() != 1 {
		t.Errorf("EmptyCycles = %d, want 1 — an empty cycle is recorded", q.EmptyCycles())
	}
}

// TestQueueActivity_Execute proves the ProjectProposalQueue activity writes
// through to its sink.
func TestQueueActivity_Execute(t *testing.T) {
	sink := NewMemoryProposalSink()
	act := NewQueueActivity(sink)
	err := act.Execute(context.Background(), ProposalQueueInput{
		Cycle: 1,
		Proposals: []SpecProposal{
			proposal("p-1", "076", finding(FindingRecurringFailure, "076", "s", CategoryCodeGeneration)),
		},
	})
	if err != nil {
		t.Fatalf("Execute errored: %v", err)
	}
	if sink.Len() != 1 {
		t.Errorf("activity did not write through to the sink; Len = %d", sink.Len())
	}
}

// TestStaticSpecCatalog proves the catalog answers Exists from its live set,
// and a nil catalog is permissive.
func TestStaticSpecCatalog(t *testing.T) {
	cat := NewStaticSpecCatalog([]string{"076", "078"})
	if !cat.Exists("076") || !cat.Exists("078") {
		t.Error("catalog must report its live specs as existing")
	}
	if cat.Exists("999") {
		t.Error("catalog must report an unlisted spec as not existing")
	}
	var nilCat *StaticSpecCatalog
	if !nilCat.Exists("anything") {
		t.Error("a nil catalog must be permissive — every spec assumed live")
	}
}
