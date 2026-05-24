package review

import (
	"context"
	"fmt"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// IsShortfall reports whether a ReviewerSlate represents a pool shortfall
// (the activity returned without filling the primary slots). The workflow
// uses this to route to the named-counts halt reason.
func IsShortfall(s ReviewerSlate) bool { return s.Primary1 == "" || s.Primary2 == "" }

// ShortfallReason renders the named-counts halt reason from a slate that
// failed to fill both primary slots. Mirrors the Acceptance Scenario 4.2
// wording so the audit reads cleanly.
func ShortfallReason(s ReviewerSlate, primariesRequired int) string {
	return fmt.Sprintf("reviewer pool shortfall: need %d primaries, have %d after exclusions (excluded_author=%q)",
		primariesRequired, len(s.EligibleAfterExclusion), s.ExcludedAuthor)
}

// SelectReviewersInput is the typed input to the SelectReviewers activity —
// just enough to identify the PR's author for FR-005 exclusion and the
// arbiter-type choice for the third-slot decision.
type SelectReviewersInput struct {
	// PRAuthor is the PR author identifier (e.g., a GitHub login). Used
	// to look up the driver to exclude via Registry.LookupByGitIdentity.
	PRAuthor string `json:"pr_author"`
	// ArbiterType determines whether to fill an arbiter slot from the
	// reviewer pool (machine arbiter) or leave it empty (operator
	// arbiter — engaged via a different activity).
	ArbiterType ArbiterType `json:"arbiter_type"`
	// PrimariesRequired is the number of primary slots to fill — always 2
	// at v1. Parameterized so future versions could change it without a
	// new activity surface.
	PrimariesRequired int `json:"primaries_required"`
}

// SelectReviewers is the reviewer-pool selection activity (spec 094
// FR-004 through FR-007, R-SEL deviation note: uses Registry.DriversFor
// directly rather than calling SelectDriver in a loop).
//
// The activity is a side effect — Registry.DriversFor calls each
// candidate's Ready probe, which is live I/O — so it MUST run as an
// activity, never in workflow code. Bound to the orchestrator's driver
// registry at worker-host startup.
type SelectReviewers struct {
	registry *driver.Registry
}

// NewSelectReviewers binds a SelectReviewers activity to a registry.
func NewSelectReviewers(registry *driver.Registry) *SelectReviewers {
	return &SelectReviewers{registry: registry}
}

// ActivityName is the stable Temporal activity name.
const SelectReviewersActivityName = "SelectReviewers"

// ActivityName returns the activity's registered name.
func (a *SelectReviewers) ActivityName() string { return SelectReviewersActivityName }

// Execute returns a ReviewerSlate or an error naming the shortfall.
//
// Algorithm:
//
//  1. Look up the PR author's driver via Registry.LookupByGitIdentity.
//     If the lookup misses (operator-authored, human contributor, unmapped
//     bot), no driver is excluded.
//  2. Fetch the deterministically-ordered ready CapCodeReview driver pool
//     via Registry.DriversFor. (DriversFor already applies the Ready
//     filter per FR-006.)
//  3. Filter out the author's driver, if any.
//  4. If fewer than PrimariesRequired remain, return a shortfall error
//     (Acceptance Scenario 4.2; the workflow translates to a halted gate
//     with a named-counts reason).
//  5. Take the first PrimariesRequired entries as primaries.
//  6. If ArbiterType=machine AND a (PrimariesRequired+1)th entry exists,
//     assign it to the arbiter slot. Otherwise leave Arbiter empty —
//     the workflow either engages the operator-arbiter surface (operator
//     type) or halts with "arbiter pool exhausted" (machine type with no
//     extra driver).
func (a *SelectReviewers) Execute(ctx context.Context, in SelectReviewersInput) (ReviewerSlate, error) {
	if a.registry == nil {
		return ReviewerSlate{}, fmt.Errorf("activities/review: SelectReviewers has no driver registry bound")
	}
	if in.PrimariesRequired < 2 {
		return ReviewerSlate{}, fmt.Errorf(
			"activities/review: SelectReviewers requires at least 2 primary slots (got %d)",
			in.PrimariesRequired,
		)
	}

	// Step 1 — resolve the PR author's driver, if any.
	var excludedID string
	if authorDriver, ok := a.registry.LookupByGitIdentity(in.PRAuthor); ok {
		excludedID = authorDriver.ID()
	}

	// Step 2 — fetch the ready, capable pool. DriversFor returns the
	// deterministic id-ordered slice (see registry.go).
	pool := a.registry.DriversFor(ctx, driver.CapCodeReview)

	// Step 3 — filter the author out.
	eligible := make([]string, 0, len(pool))
	for _, d := range pool {
		if d.ID() == excludedID {
			continue
		}
		eligible = append(eligible, d.ID())
	}

	// Step 4 — shortfall: return the slate with Primary1 empty as a state
	// flag. The workflow detects shortfall by checking Primary1 == "" and
	// halts with a named-counts reason; we do NOT return an error here
	// because the activity error path discards the slate's audit fields,
	// and the audit (which driver was excluded, what the eligible pool
	// was) is what makes the halt actionable.
	if len(eligible) < in.PrimariesRequired {
		return ReviewerSlate{
			ExcludedAuthor:         excludedID,
			EligibleAfterExclusion: eligible,
		}, nil
	}

	// Step 5 — primaries are the first two.
	slate := ReviewerSlate{
		Primary1:               eligible[0],
		Primary2:               eligible[1],
		ExcludedAuthor:         excludedID,
		EligibleAfterExclusion: eligible,
	}

	// Step 6 — optional machine arbiter from a third entry.
	if in.ArbiterType == ArbiterMachine && len(eligible) >= in.PrimariesRequired+1 {
		slate.Arbiter = eligible[in.PrimariesRequired]
	}

	return slate, nil
}
