package loop

import "sort"

// GateableCategory is one member of the closed set of net-positive,
// kernel-gate-able action categories the loop is PERMITTED to propose
// autonomous work within (spec 078 FR-007, Key Entities: Gate-able Category).
//
// "Governance enables autonomy": the loop may grant high autonomy to work in
// these categories precisely because the chitin kernel gates every agent
// action and catches the dangerous case. A proposal whose work falls outside
// this set is refused at synthesis time — the loop never proposes a dangerous
// or ungated action category (spec 078 FR-007, edge case: dangerous category).
type GateableCategory string

const (
	// CategoryCodeGeneration — generating code against a spec/task.
	CategoryCodeGeneration GateableCategory = "code_generation"
	// CategoryPRReview — reviewing a pull request.
	CategoryPRReview GateableCategory = "pr_review"
	// CategoryReviewDeterministicSpec — reviewing work against a deterministic
	// spec's stated acceptance criteria.
	CategoryReviewDeterministicSpec GateableCategory = "review_against_deterministic_spec"
	// CategoryReviewDeterministicCode — reviewing code against a deterministic
	// reference implementation.
	CategoryReviewDeterministicCode GateableCategory = "review_against_deterministic_code"
	// CategoryE2ETestAuthoring — authoring an end-to-end test suite.
	CategoryE2ETestAuthoring GateableCategory = "e2e_test_authoring"
	// CategoryPeerReview — peer review of tests or of code.
	CategoryPeerReview GateableCategory = "peer_review"
)

// gateableCategories is the closed set, as a membership map. It is the single
// source of truth for IsGateable; AllGateableCategories derives an ordered
// view from it. A category not present here is, by construction, ungated.
var gateableCategories = map[GateableCategory]struct{}{
	CategoryCodeGeneration:          {},
	CategoryPRReview:                {},
	CategoryReviewDeterministicSpec: {},
	CategoryReviewDeterministicCode: {},
	CategoryE2ETestAuthoring:        {},
	CategoryPeerReview:              {},
}

// IsGateable reports whether c is in the closed gate-able category set —
// the membership check proposal synthesis calls to refuse an out-of-set
// proposal (spec 078 FR-007). The empty category is never gate-able: a
// finding that cannot name a concrete net-positive category for its proposed
// work must not produce a proposal.
func IsGateable(c GateableCategory) bool {
	if c == "" {
		return false
	}
	_, ok := gateableCategories[c]
	return ok
}

// AllGateableCategories returns the closed set, sorted ascending — a
// deterministic view for tests and for an operator reading what the loop is
// permitted to propose. It allocates fresh on each call.
func AllGateableCategories() []GateableCategory {
	out := make([]GateableCategory, 0, len(gateableCategories))
	for c := range gateableCategories {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
