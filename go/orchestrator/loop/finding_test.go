package loop

import "testing"

// finding builds a finding for tests.
func finding(kind FindingKind, specRef, sig string, cat GateableCategory, ev ...TelemetryRecord) Finding {
	return Finding{
		Kind: kind, SpecRef: specRef, Signature: sig,
		Summary: string(kind) + ": " + sig, Occurrences: len(ev),
		Category: cat, Evidence: ev,
	}
}

// TestFinding_Identity_Stable proves a finding's identity is a pure function
// of Kind+SpecRef+Signature — independent of its evidence set. Two findings of
// the same observation with DIFFERENT evidence share an identity; this is what
// duplicate suppression relies on (spec 078 FR-014).
func TestFinding_Identity_Stable(t *testing.T) {
	a := finding(FindingRecurringFailure, "076", "go test ./... failed",
		CategoryCodeGeneration, rec("e1", SourceCI, 5, "failure", "sig", "076"))
	b := finding(FindingRecurringFailure, "076", "go test ./... failed",
		CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"),
		rec("e2", SourceCI, 9, "failure", "sig", "076")) // extra evidence — a recurrence.

	if a.Identity() != b.Identity() {
		t.Errorf("identity must be independent of evidence: %s != %s", a.Identity(), b.Identity())
	}
	if !a.SameAs(b) {
		t.Error("SameAs must be true for two findings of the identical observation")
	}

	// A different signature is a different finding.
	c := finding(FindingRecurringFailure, "076", "a DIFFERENT failure", CategoryCodeGeneration)
	if a.SameAs(c) {
		t.Error("findings with different signatures must not be SameAs")
	}
	// A regression of the same signature is a DIFFERENT finding from a fresh
	// failure of it — Kind is part of the identity.
	reg := finding(FindingRegression, "076", "go test ./... failed", CategoryCodeGeneration)
	if a.SameAs(reg) {
		t.Error("a regression finding and a fresh-failure finding of the same signature must differ — Kind is part of identity")
	}
}

// TestFinding_IsProposable proves the proposability gate: a finding must name a
// spec (FR-003) AND carry a gate-able category (FR-007). Failing either makes
// it non-proposable.
func TestFinding_IsProposable(t *testing.T) {
	good := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration)
	if !good.IsProposable() {
		t.Error("a finding naming a spec with a gate-able category must be proposable")
	}

	noSpec := finding(FindingRecurringFailure, "", "sig", CategoryCodeGeneration)
	if noSpec.IsProposable() {
		t.Error("a finding with no spec ref must not be proposable — a proposal names a spec (FR-003)")
	}

	badCat := finding(FindingRecurringFailure, "076", "sig", GateableCategory("delete_prod"))
	if badCat.IsProposable() {
		t.Error("a finding outside the gate-able category set must not be proposable (FR-007)")
	}

	noCat := finding(FindingRecurringFailure, "076", "sig", "")
	if noCat.IsProposable() {
		t.Error("a finding with no category must not be proposable")
	}
}

// TestFinding_MergeEvidence_Dedups proves the FR-014 duplicate primitive: two
// telemetry records of the IDENTICAL finding collapse — the merged finding
// de-duplicates evidence by record ID, and Occurrences is the merged count.
func TestFinding_MergeEvidence_Dedups(t *testing.T) {
	e1 := rec("e1", SourceCI, 5, "failure", "sig", "076")
	e2 := rec("e2", SourceCI, 9, "failure", "sig", "076")
	e3 := rec("e3", SourceCI, 12, "failure", "sig", "076")

	original := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration, e1, e2)
	// The recurrence carries e2 again (a duplicate) plus a genuinely new e3.
	recurrence := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration, e2, e3)

	merged := original.MergeEvidence(recurrence)

	if len(merged.Evidence) != 3 {
		t.Fatalf("merged evidence = %d records, want 3 (e1,e2,e3 — e2 deduped)", len(merged.Evidence))
	}
	if merged.Occurrences != 3 {
		t.Errorf("merged Occurrences = %d, want 3 — recomputed from merged evidence", merged.Occurrences)
	}
	// The merge keeps WHICH finding this is.
	if !merged.SameAs(original) {
		t.Error("MergeEvidence must not change the finding's identity")
	}
	// Evidence is in canonical (timestamp) order: e1(5), e2(9), e3(12).
	wantOrder := []string{"e1", "e2", "e3"}
	for i, id := range wantOrder {
		if merged.Evidence[i].ID != id {
			t.Errorf("merged evidence[%d] = %q, want %q", i, merged.Evidence[i].ID, id)
		}
	}
}

// TestSortFindings_Deterministic proves SortFindings imposes a total order —
// SpecRef, Kind, Signature — so a cycle's findings are emitted replay-stably.
func TestSortFindings_Deterministic(t *testing.T) {
	findings := []Finding{
		finding(FindingRecurringFailure, "078", "z", CategoryCodeGeneration),
		finding(FindingRecurringFailure, "076", "b", CategoryCodeGeneration),
		finding(FindingMissedOpportunity, "076", "a", CategoryCodeGeneration),
		finding(FindingRecurringFailure, "076", "a", CategoryCodeGeneration),
	}
	SortFindings(findings)
	// 076 before 078; within 076, kind missed_opportunity < recurring_failure;
	// within (076, recurring_failure), signature a before b.
	want := []struct{ spec, sig string }{
		{"076", "a"}, // missed_opportunity 076/a
		{"076", "a"}, // recurring_failure 076/a
		{"076", "b"},
		{"078", "z"},
	}
	for i, w := range want {
		if findings[i].SpecRef != w.spec || findings[i].Signature != w.sig {
			t.Errorf("SortFindings[%d] = (%s,%s), want (%s,%s)",
				i, findings[i].SpecRef, findings[i].Signature, w.spec, w.sig)
		}
	}
}
