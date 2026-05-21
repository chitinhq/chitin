package ingest

// Broad-net gathering (US2, P2 — spec 079 User Story 2). The operator (or
// spec 078's self-improvement loop) names a topic worth scanning, and a
// tool-equipped agent — web search, X/social search, browser, document
// reading — casts a broad net: it gathers candidate sources across the open
// web and social platforms and feeds every candidate into the SAME fetch →
// read → filter path an operator-fed item travels.
//
// SCOPE — US2 is OUT OF SCOPE for this slice. This file documents the US2
// surface so the package's shape is complete and the foundational+US1 work
// builds against it without churn when US2 lands. The gathering activity
// itself is a documented TODO: it MUST invoke a tool-equipped agent via the
// spec-075 driver contract (FR-003) — the pipeline does not re-implement
// search/browse/document tooling — and every gathering fetch is kernel-gated
// (FR-012), routed through exactly the FetchActivity in fetch.go.
//
// TODO(spec 079 US2 / T015): implement the broad-net gathering activity.
//   - Take a named topic; invoke a tool-equipped gathering agent (web search,
//     X/social search, browser, document reading) via the spec-075 driver
//     registry (FR-003). Do NOT re-implement those tools.
//   - Produce multiple candidate IngestItem STUBS, each carrying TrustGathered
//     (US2 acceptance scenario 2) — never TrustOperatorSeeded.
//   - Route every candidate through the identical FetchAndRead → Filter path
//     an operator-fed item travels (FR-001, T016).
//   - A gathering run that finds nothing credible records an EMPTY gather —
//     breadth that yields no signal is a valid outcome (T019, US2 scenario 4).
//
// TODO(spec 079 US2 / T027): bound the gathered batch size; queue candidates
// beyond the bound for a later cycle — never drop candidates silently
// (FR-016, edge case "a high volume of gathered candidates floods the
// filter"). The bound applies to gathered batches; an operator-fed item is a
// single submission and is unaffected.

// GatherRequest is the input to a future broad-net gathering run (US2). It is
// defined now so the US2 surface is named and the foundational types are
// complete; the gathering activity that consumes it is the T015 TODO above.
type GatherRequest struct {
	// Topic is the named subject to scan — "durable-execution patterns",
	// "agent-evaluation methods", a competitor's approach. It is also the
	// FilterTopic the gathered batch is ranked against (relevance, FR-006).
	Topic string `json:"topic"`
	// Keywords are the relevance terms the filter matches gathered items
	// against — see FilterTopic in filter.go.
	Keywords []string `json:"keywords"`
	// MaxCandidates bounds how many candidates a single gather run admits;
	// the remainder is queued, never dropped (FR-016, the T027 TODO).
	MaxCandidates int `json:"max_candidates"`
}

// GatherResult is the output of a future broad-net gathering run (US2) — the
// candidate stubs the gather produced. An empty Candidates slice with
// Empty=true is a valid outcome: breadth that yields no signal (US2
// acceptance scenario 4). Defined now; produced by the T015 TODO.
type GatherResult struct {
	// Candidates are the gathered source stubs — each an IngestItem with a
	// SourceRef and TrustGathered, no Content yet (the fetch stage fills it).
	Candidates []IngestItem `json:"candidates"`
	// Queued are candidates beyond MaxCandidates, held for a later cycle
	// (FR-016) — never dropped.
	Queued []IngestItem `json:"queued"`
	// Empty is true when the gather found nothing credible — a recorded,
	// valid empty gather (US2 acceptance scenario 4).
	Empty bool `json:"empty"`
}
