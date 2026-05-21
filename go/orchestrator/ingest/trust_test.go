package ingest

import "testing"

// Spec 079 T007 / FR-002 / FR-008 tests for the Trust Marker: the
// operator-seeded marker raises trust (a credibility boost) but the boost is
// bounded so it cannot, alone, carry a low-signal item over the keep bar —
// the seed never bypasses the filter.

// TestTrustMarker_Valid proves only the two defined provenance classes are
// valid — the pipeline never ingests an item whose provenance it cannot name.
func TestTrustMarker_Valid(t *testing.T) {
	if !TrustOperatorSeeded.Valid() {
		t.Error("operator-seeded must be a valid marker")
	}
	if !TrustGathered.Valid() {
		t.Error("gathered must be a valid marker")
	}
	if TrustMarker("").Valid() {
		t.Error("an unset marker must be invalid")
	}
	if TrustMarker("forged").Valid() {
		t.Error("an unknown marker must be invalid")
	}
}

// TestTrustMarker_OperatorSeedRaisesTrust proves FR-008: the operator-seeded
// marker contributes a positive credibility boost; a gathered item gets none.
func TestTrustMarker_OperatorSeedRaisesTrust(t *testing.T) {
	if TrustOperatorSeeded.CredibilityBoost() <= 0 {
		t.Error("operator-seeded must raise trust (a positive credibility boost) — FR-008")
	}
	if TrustGathered.CredibilityBoost() != 0 {
		t.Error("a gathered item gets no trust boost — it earns its score from the source")
	}
}

// TestTrustMarker_SeedBoostDoesNotBypassFilter proves the FR-008 invariant:
// the operator-seed boost is bounded BELOW the headroom the filter's keep
// threshold leaves — the seed raises trust but cannot, by itself, drag a
// genuinely low-signal item over the keep bar. This guards the spec's edge
// case "an operator-fed item is itself low-signal".
func TestTrustMarker_SeedBoostDoesNotBypassFilter(t *testing.T) {
	// The boost feeds credibility, which is weighted 0.45 in the rank. The
	// most the seed can move the final rank is boost * credibilityWeight.
	const credibilityWeight = 0.45
	maxRankLift := operatorSeedBoost * credibilityWeight
	// If the seed could lift a rank from below the drop floor to above the
	// keep threshold, it would bypass the filter. It must not.
	if maxRankLift >= (keepThreshold - holdFloor) {
		t.Errorf("operator-seed boost lifts rank by at most %.3f, but the keep/drop band is %.3f wide — "+
			"the seed could bypass the filter (violates FR-008)",
			maxRankLift, keepThreshold-holdFloor)
	}
}
