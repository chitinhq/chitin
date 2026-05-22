package driver

import (
	"fmt"
	"sort"
)

// selectDriver picks one driver from candidates deterministically (FR-005).
//
// Invariant: given the same set of candidate drivers, selectDriver always
// returns the same driver and the same reason — regardless of the order
// candidates are supplied in and regardless of Go map iteration order.
//
// The ordering is a total order with three keys, applied in sequence:
//
//  1. Tier         — frontier (0) before mid (1) before local (2);
//     lower Tier ordinal wins.
//  2. CostClass    — cheaper before dearer; lower CostClass ordinal wins.
//  3. DriverID     — lexical, ascending; the stable final tie-breaker that
//     guarantees a total order even for two cards that are
//     otherwise identical (edge case "identical capability
//     cards and tier").
//
// Because DriverID is registry-unique, the third key never ties — so the
// order is total and the result is fully determined. selectDriver is pure:
// no wall-clock, no randomness, no reliance on map iteration order. It does
// not copy the input slice; callers pass a slice they own.
//
// It returns an error only when candidates is empty — selection from no
// candidates is undefined; the Registry turns that into a typed
// blocked-unroutable outcome (FR-012) before calling here on the happy path.
func selectDriver(candidates []AgentDriver) (AgentDriver, string, error) {
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("driver: no candidates to select from")
	}

	ranked := make([]AgentDriver, len(candidates))
	copy(ranked, candidates)
	sort.SliceStable(ranked, func(i, j int) bool {
		return lessDriver(ranked[i], ranked[j])
	})

	chosen := ranked[0]
	c := chosen.Card()
	reason := fmt.Sprintf(
		"selected %q: tier=%s, cost=%s, id tie-breaker over %d candidate(s)",
		chosen.ID(), c.Tier, c.CostClass, len(ranked),
	)
	return chosen, reason, nil
}

// lessDriver is the strict-weak-ordering predicate behind selectDriver:
// true iff driver a sorts strictly before driver b under the
// tier → cost class → driver-id total order. Comparing on driver id last
// makes the order total, so SliceStable here behaves like a full sort.
func lessDriver(a, b AgentDriver) bool {
	ca, cb := a.Card(), b.Card()
	if ca.Tier != cb.Tier {
		return ca.Tier < cb.Tier
	}
	if ca.CostClass != cb.CostClass {
		return ca.CostClass < cb.CostClass
	}
	return a.ID() < b.ID()
}
