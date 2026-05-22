package loop

import "testing"

// TestAnalyzer_EmptyWindow proves the empty/unreachable window yields no
// findings — a valid empty cycle, never an error (spec 078 edge case).
func TestAnalyzer_EmptyWindow(t *testing.T) {
	a := NewDeterministicAnalyzer(nil)
	if got := a.Analyze(TelemetryWindow{}); len(got) != 0 {
		t.Errorf("Analyze(empty window) = %d findings, want 0", len(got))
	}
}

// TestAnalyzer_RecurringFailure_OneFinding proves the US1 core (SC-001): a
// window containing the SAME failure signature twice yields exactly ONE
// recurring-failure finding naming that signature, carrying both records as
// evidence.
func TestAnalyzer_RecurringFailure_OneFinding(t *testing.T) {
	a := NewDeterministicAnalyzer(nil)
	w := TelemetryWindow{Since: ts(0), Until: ts(60), Records: []TelemetryRecord{
		rec("ci-1", SourceCI, 10, "failure", "go test ./... exit 1", "076"),
		rec("ci-2", SourceCI, 20, "failure", "go test ./... exit 1", "076"),
	}}
	findings := a.Analyze(w)

	if len(findings) != 1 {
		t.Fatalf("Analyze found %d findings, want exactly 1 recurring failure", len(findings))
	}
	f := findings[0]
	if f.Kind != FindingRecurringFailure {
		t.Errorf("finding kind = %q, want recurring_failure", f.Kind)
	}
	if f.Signature != "go test ./... exit 1" {
		t.Errorf("finding signature = %q, want the failing command", f.Signature)
	}
	if f.SpecRef != "076" {
		t.Errorf("finding spec ref = %q, want 076", f.SpecRef)
	}
	if f.Occurrences != 2 || len(f.Evidence) != 2 {
		t.Errorf("finding occurrences=%d evidence=%d, want 2 and 2", f.Occurrences, len(f.Evidence))
	}
	// The CI source maps to the code-generation gate-able category.
	if f.Category != CategoryCodeGeneration {
		t.Errorf("finding category = %q, want code_generation", f.Category)
	}
}

// TestAnalyzer_SingleFailureIsNoise proves a failure that appears ONCE is not
// a recurrence — it produces no finding. Only a repeat is a pattern.
func TestAnalyzer_SingleFailureIsNoise(t *testing.T) {
	a := NewDeterministicAnalyzer(nil)
	w := TelemetryWindow{Records: []TelemetryRecord{
		rec("ci-1", SourceCI, 10, "failure", "a one-off failure", "076"),
	}}
	if got := a.Analyze(w); len(got) != 0 {
		t.Errorf("a single failure must not yield a finding; got %d", len(got))
	}
}

// TestAnalyzer_DistinctSignaturesAreDistinctFindings proves two different
// failure signatures, each recurring, produce two distinct findings — and a
// success record never contributes.
func TestAnalyzer_DistinctSignaturesAreDistinctFindings(t *testing.T) {
	a := NewDeterministicAnalyzer(nil)
	w := TelemetryWindow{Records: []TelemetryRecord{
		rec("ci-1", SourceCI, 1, "failure", "failure A", "076"),
		rec("ci-2", SourceCI, 2, "failure", "failure A", "076"),
		rec("pr-1", SourcePR, 3, "failure", "failure B", "078"),
		rec("pr-2", SourcePR, 4, "failure", "failure B", "078"),
		rec("ci-3", SourceCI, 5, "success", "a passing run", "076"), // not a failure.
	}}
	findings := a.Analyze(w)
	if len(findings) != 2 {
		t.Fatalf("Analyze found %d findings, want 2 distinct recurring failures", len(findings))
	}
}

// TestAnalyzer_Deterministic proves the analyzer is a pure function: 50 runs
// over the same window yield the identical findings in the identical order —
// the property the loop workflow's replay-determinism relies on.
func TestAnalyzer_Deterministic(t *testing.T) {
	a := NewDeterministicAnalyzer(nil)
	w := TelemetryWindow{Records: []TelemetryRecord{
		rec("ci-2", SourceCI, 2, "failure", "failure A", "076"),
		rec("ci-1", SourceCI, 1, "failure", "failure A", "076"),
		rec("pr-2", SourcePR, 4, "failure", "failure B", "078"),
		rec("pr-1", SourcePR, 3, "failure", "failure B", "078"),
	}}
	first := a.Analyze(w)
	for i := 0; i < 50; i++ {
		got := a.Analyze(w)
		if len(got) != len(first) {
			t.Fatalf("run %d: finding count drifted", i)
		}
		for j := range got {
			if got[j].Identity() != first[j].Identity() {
				t.Fatalf("run %d: finding %d identity drifted: %s != %s",
					i, j, got[j].Identity(), first[j].Identity())
			}
		}
	}
}

// TestSuppressDuplicates_RecurrenceAttachesEvidence proves the FR-014 rule: a
// freshly-detected finding that matches a still-pending finding is NOT emitted
// as fresh — its evidence is merged into the pending finding (returned in
// `updated`), so the loop attaches new evidence to the existing proposal
// rather than queue a duplicate (spec 078 SC-006).
func TestSuppressDuplicates_RecurrenceAttachesEvidence(t *testing.T) {
	pending := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"))
	// This cycle detects the SAME finding with a new record e2.
	detected := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"),
		rec("e2", SourceCI, 9, "failure", "sig", "076"))

	fresh, updated := SuppressDuplicates([]Finding{detected}, []Finding{pending})

	if len(fresh) != 0 {
		t.Errorf("a recurrence must not be a fresh finding; got %d fresh", len(fresh))
	}
	if len(updated) != 1 {
		t.Fatalf("a recurrence with new evidence must yield 1 updated finding; got %d", len(updated))
	}
	if len(updated[0].Evidence) != 2 {
		t.Errorf("updated finding evidence = %d, want 2 — e2 attached to the pending finding",
			len(updated[0].Evidence))
	}
}

// TestSuppressDuplicates_NewFindingIsFresh proves a finding matching no
// pending finding is genuinely new.
func TestSuppressDuplicates_NewFindingIsFresh(t *testing.T) {
	detected := finding(FindingRecurringFailure, "076", "brand new", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "brand new", "076"))
	fresh, updated := SuppressDuplicates([]Finding{detected}, nil)
	if len(fresh) != 1 || len(updated) != 0 {
		t.Errorf("a finding with no pending match must be fresh; got fresh=%d updated=%d",
			len(fresh), len(updated))
	}
}

// TestSuppressDuplicates_NoNewEvidenceIsNotUpdated proves a recurrence that
// brings NO new evidence does not produce an `updated` entry — there is
// nothing to re-attach.
func TestSuppressDuplicates_NoNewEvidenceIsNotUpdated(t *testing.T) {
	same := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"))
	fresh, updated := SuppressDuplicates([]Finding{same}, []Finding{same})
	if len(fresh) != 0 || len(updated) != 0 {
		t.Errorf("a recurrence with identical evidence yields nothing; got fresh=%d updated=%d",
			len(fresh), len(updated))
	}
}

// TestRejectedSet_AllowsReProposal proves the FR-015 rule: a finding matching a
// rejected proposal is re-proposable ONLY with strictly more evidence; a
// never-rejected finding is always allowed.
func TestRejectedSet_AllowsReProposal(t *testing.T) {
	rejectedFinding := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"))
	rs := NewRejectedSet([]SpecProposal{{
		Finding: rejectedFinding, Status: StatusProposalRejected,
	}})

	// The identical finding, same evidence — forbidden.
	if rs.AllowsReProposal(rejectedFinding) {
		t.Error("a rejected finding with no new evidence must not be re-proposable (FR-015)")
	}

	// The same finding with a new record — allowed.
	withMore := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"),
		rec("e2", SourceCI, 9, "failure", "sig", "076"))
	if !rs.AllowsReProposal(withMore) {
		t.Error("a rejected finding WITH new evidence must be re-proposable (FR-015)")
	}

	// A finding never rejected — always allowed.
	other := finding(FindingRecurringFailure, "078", "other", CategoryCodeGeneration)
	if !rs.AllowsReProposal(other) {
		t.Error("a never-rejected finding must always be re-proposable")
	}
}

// TestRejectedSet_NilIsPermissive proves a nil RejectedSet allows everything.
func TestRejectedSet_NilIsPermissive(t *testing.T) {
	var rs *RejectedSet
	if !rs.AllowsReProposal(finding(FindingRecurringFailure, "076", "x", CategoryCodeGeneration)) {
		t.Error("a nil RejectedSet must be permissive")
	}
}
