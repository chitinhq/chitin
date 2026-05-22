package loop

import "sort"

// ProposalStatus is the lifecycle state of a spec proposal in the loop's
// read-model (spec 078 Key Entities: Spec Proposal, Proposal Queue). It is a
// closed enumeration.
//
// The loop only ever PRODUCES proposals in StatusPending — the human gate is
// absolute (spec 078 FR-005): a cycle ends with its proposals queued and
// nothing in code or policy changed. Approved / Rejected / Implemented are
// states an OPERATOR action (or, for Implemented, the orchestrator after an
// approval) drives; the loop reads them only to decide what NOT to re-propose.
type ProposalStatus string

const (
	// StatusProposalPending is the state every loop-emitted proposal starts
	// in: queued for operator review, nothing applied (spec 078 FR-005).
	StatusProposalPending ProposalStatus = "pending"
	// StatusProposalApproved means the operator approved the proposal. Only an
	// operator sets this — never the loop.
	StatusProposalApproved ProposalStatus = "approved"
	// StatusProposalRejected means the operator rejected the proposal. The
	// loop records the rejection and does not re-propose the identical change
	// without new evidence (spec 078 FR-015).
	StatusProposalRejected ProposalStatus = "rejected"
	// StatusProposalImplemented means an approved proposal has been built —
	// through the orchestrator and the spec-076 scheduler, never a side
	// channel (spec 078 FR-006).
	StatusProposalImplemented ProposalStatus = "implemented"
	// StatusProposalStale means the proposal targeted a spec that no longer
	// exists or has been superseded; it is marked stale rather than emitted
	// against a dead spec (spec 078 edge case: dead / superseded spec).
	StatusProposalStale ProposalStatus = "stale"
)

// Terminal reports whether s is a settled proposal state the loop will not
// revisit. A pending proposal is the only non-terminal state — the operator,
// not the loop, moves it on.
func (s ProposalStatus) Terminal() bool {
	switch s {
	case StatusProposalApproved, StatusProposalRejected,
		StatusProposalImplemented, StatusProposalStale:
		return true
	default:
		return false
	}
}

// SpecChange is one concrete, reviewable edit a proposal makes to a chitin
// spec — a hunk, not a vague suggestion (spec 078 FR-003, US1 acceptance
// scenario 2). A proposal carries one or more of these.
type SpecChange struct {
	// SpecRef is the spec the change is against — e.g. "076". It MUST name a
	// real spec; a proposal against a missing or superseded spec is marked
	// stale (spec 078 edge case).
	SpecRef string `json:"spec_ref"`
	// Section is the part of the spec the change touches — e.g. "FR-017",
	// "Edge Cases" — so the operator can locate it.
	Section string `json:"section"`
	// Before is the current text the change replaces; empty for a pure
	// addition.
	Before string `json:"before"`
	// After is the proposed text; empty for a pure deletion.
	After string `json:"after"`
	// Rationale is the one-line reason this specific change follows from the
	// finding's evidence.
	Rationale string `json:"rationale"`
}

// SpecProposal is the loop's OUTPUT — a concrete, reviewable change against a
// named chitin spec, carrying the Finding that grounds it and that finding's
// evidence (spec 078 Key Entities: Spec Proposal, FR-003, FR-004).
//
// A SpecProposal is NEVER auto-applied. Every proposal the loop emits is
// StatusProposalPending and queued for the operator; implementation proceeds
// only after explicit operator approval — the human gate is absolute
// (spec 078 FR-005, SC-002). The loop has no side channel into the codebase.
type SpecProposal struct {
	// ID is the proposal's stable identity. It is derived from the grounding
	// finding's Identity (see NewProposal), so a recurring finding maps to the
	// SAME proposal id — the key duplicate suppression uses to attach new
	// evidence rather than re-queue (spec 078 FR-014, SC-006).
	ID string `json:"id"`
	// Cycle is the loop cycle that produced (or last touched) the proposal,
	// so every proposal is attributable to its cycle (spec 078 FR-013).
	Cycle int `json:"cycle"`
	// TargetSpec is the named chitin spec the proposal changes (spec 078
	// FR-003). It MUST be non-empty — a proposal is a change to a NAMED spec.
	TargetSpec string `json:"target_spec"`
	// Title is a short operator-facing headline naming the failure or
	// opportunity the proposal addresses (spec 078 US1 acceptance scenario 1).
	Title string `json:"title"`
	// Body is the synthesized proposal prose — the one genuinely ambiguous,
	// frontier-agent-authored part of the loop (spec 078 FR-009). For an
	// out-of-category or evidence-thin finding the loop refuses synthesis and
	// no proposal is produced at all.
	Body string `json:"body"`
	// Category is the gate-able action category the proposed work falls in
	// (spec 078 FR-007). A proposal is only ever produced for a category in
	// the closed gate-able set.
	Category GateableCategory `json:"category"`
	// Changes is the concrete spec edits — one or more SpecChange hunks. A
	// proposal with no changes is not a proposal (NewProposal rejects it).
	Changes []SpecChange `json:"changes"`
	// Finding is the grounding finding, carried verbatim so the operator
	// reviews the claim with its proof (spec 078 FR-004).
	Finding Finding `json:"finding"`
	// Status is the proposal's lifecycle state. The loop only ever emits
	// StatusProposalPending (spec 078 FR-005); other states are operator- or
	// orchestrator-driven.
	Status ProposalStatus `json:"status"`
}

// EvidenceIDs returns the cite-able telemetry-record IDs that ground the
// proposal — the proof an operator follows back to raw telemetry (spec 078
// FR-004). The result is sorted, so it is deterministic.
func (p SpecProposal) EvidenceIDs() []string {
	ids := make([]string, 0, len(p.Finding.Evidence))
	for _, ev := range p.Finding.Evidence {
		ids = append(ids, ev.ID)
	}
	sort.Strings(ids)
	return ids
}

// Valid reports whether the proposal is well-formed — it names a real spec,
// carries at least one concrete change, and has a gate-able category. An
// invalid proposal must never be queued; SynthesizeProposal returns one only
// when it is valid.
func (p SpecProposal) Valid() bool {
	return p.TargetSpec != "" && len(p.Changes) > 0 && IsGateable(p.Category)
}

// proposalIDForFinding derives a proposal id from a finding's identity. A
// recurring finding has a stable Identity, so it maps to the SAME proposal id
// every cycle — which is exactly how duplicate suppression recognizes that a
// recurrence belongs to an already-queued proposal (spec 078 FR-014).
func proposalIDForFinding(f Finding) string {
	return "proposal-" + f.Identity()
}

// SortProposals orders a slice of proposals deterministically by ID — the
// stable, unique key. A cycle's proposals are queued in this order so the
// projection is replay-stable.
func SortProposals(proposals []SpecProposal) {
	sort.SliceStable(proposals, func(i, j int) bool {
		return proposals[i].ID < proposals[j].ID
	})
}
