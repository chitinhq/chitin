package loop

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
)

// ProposalSink is the write-only sink for a cycle's spec proposals — the seam
// between the loop and the proposal queue read-model (spec 078 FR-013, Key
// Entities: Proposal Queue).
//
// It is an INTERFACE so the loop does not hard-depend on a concrete queue
// store; the default sink logs, an in-memory sink serves tests, and a durable
// projection plugs in behind the same contract.
//
// An implementation MUST be WRITE-ONLY (spec 078 plan: consistent with 070
// FR-016 — the queue is written, never read back to decide what runs next).
// Enqueue records the proposals; it is never consulted to choose the next
// cycle's work. The loop's analysis is computed purely from telemetry.
type ProposalSink interface {
	// Enqueue records a cycle's proposals to the queue read-model. It returns
	// an error only on a genuine write fault; a queue fault must never lose
	// the loop's output silently — the caller logs and surfaces it.
	Enqueue(ctx context.Context, in ProposalQueueInput) error
}

// ProposalQueueInput is the typed input to the ProjectProposalQueue activity —
// a cycle's proposals plus the cycle attribution every proposal carries
// (spec 078 FR-013: each proposal attributable to its cycle and finding).
type ProposalQueueInput struct {
	// Cycle is the loop cycle the proposals belong to.
	Cycle int `json:"cycle"`
	// Proposals is the cycle's freshly-produced proposals — each in
	// StatusProposalPending (the loop never emits any other status — FR-005).
	Proposals []SpecProposal `json:"proposals"`
	// UpdatedProposals is the still-pending proposals whose evidence GREW this
	// cycle because a finding recurred — their new evidence is attached to the
	// existing pending proposal rather than a duplicate being queued
	// (spec 078 FR-014, SC-006).
	UpdatedProposals []SpecProposal `json:"updated_proposals"`
	// EmptyCycle is true when the cycle produced no finding worth a proposal —
	// silence is a valid outcome and is still recorded (spec 078 US3
	// acceptance scenario 4; carried here so the queue records empty cycles).
	EmptyCycle bool `json:"empty_cycle"`
}

// logProposalSink is the fallback ProposalSink: it logs each proposal rather
// than writing a durable queue. It is the safe default when no queue store is
// configured — the loop's output is still observable.
type logProposalSink struct{}

// Enqueue logs each proposal. It never returns an error.
func (logProposalSink) Enqueue(_ context.Context, in ProposalQueueInput) error {
	if in.EmptyCycle && len(in.Proposals) == 0 && len(in.UpdatedProposals) == 0 {
		log.Printf("proposal-queue: cycle=%d empty cycle — no proposals, checkpoint advances", in.Cycle)
		return nil
	}
	for _, p := range in.Proposals {
		log.Printf("proposal-queue: cycle=%d NEW proposal=%s spec=%s status=%s title=%q evidence=%v",
			in.Cycle, p.ID, p.TargetSpec, p.Status, p.Title, p.EvidenceIDs())
	}
	for _, p := range in.UpdatedProposals {
		log.Printf("proposal-queue: cycle=%d UPDATED proposal=%s spec=%s — new evidence attached evidence=%v",
			in.Cycle, p.ID, p.TargetSpec, p.EvidenceIDs())
	}
	return nil
}

// NewLogProposalSink returns the fallback logging ProposalSink.
func NewLogProposalSink() ProposalSink { return logProposalSink{} }

// MemoryProposalSink is an in-memory ProposalSink — the queue read-model held
// in a map keyed by proposal ID. It serves tests and a single-process loop;
// it is the concrete sink the US1 on-demand cycle uses by default.
//
// It is write-only by the ProposalSink contract: Enqueue records proposals.
// The Snapshot / Pending readers it exposes are for an OPERATOR or a test
// inspecting the queue — NOT for the loop to decide its next cycle (spec 078
// FR-013 / 070 FR-016). The loop never calls them.
//
// Keying by proposal ID is what makes duplicate suppression land in the queue:
// a recurring finding maps to the same proposal ID, so an UpdatedProposals
// entry overwrites the pending proposal in place — the queue never grows a
// duplicate (spec 078 FR-014, SC-006).
type MemoryProposalSink struct {
	mu sync.Mutex
	// byID is the queue read-model — every proposal the loop has enqueued,
	// keyed by its stable ID.
	byID map[string]SpecProposal
	// emptyCycles counts cycles recorded with no proposal — silence is a
	// valid, recorded outcome (spec 078 US3 acceptance scenario 4).
	emptyCycles int
}

// NewMemoryProposalSink returns an empty in-memory proposal queue.
func NewMemoryProposalSink() *MemoryProposalSink {
	return &MemoryProposalSink{byID: map[string]SpecProposal{}}
}

// Enqueue records a cycle's proposals into the in-memory queue. A new proposal
// is inserted; an updated proposal (a recurrence) overwrites the existing
// entry at the same ID — so the queue holds exactly one proposal per finding,
// never a duplicate (spec 078 FR-014). An empty cycle is counted.
func (s *MemoryProposalSink) Enqueue(_ context.Context, in ProposalQueueInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.EmptyCycle && len(in.Proposals) == 0 && len(in.UpdatedProposals) == 0 {
		s.emptyCycles++
		return nil
	}
	for _, p := range in.Proposals {
		// A loop-emitted proposal is always pending — enforce it at the queue
		// boundary too, so nothing un-gated ever enters the read-model (FR-005).
		if p.Status != StatusProposalPending {
			return fmt.Errorf(
				"loop: refusing to enqueue proposal %s with non-pending status %q — "+
					"the loop only ever queues pending proposals (FR-005)", p.ID, p.Status)
		}
		s.byID[p.ID] = p
	}
	for _, p := range in.UpdatedProposals {
		// A recurrence attaches new evidence to the SAME id — overwrite in
		// place, never insert a second entry (spec 078 FR-014, SC-006).
		s.byID[p.ID] = p
	}
	return nil
}

// Snapshot returns every proposal in the queue, ordered by ID — a deterministic
// view for an operator or a test. It is NOT a loop-scheduling input.
func (s *MemoryProposalSink) Snapshot() []SpecProposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SpecProposal, 0, len(s.byID))
	for _, p := range s.byID {
		out = append(out, p)
	}
	SortProposals(out)
	return out
}

// Pending returns the still-pending proposals in the queue, ordered by ID —
// the set the operator reviews. It is a read for an operator, never for the
// loop's own next-cycle decision (spec 078 FR-013 / 070 FR-016).
func (s *MemoryProposalSink) Pending() []SpecProposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []SpecProposal
	for _, p := range s.byID {
		if p.Status == StatusProposalPending {
			out = append(out, p)
		}
	}
	SortProposals(out)
	return out
}

// Len returns the number of proposals in the queue.
func (s *MemoryProposalSink) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byID)
}

// EmptyCycles returns how many empty cycles the queue has recorded.
func (s *MemoryProposalSink) EmptyCycles() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emptyCycles
}

// QueueActivity is the ProjectProposalQueue activity (spec 078 FR-013).
// Writing the proposal queue read-model is a SIDE EFFECT — a write to an
// external store — so it MUST run in an activity, never in workflow code. The
// activity is bound to a ProposalSink at worker-host startup.
type QueueActivity struct {
	// sink is the write-only proposal-queue sink. It is never read by the loop
	// to decide the next cycle (spec 078 FR-013 / 070 FR-016).
	sink ProposalSink
}

// NewQueueActivity returns a ProjectProposalQueue activity bound to sink. A nil
// sink falls back to the logging sink so the activity is always usable.
func NewQueueActivity(sink ProposalSink) *QueueActivity {
	if sink == nil {
		sink = NewLogProposalSink()
	}
	return &QueueActivity{sink: sink}
}

// ActivityName is the stable Temporal activity name ProjectProposalQueue
// registers under and the loop workflow dispatches to.
func (a *QueueActivity) ActivityName() string { return "ProjectProposalQueue" }

// Execute records a cycle's proposals to the queue read-model. It is the
// activity function registered with the Temporal worker. The queue is
// write-only (spec 078 FR-013); the result is never fed back into the loop.
func (a *QueueActivity) Execute(ctx context.Context, in ProposalQueueInput) error {
	if a.sink == nil {
		return fmt.Errorf("loop: ProjectProposalQueue has no ProposalSink bound")
	}
	if err := a.sink.Enqueue(ctx, in); err != nil {
		return fmt.Errorf("loop: ProjectProposalQueue for cycle %d: %w", in.Cycle, err)
	}
	return nil
}

// --- spec catalog activity --------------------------------------------------

// StaticSpecCatalog is a fixed-content SpecCatalog — it answers Exists from a
// pre-supplied set of live spec refs. It is the catalog US1 ships with and the
// catalog tests use; a live filesystem-scan catalog plugs in behind the same
// SpecCatalog interface.
//
// TODO(spec-078-US1/T016): a concrete catalog activity that scans specs/ for
// the live, non-superseded spec set replaces StaticSpecCatalog for live
// operation. The static catalog keeps the stale-spec rule (edge case) provable
// without a filesystem.
type StaticSpecCatalog struct {
	// live is the set of spec refs that exist and are current.
	live map[string]struct{}
}

// NewStaticSpecCatalog builds a catalog over a fixed set of live spec refs.
func NewStaticSpecCatalog(liveRefs []string) *StaticSpecCatalog {
	m := make(map[string]struct{}, len(liveRefs))
	for _, r := range liveRefs {
		m[r] = struct{}{}
	}
	return &StaticSpecCatalog{live: m}
}

// Exists reports whether a spec ref is live in the catalog.
func (c *StaticSpecCatalog) Exists(specRef string) bool {
	if c == nil {
		return true // a nil catalog is permissive — every spec assumed live.
	}
	_, ok := c.live[specRef]
	return ok
}

// LiveRefs returns the catalog's live spec refs, sorted — for diagnostics.
func (c *StaticSpecCatalog) LiveRefs() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.live))
	for r := range c.live {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
