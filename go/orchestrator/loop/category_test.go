package loop

import "testing"

// TestCategory_IsGateable_ClosedSet proves the gate-able category set is
// closed: every declared member is gate-able, and anything outside it — an
// invented category, the empty category — is refused (spec 078 FR-007).
func TestCategory_IsGateable_ClosedSet(t *testing.T) {
	// Every declared member is gate-able.
	for _, c := range []GateableCategory{
		CategoryCodeGeneration, CategoryPRReview,
		CategoryReviewDeterministicSpec, CategoryReviewDeterministicCode,
		CategoryE2ETestAuthoring, CategoryPeerReview,
	} {
		if !IsGateable(c) {
			t.Errorf("declared category %q must be gate-able", c)
		}
	}

	// Anything outside the closed set is refused — synthesis declines it.
	for _, c := range []GateableCategory{
		"", "deploy_to_prod", "delete_database", "rotate_secrets", "arbitrary",
	} {
		if IsGateable(c) {
			t.Errorf("category %q is outside the closed gate-able set and must be refused", c)
		}
	}
}

// TestCategory_AllGateableCategories_Sorted proves the exported view is the
// full closed set, sorted and deterministic.
func TestCategory_AllGateableCategories_Sorted(t *testing.T) {
	all := AllGateableCategories()
	if len(all) != 6 {
		t.Fatalf("AllGateableCategories returned %d, want the 6-member closed set", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1] >= all[i] {
			t.Errorf("AllGateableCategories not sorted ascending at %d: %q >= %q",
				i, all[i-1], all[i])
		}
	}
	// Every returned member round-trips through IsGateable.
	for _, c := range all {
		if !IsGateable(c) {
			t.Errorf("AllGateableCategories returned %q but IsGateable says no", c)
		}
	}
}
