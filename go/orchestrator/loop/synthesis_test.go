package loop

import (
	"strings"
	"testing"
)

// TestSynthesize_ProducesConcreteProposal proves the US1 happy path: a
// proposable finding becomes a concrete, valid SpecProposal — named spec,
// concrete change, carried evidence, queued pending (spec 078 FR-003, FR-004,
// US1 acceptance scenario 2).
func TestSynthesize_ProducesConcreteProposal(t *testing.T) {
	f := finding(FindingRecurringFailure, "076", "go test ./... exit 1", CategoryCodeGeneration,
		rec("ci-1", SourceCI, 10, "failure", "go test ./... exit 1", "076"),
		rec("ci-2", SourceCI, 20, "failure", "go test ./... exit 1", "076"))

	res := SynthesizeProposal(f, NewStaticSpecCatalog([]string{"076"}), nil, StructuredProse{}, 7)

	if !res.Produced {
		t.Fatalf("a proposable finding must produce a proposal; refused=%v stale=%v reason=%q",
			res.Refused, res.Stale, res.Reason)
	}
	p := res.Proposal
	if !p.Valid() {
		t.Error("a produced proposal must be Valid")
	}
	if p.TargetSpec != "076" {
		t.Errorf("proposal target spec = %q, want 076 — a proposal names a spec (FR-003)", p.TargetSpec)
	}
	if p.Status != StatusProposalPending {
		t.Errorf("proposal status = %q, want pending — the loop never emits any other (FR-005)", p.Status)
	}
	if len(p.Changes) == 0 {
		t.Error("a proposal must carry at least one concrete change, not a vague suggestion")
	}
	if len(p.Finding.Evidence) != 2 {
		t.Errorf("proposal must carry its finding's evidence; got %d records", len(p.Finding.Evidence))
	}
	// The body cites the grounding telemetry — the operator reviews a claim
	// with its proof (FR-004).
	if !strings.Contains(p.Body, "ci-1") || !strings.Contains(p.Body, "ci-2") {
		t.Error("proposal body must cite the grounding telemetry records")
	}
	if p.Cycle != 7 {
		t.Errorf("proposal cycle = %d, want 7 — every proposal is attributable to its cycle", p.Cycle)
	}
}

// TestSynthesize_RefusesOutOfCategory proves FR-007: a finding whose category
// is outside the closed gate-able set is REFUSED — no proposal is produced.
func TestSynthesize_RefusesOutOfCategory(t *testing.T) {
	f := finding(FindingRecurringFailure, "076", "sig", GateableCategory("delete_prod"))
	res := SynthesizeProposal(f, nil, nil, StructuredProse{}, 1)
	if res.Produced {
		t.Error("a finding in an ungated category must be refused (FR-007)")
	}
	if !res.Refused {
		t.Error("an out-of-category finding must be marked Refused")
	}
}

// TestSynthesize_RefusesNoSpecRef proves a finding naming no spec is refused —
// a proposal is a change against a NAMED spec (spec 078 FR-003).
func TestSynthesize_RefusesNoSpecRef(t *testing.T) {
	f := finding(FindingRecurringFailure, "", "sig", CategoryCodeGeneration)
	res := SynthesizeProposal(f, nil, nil, StructuredProse{}, 1)
	if res.Produced || !res.Refused {
		t.Error("a finding with no spec ref must be refused — a proposal names a spec (FR-003)")
	}
}

// TestSynthesize_MarksStaleSpec proves the stale-spec edge case: a finding
// targeting a spec the catalog does not list as live is marked STALE — not
// emitted as a change against a dead spec.
func TestSynthesize_MarksStaleSpec(t *testing.T) {
	f := finding(FindingRecurringFailure, "999", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "999"))
	// The catalog has 076 live but not 999.
	res := SynthesizeProposal(f, NewStaticSpecCatalog([]string{"076"}), nil, StructuredProse{}, 1)
	if res.Produced {
		t.Error("a finding targeting a dead spec must not produce a proposal")
	}
	if !res.Stale {
		t.Errorf("a finding targeting a missing/superseded spec must be marked Stale; reason=%q", res.Reason)
	}
}

// TestSynthesize_HonorsRejection proves FR-015: a finding matching a prior
// rejection, with no new evidence, is refused at synthesis.
func TestSynthesize_HonorsRejection(t *testing.T) {
	f := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"))
	rejected := NewRejectedSet([]SpecProposal{{Finding: f, Status: StatusProposalRejected}})

	res := SynthesizeProposal(f, NewStaticSpecCatalog([]string{"076"}), rejected, StructuredProse{}, 1)
	if res.Produced {
		t.Error("a finding identical to a rejected proposal, no new evidence, must be refused (FR-015)")
	}
	if !res.Refused {
		t.Error("a rejection-blocked finding must be marked Refused")
	}

	// With new evidence it IS re-proposable.
	withMore := f.MergeEvidence(finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e2", SourceCI, 9, "failure", "sig", "076")))
	res2 := SynthesizeProposal(withMore, NewStaticSpecCatalog([]string{"076"}), rejected, StructuredProse{}, 2)
	if !res2.Produced {
		t.Error("a rejected finding WITH new evidence must be re-proposable (FR-015)")
	}
}

// TestSynthesize_NilCatalogIsPermissive proves a nil catalog assumes every
// spec live — the pure synthesis path is testable without a catalog.
func TestSynthesize_NilCatalogIsPermissive(t *testing.T) {
	f := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"))
	res := SynthesizeProposal(f, nil, nil, nil /* default StructuredProse */, 1)
	if !res.Produced {
		t.Errorf("with a nil catalog every spec is live — the finding must produce a proposal; reason=%q", res.Reason)
	}
}
