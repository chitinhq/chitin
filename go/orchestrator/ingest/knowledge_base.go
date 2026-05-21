package ingest

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// The knowledge-base projection (US1 T012; FR-011, spec 079 Key Entities:
// Knowledge Base). The pipeline's output — kept, ranked KnowledgeItems — is
// PROJECTED into the knowledge base by an activity. The knowledge base is the
// surface spec 078's self-improvement loop reads to inform spec proposals.
//
// SIDE-EFFECT BOUNDARY (FR-011, the non-negotiable invariant): a knowledge-
// base write is a side effect, so it MUST run in a Temporal ACTIVITY, never
// in workflow code. And — critically — the projection writes to the knowledge
// base ONLY. It MUST NOT change code, policy, or configuration (FR-011,
// SC-005): the pipeline gathers, filters, and surfaces; it never acts. The
// KnowledgeBase interface below has exactly one verb — Surface — and no path
// to anything but the knowledge store.
//
// Plan note: spec 079's plan.md sketches this projection in
// `go/orchestrator/activities/knowledge_base.go`. This slice keeps the whole
// ingest pipeline inside `go/orchestrator/ingest/` per the implementation
// constraint (one self-contained package, no edits to existing packages);
// the activity is registered into the worker host by ingest.Register (see
// workflow.go). Moving it under activities/ later is a file move, not a
// rewrite — the activity name SurfaceActivityName is the stable seam.

// KnowledgeBase is the sink the pipeline surfaces kept items into (FR-011). It
// is an interface so the pipeline depends on the abstraction, not a concrete
// store — the knowledge base's storage, schema, and retrieval design are
// spec 078's surface, explicitly out of scope here (spec 079 Out of Scope:
// "the knowledge base's storage, schema, and retrieval design"). This
// interface defines only the INFLOW.
//
// The interface deliberately has a single verb. There is no Update, no
// Delete, no write to anything but the knowledge store — the type system
// itself enforces FR-011: the pipeline cannot change code or policy because
// it holds no handle that could.
type KnowledgeBase interface {
	// Surface records one kept, ranked KnowledgeItem in the knowledge base,
	// available to spec 078's loop. It is idempotent on SourceRef: surfacing
	// the same source twice is a safe no-op (the second write updates in
	// place), so the activity is safe under Temporal's at-least-once
	// execution. It returns an error only on a genuine store fault.
	Surface(ctx context.Context, item KnowledgeItem) error
}

// SurfaceActivityName is the stable Temporal activity name the knowledge-base
// projection registers under and the ingestion workflow dispatches to.
const SurfaceActivityName = "SurfaceKnowledgeItem"

// SurfaceInput is the typed input to the SurfaceKnowledgeItem activity — the
// kept KnowledgeItem to project into the knowledge base.
type SurfaceInput struct {
	// Item is the kept, ranked item to surface. It is produced only from a
	// kept Verdict (see NewKnowledgeItem) — a dropped or held item never
	// reaches this activity (SC-005).
	Item KnowledgeItem `json:"item"`
}

// SurfaceResult is the typed output of the SurfaceKnowledgeItem activity.
type SurfaceResult struct {
	// Surfaced is true when the item was recorded in the knowledge base.
	Surfaced bool `json:"surfaced"`
	// SourceRef echoes the surfaced item, for correlation in telemetry.
	SourceRef string `json:"source_ref"`
}

// SurfaceActivity is the SurfaceKnowledgeItem activity (US1 T012). It holds
// the KnowledgeBase sink, bound at worker-host startup. A nil sink falls back
// to a logging projector (see logKnowledgeBase) so the pipeline is runnable
// before the real knowledge base exists.
type SurfaceActivity struct {
	// kb is the knowledge-base sink. Nil falls back to logKnowledgeBase.
	kb KnowledgeBase
}

// NewSurfaceActivity returns a SurfaceKnowledgeItem activity bound to kb. A
// nil kb falls back to a logging projector — useful before spec 078's
// knowledge-base read-model is stood up.
func NewSurfaceActivity(kb KnowledgeBase) *SurfaceActivity {
	if kb == nil {
		kb = logKnowledgeBase{}
	}
	return &SurfaceActivity{kb: kb}
}

// ActivityName returns the activity's registration name.
func (*SurfaceActivity) ActivityName() string { return SurfaceActivityName }

// Execute projects one kept KnowledgeItem into the knowledge base. It is the
// activity function registered with the Temporal worker. It guards the
// SC-005 invariant at the boundary: an item arriving here with no SourceRef
// is malformed and is refused — the pipeline never surfaces an unanchored
// item.
func (a *SurfaceActivity) Execute(ctx context.Context, in SurfaceInput) (SurfaceResult, error) {
	if in.Item.SourceRef == "" {
		return SurfaceResult{}, fmt.Errorf("ingest: SurfaceKnowledgeItem given an item with no source ref")
	}
	if err := a.kb.Surface(ctx, in.Item); err != nil {
		return SurfaceResult{}, fmt.Errorf(
			"ingest: surfacing %q into the knowledge base: %w", in.Item.SourceRef, err)
	}
	return SurfaceResult{Surfaced: true, SourceRef: in.Item.SourceRef}, nil
}

// logKnowledgeBase is the fallback KnowledgeBase used when no real sink is
// bound. It logs each surfaced item and discards it — the pipeline runs end
// to end, but nothing is persisted. It mirrors the orchestrator's
// logBoardProjector fallback pattern (activities/board_projection.go).
//
// TODO(spec 079 / spec 078 integration): bind the production KnowledgeBase to
// spec 078's knowledge-base read-model at worker-host startup. The store's
// design is spec 078's surface (spec 079 Out of Scope); logKnowledgeBase is
// the development stand-in until that read-model exists.
type logKnowledgeBase struct{}

func (logKnowledgeBase) Surface(_ context.Context, item KnowledgeItem) error {
	log.Printf("ingest: knowledge base (logging fallback) — surfaced %q rank=%.2f trust=%s",
		item.SourceRef, item.Rank, item.Trust)
	return nil
}

// MemoryKnowledgeBase is an in-memory KnowledgeBase — a concrete sink for
// tests and for the quickstart, and a worked example of the inflow contract.
// It is concurrency-safe. It is NOT a production store; the real knowledge
// base is spec 078's surface (the TODO on logKnowledgeBase).
type MemoryKnowledgeBase struct {
	mu    sync.Mutex
	items map[string]KnowledgeItem
}

// NewMemoryKnowledgeBase returns an empty in-memory knowledge base.
func NewMemoryKnowledgeBase() *MemoryKnowledgeBase {
	return &MemoryKnowledgeBase{items: map[string]KnowledgeItem{}}
}

// Surface records item, keyed by canonical SourceRef. It is idempotent: a
// second surface of the same source updates in place (FR-014 / at-least-once
// safety).
func (m *MemoryKnowledgeBase) Surface(_ context.Context, item KnowledgeItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[CanonicalRef(item.SourceRef)] = item
	return nil
}

// Has reports whether a source is already surfaced — the dedup probe a test
// or the workflow uses to confirm FR-005 / FR-014.
func (m *MemoryKnowledgeBase) Has(sourceRef string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.items[CanonicalRef(sourceRef)]
	return ok
}

// Len returns the number of surfaced items — used by tests to assert that
// exactly the kept items, and no dropped item, reached the knowledge base
// (SC-005).
func (m *MemoryKnowledgeBase) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.items)
}

// Items returns a snapshot of every surfaced KnowledgeItem.
func (m *MemoryKnowledgeBase) Items() []KnowledgeItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]KnowledgeItem, 0, len(m.items))
	for _, it := range m.items {
		out = append(out, it)
	}
	return out
}
