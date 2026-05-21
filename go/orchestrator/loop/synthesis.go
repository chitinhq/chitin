package loop

import "fmt"

// SpecCatalog answers whether a chitin spec exists and is current — the loop's
// read surface for the stale-spec rule (spec 078 edge case: a proposal would
// touch a spec that no longer exists or has been superseded).
//
// It is an INTERFACE so the loop does not hard-depend on how specs are
// catalogued: the workflow supplies a concrete catalog built by an activity
// (a filesystem scan of specs/), and tests supply a fixture. A nil catalog is
// treated permissively — every spec is assumed live — so the pure synthesis
// path is testable without one.
type SpecCatalog interface {
	// Exists reports whether the spec with the given ref (e.g. "076") exists
	// and is current — not missing, not superseded.
	Exists(specRef string) bool
}

// SynthesisResult is the outcome of attempting to turn one Finding into a
// SpecProposal. Exactly one of Proposal / Refused / Stale is set:
//
//   - Proposal is set when synthesis produced a concrete, queue-able proposal.
//   - Refused is set, with a reason, when the finding is out-of-category, not
//     proposable, or rejection-blocked — no proposal is produced (FR-007,
//     FR-015, FR-009: the loop refuses rather than emits a bad proposal).
//   - Stale is set, with a reason, when the finding targets a missing or
//     superseded spec — the finding is marked stale, not emitted against a
//     dead spec (spec 078 edge case).
type SynthesisResult struct {
	// Proposal is the produced proposal; its Valid() is true. Zero when the
	// finding was refused or stale.
	Proposal SpecProposal `json:"proposal,omitempty"`
	// Produced is true iff Proposal is set and queue-able.
	Produced bool `json:"produced"`
	// Refused is true iff the finding was declined at synthesis — the reason
	// is in Reason. A refused finding produces no proposal (spec 078 FR-007).
	Refused bool `json:"refused"`
	// Stale is true iff the finding targets a dead/superseded spec — the
	// reason is in Reason (spec 078 edge case).
	Stale bool `json:"stale"`
	// Reason is the human-readable account of a refusal or a stale verdict.
	Reason string `json:"reason"`
}

// ProseSynthesizer authors the one genuinely ambiguous part of a proposal —
// its prose body — from a Finding (spec 078 FR-009). This is the SINGLE step
// the spec reserves for a frontier agent; every other loop step is
// deterministic (spec 078 FR-008, US2).
//
// It is an INTERFACE: the loop workflow drives a frontier-agent invocation
// behind it; tests and the deterministic path use StructuredProse below, which
// assembles a body from the finding's evidence with zero frontier-token cost.
//
// TODO(spec-078-US1/T012): wire a frontier-agent ProseSynthesizer through the
// spec-075 driver registry so the workflow can author richer prose. The
// structured fallback below keeps US1 shippable and keeps the synthesis path
// fully testable without a model — it is the deterministic floor, not the
// final prose author.
type ProseSynthesizer interface {
	// SynthesizeProse returns the proposal body prose for a finding. It is the
	// only loop step permitted to cost frontier tokens (spec 078 FR-009).
	SynthesizeProse(f Finding) string
}

// StructuredProse is the zero-frontier-token ProseSynthesizer: it assembles a
// proposal body deterministically from the finding's fields and evidence. It
// is the synthesis path US1 ships with and the floor a frontier synthesizer
// must beat — never the loop's only option, but always a safe one.
type StructuredProse struct{}

// SynthesizeProse builds a deterministic, evidence-grounded proposal body.
func (StructuredProse) SynthesizeProse(f Finding) string {
	body := fmt.Sprintf(
		"The self-improvement loop observed a %s grounded in %d telemetry record(s) "+
			"on spec %s.\n\nObservation: %s\n\nEvidence:\n",
		f.Kind, len(f.Evidence), f.SpecRef, f.Summary)
	for _, ev := range f.Evidence {
		body += fmt.Sprintf("  - [%s] %s (%s): %s\n", ev.Source, ev.ID, ev.Kind, ev.Summary)
	}
	body += fmt.Sprintf(
		"\nProposed change category: %s. This proposal is queued for operator "+
			"review and has NOT been applied — the human gate is absolute.",
		f.Category)
	return body
}

// SynthesizeProposal turns one Finding into a SynthesisResult — the pure core
// of proposal generation (spec 078 FR-003, FR-004, FR-007; US1 T012, T016).
//
// The synthesis gate, in order:
//
//  1. The finding MUST be proposable — it names a spec and carries a gate-able
//     category (FR-003, FR-007). A finding failing this is Refused.
//  2. The target spec MUST exist and be current — else the finding is Stale,
//     marked rather than emitted against a dead spec (edge case).
//  3. The RejectedSet MUST allow it — a prior rejection of the identical
//     change with no new evidence Refuses it (FR-015).
//  4. Only then is a concrete SpecProposal assembled — a change against the
//     named spec, carrying the finding and its evidence, in StatusProposalPending.
//
// catalog may be nil (every spec assumed live — the permissive test path).
// rejected may be nil (nothing rejected). prose may be nil (StructuredProse is
// used). The returned proposal, when Produced, is always Valid().
func SynthesizeProposal(
	f Finding,
	catalog SpecCatalog,
	rejected *RejectedSet,
	prose ProseSynthesizer,
	cycle int,
) SynthesisResult {
	// 1. Refuse a finding that cannot become a concrete, gate-able proposal.
	if !f.IsProposable() {
		return SynthesisResult{
			Refused: true,
			Reason: fmt.Sprintf(
				"finding is not proposable (spec_ref=%q category=%q): a proposal MUST "+
					"name a spec and fall in the gate-able category set (FR-003, FR-007)",
				f.SpecRef, f.Category),
		}
	}

	// 2. Mark stale a finding whose target spec is missing or superseded —
	//    never emit a change against a dead spec (spec 078 edge case).
	if catalog != nil && !catalog.Exists(f.SpecRef) {
		return SynthesisResult{
			Stale: true,
			Reason: fmt.Sprintf(
				"target spec %q no longer exists or has been superseded — finding marked stale",
				f.SpecRef),
		}
	}

	// 3. Honor an operator rejection — do not re-propose without new evidence.
	if !rejected.AllowsReProposal(f) {
		return SynthesisResult{
			Refused: true,
			Reason: fmt.Sprintf(
				"an identical change for finding %s was rejected and no new evidence "+
					"has accrued — not re-proposing (FR-015)", f.Identity()),
		}
	}

	// 4. Assemble the concrete proposal. The body prose is the one frontier-
	//    eligible step (FR-009); everything else here is deterministic.
	if prose == nil {
		prose = StructuredProse{}
	}
	change := SpecChange{
		SpecRef:   f.SpecRef,
		Section:   sectionForFinding(f),
		After:     f.Summary,
		Rationale: fmt.Sprintf("grounded in %d telemetry record(s)", len(f.Evidence)),
	}
	proposal := SpecProposal{
		ID:         proposalIDForFinding(f),
		Cycle:      cycle,
		TargetSpec: f.SpecRef,
		Title:      proposalTitle(f),
		Body:       prose.SynthesizeProse(f),
		Category:   f.Category,
		Changes:    []SpecChange{change},
		Finding:    f,
		Status:     StatusProposalPending, // the human gate — never applied (FR-005).
	}
	if !proposal.Valid() {
		// Defensive: a finding that passed step 1 should always yield a valid
		// proposal. Refuse rather than queue a malformed one.
		return SynthesisResult{
			Refused: true,
			Reason:  "assembled proposal failed validity check — refusing to queue it",
		}
	}
	return SynthesisResult{Proposal: proposal, Produced: true}
}

// proposalTitle builds the operator-facing headline naming the failure or
// opportunity the proposal addresses (spec 078 US1 acceptance scenario 1).
func proposalTitle(f Finding) string {
	switch f.Kind {
	case FindingRecurringFailure:
		return fmt.Sprintf("Address recurring failure in spec %s: %s", f.SpecRef, f.Signature)
	case FindingRegression:
		return fmt.Sprintf("Follow-up: regression in spec %s: %s", f.SpecRef, f.Signature)
	default:
		return fmt.Sprintf("Improve spec %s: %s", f.SpecRef, f.Signature)
	}
}

// sectionForFinding picks the spec section a finding's change targets. US1
// keeps this deliberately simple — recurring failures and regressions land
// against "Edge Cases", the section a spec records failure-handling in.
//
// TODO(spec-078-US2): a richer mapping — pointing a code-against-deterministic-
// spec finding at a specific FR — once the deterministic-review tier lands.
func sectionForFinding(f Finding) string {
	switch f.Kind {
	case FindingRecurringFailure, FindingRegression:
		return "Edge Cases"
	default:
		return "Requirements"
	}
}
