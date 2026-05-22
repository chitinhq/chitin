// Package ingest is the Information Ingestion Pipeline (spec 079) — the
// external front-end of the self-improvement loop (spec 078). It casts a net
// for outside knowledge and filters signal from noise before that knowledge
// informs anything.
//
// Two paths feed one pipeline: an operator hand-feeds a specific
// URL/article/video as a high-trust seed (US1, P1 — implemented here), or a
// tool-equipped agent casts a broad net on a named topic (US2, P2 — see the
// TODOs in gather.go). Both route through the same fetch → read → filter →
// surface stages. Every fetched source becomes a uniform IngestItem; the
// signal/noise filter ranks it and produces a KnowledgeItem or a recorded
// drop. Only kept items reach the knowledge base; the pipeline never changes
// code, policy, or configuration (FR-011).
//
// House-style note: the filter and the verdict/item/trust types are PURE — no
// Temporal import — so the filter's determinism (the spec's crux, SC-004) can
// be exhaustively unit-tested by `go test` without a workflow harness. The
// fetch and gather steps are Temporal activities (network egress is a side
// effect, kernel-gated); the pipeline is a workflow (see workflow.go).
package ingest

// TrustMarker is the provenance class of an ingested item (FR-002, FR-008,
// spec 079 Key Entities: Trust Marker). It records HOW an item entered the
// pipeline. The marker raises an operator-seeded item's trust at the filter
// stage — but it MUST NOT let any item bypass the filter: every item, fed or
// gathered, is filtered (FR-008, edge case "an operator-fed item is itself
// low-signal").
type TrustMarker string

const (
	// TrustOperatorSeeded marks an item the operator submitted directly — a
	// specific URL/article/video they vouched for (US1, FR-002). It is the
	// high-trust class: the filter gives it a credibility boost, but still
	// scores and may still drop it (FR-008).
	TrustOperatorSeeded TrustMarker = "operator-seeded"

	// TrustGathered marks an item an autonomous broad-net gathering run
	// produced (US2, FR-001). It carries no operator vouch and so gets no
	// credibility boost at the filter — it stands or falls on the source's
	// own signal. US2 (the gathering front-end) is a documented TODO; the
	// marker is defined now so US1's filter handles both provenances.
	TrustGathered TrustMarker = "gathered"
)

// Valid reports whether m is one of the two defined provenance classes. An
// unset or unknown marker is invalid — the pipeline never ingests an item
// whose provenance it cannot name.
func (m TrustMarker) Valid() bool {
	switch m {
	case TrustOperatorSeeded, TrustGathered:
		return true
	default:
		return false
	}
}

// String renders the marker for telemetry and audit records.
func (m TrustMarker) String() string { return string(m) }

// CredibilityBoost is the credibility increment the marker contributes to an
// item's filter score. An operator vouch raises trust (FR-008); a gathered
// item gets nothing — it must earn its score from the source itself. The
// boost is a fixed, deterministic constant so the filter stays deterministic
// (FR-009): the same marker always contributes the same amount.
func (m TrustMarker) CredibilityBoost() float64 {
	switch m {
	case TrustOperatorSeeded:
		return operatorSeedBoost
	default:
		return 0
	}
}

// operatorSeedBoost is the fixed credibility increment an operator-seeded
// marker contributes (FR-008). It is deliberately bounded well below the
// keep threshold's headroom: a vouch raises trust but cannot, alone, drag a
// genuinely low-signal item over the bar — the operator seed must NOT bypass
// the filter (edge case "an operator-fed item is itself low-signal").
const operatorSeedBoost = 0.15
