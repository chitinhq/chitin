package ingest

import "fmt"

// Disposition is the filter's keep/drop/hold decision for one item (FR-007,
// FR-010, spec 079 Key Entities: Filter Verdict). It is an exhaustive,
// closed set: every item the filter evaluates settles into exactly one of
// the three — the spec forbids a fourth, silent outcome.
type Disposition string

const (
	// DispositionKept is a high-signal item the filter keeps. A kept verdict
	// carries a Rank and produces a KnowledgeItem that reaches the knowledge
	// base (FR-011).
	DispositionKept Disposition = "kept"

	// DispositionDropped is a low-signal item the filter drops. A dropped
	// verdict MUST carry a recorded, auditable Reason (FR-007); a dropped
	// item NEVER reaches the knowledge base. Noise never passes silently.
	DispositionDropped Disposition = "dropped"

	// DispositionHeld is an item the filter cannot confidently assess. It is
	// held for operator review — never silently kept, never silently dropped
	// (FR-010). A held verdict carries a Reason explaining the uncertainty.
	DispositionHeld Disposition = "held-for-operator-review"
)

// Valid reports whether d is one of the three defined dispositions.
func (d Disposition) Valid() bool {
	switch d {
	case DispositionKept, DispositionDropped, DispositionHeld:
		return true
	default:
		return false
	}
}

// String renders the disposition for telemetry and audit records.
func (d Disposition) String() string { return string(d) }

// Verdict is the Filter Verdict (FR-007, FR-010, spec 079 Key Entities) — the
// filter's per-item outcome. Exactly one of three shapes: kept with a Rank,
// dropped with a Reason, or held for operator review with a Reason. The
// filter produces one Verdict per item; the pipeline routes on Disposition.
//
// A Verdict is the audit record. A drop the operator can see (US1 acceptance
// scenario 4, "the operator can see why their pick did not survive") is a
// Verdict with Disposition=dropped and a non-empty Reason — never a silent
// disappearance.
type Verdict struct {
	// SourceRef is the item the verdict is for — the audit anchor tying the
	// verdict back to its IngestItem.
	SourceRef string `json:"source_ref"`

	// Disposition is the keep/drop/hold decision — the closed three-way set.
	Disposition Disposition `json:"disposition"`

	// Rank is the filter's signal score, 0..1. It is meaningful for a KEPT
	// verdict (higher = stronger signal) and recorded for dropped/held
	// verdicts too so the operator sees how close a drop was to the bar.
	Rank float64 `json:"rank"`

	// Credibility, Relevance, and Value are the three component assessments
	// the filter combines into Rank (FR-006). Each is 0..1. They are
	// recorded so a drop's reason is decomposable — the operator can see
	// WHICH dimension a dropped item failed on.
	Credibility float64 `json:"credibility"`
	Relevance   float64 `json:"relevance"`
	Value       float64 `json:"value"`

	// Reason is the human-readable, auditable account of the verdict. It is
	// REQUIRED for a dropped verdict (FR-007) and for a held verdict
	// (FR-010); for a kept verdict it is a short keep rationale.
	Reason string `json:"reason"`

	// Trust echoes the item's provenance class, so a verdict record alone
	// shows whether an operator-seeded pick survived the filter.
	Trust TrustMarker `json:"trust"`
}

// KeptVerdict builds a Verdict for a high-signal item the filter keeps.
func KeptVerdict(it IngestItem, rank, cred, rel, val float64, reason string) Verdict {
	return Verdict{
		SourceRef:   it.SourceRef,
		Disposition: DispositionKept,
		Rank:        rank,
		Credibility: cred,
		Relevance:   rel,
		Value:       val,
		Reason:      reason,
		Trust:       it.Trust,
	}
}

// DroppedVerdict builds a Verdict for a low-signal item the filter drops. The
// reason is REQUIRED (FR-007): a drop with no recorded reason is forbidden —
// the operator must always be able to see why a pick did not survive (US1
// acceptance scenario 4). An empty reason is replaced with an explicit
// placeholder rather than allowed to be silent.
func DroppedVerdict(it IngestItem, rank, cred, rel, val float64, reason string) Verdict {
	if reason == "" {
		reason = "dropped as low-signal (no specific reason recorded — this is a filter bug)"
	}
	return Verdict{
		SourceRef:   it.SourceRef,
		Disposition: DispositionDropped,
		Rank:        rank,
		Credibility: cred,
		Relevance:   rel,
		Value:       val,
		Reason:      reason,
		Trust:       it.Trust,
	}
}

// HeldVerdict builds a Verdict for an item the filter cannot confidently
// assess — held for operator review (FR-010). The reason is REQUIRED: a held
// item must carry why it is uncertain so the operator knows what to judge.
func HeldVerdict(it IngestItem, rank, cred, rel, val float64, reason string) Verdict {
	if reason == "" {
		reason = "held for operator review (no specific reason recorded — this is a filter bug)"
	}
	return Verdict{
		SourceRef:   it.SourceRef,
		Disposition: DispositionHeld,
		Rank:        rank,
		Credibility: cred,
		Relevance:   rel,
		Value:       val,
		Reason:      reason,
		Trust:       it.Trust,
	}
}

// Validate checks the verdict's internal consistency — it is well-formed iff
// its disposition is one of the three and a dropped/held verdict carries a
// reason. It is a guard for the workflow boundary: a malformed verdict must
// never silently surface or silently vanish an item.
func (v Verdict) Validate() error {
	if v.SourceRef == "" {
		return fmt.Errorf("ingest: verdict has an empty source ref")
	}
	if !v.Disposition.Valid() {
		return fmt.Errorf("ingest: verdict for %q has an unknown disposition %q", v.SourceRef, v.Disposition)
	}
	if v.Disposition != DispositionKept && v.Reason == "" {
		return fmt.Errorf("ingest: %s verdict for %q has no recorded reason (FR-007/FR-010)",
			v.Disposition, v.SourceRef)
	}
	return nil
}

// KnowledgeItem is the pipeline's OUTPUT — a kept, ranked item bound for the
// knowledge base (FR-011, spec 079 Key Entities: Knowledge Base). It is what
// spec 078's self-improvement loop reads to inform spec proposals; it never
// changes code, policy, or configuration.
//
// A KnowledgeItem is produced ONLY for a kept Verdict — a dropped or held
// item never becomes one, so noise never reaches the knowledge base (SC-005).
type KnowledgeItem struct {
	// SourceRef is the source the knowledge came from — the dedup key and
	// the audit anchor.
	SourceRef string `json:"source_ref"`
	// Title is the source's title.
	Title string `json:"title"`
	// Content is the read content the loop may draw on. Still untrusted data
	// (FR-013) — the knowledge base stores it, the loop reasons ABOUT it,
	// nothing executes it.
	Content string `json:"content"`
	// Medium is the source's original form.
	Medium Medium `json:"medium"`
	// Trust is the item's provenance class, carried through so the loop knows
	// whether knowledge was operator-vouched or autonomously gathered.
	Trust TrustMarker `json:"trust"`
	// Rank is the filter's signal score, 0..1 — the loop may weight
	// higher-ranked knowledge more heavily.
	Rank float64 `json:"rank"`
	// FilterReason is the filter's keep rationale, carried for audit.
	FilterReason string `json:"filter_reason"`
	// FetchedAtUnix is the item's fetch time (Unix seconds), for provenance.
	FetchedAtUnix int64 `json:"fetched_at_unix"`
}

// NewKnowledgeItem builds the knowledge-base output for a kept item. It
// REQUIRES a kept verdict: calling it for a dropped or held verdict is a
// programming error — a dropped item must never become a KnowledgeItem
// (SC-005) — and returns an error rather than silently surfacing noise.
func NewKnowledgeItem(it IngestItem, v Verdict) (KnowledgeItem, error) {
	if err := v.Validate(); err != nil {
		return KnowledgeItem{}, err
	}
	if v.Disposition != DispositionKept {
		return KnowledgeItem{}, fmt.Errorf(
			"ingest: refusing to surface %q — its verdict is %s, not kept (SC-005: only kept items reach the knowledge base)",
			it.SourceRef, v.Disposition)
	}
	return KnowledgeItem{
		SourceRef:     it.SourceRef,
		Title:         it.Title,
		Content:       it.Content,
		Medium:        it.Medium,
		Trust:         it.Trust,
		Rank:          v.Rank,
		FilterReason:  v.Reason,
		FetchedAtUnix: it.FetchedAtUnix,
	}, nil
}
