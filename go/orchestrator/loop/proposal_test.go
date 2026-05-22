package loop

import "testing"

// TestProposalStatus_Terminal proves pending is the only non-terminal status —
// the operator (not the loop) moves a proposal off pending.
func TestProposalStatus_Terminal(t *testing.T) {
	if StatusProposalPending.Terminal() {
		t.Error("pending must be non-terminal — the operator advances it")
	}
	for _, s := range []ProposalStatus{
		StatusProposalApproved, StatusProposalRejected,
		StatusProposalImplemented, StatusProposalStale,
	} {
		if !s.Terminal() {
			t.Errorf("status %q must be terminal", s)
		}
	}
}

// TestProposal_Valid proves a well-formed proposal names a spec, carries a
// change, and has a gate-able category — and an ill-formed one is rejected
// (spec 078 FR-003, FR-007).
func TestProposal_Valid(t *testing.T) {
	good := SpecProposal{
		TargetSpec: "076", Category: CategoryCodeGeneration,
		Changes: []SpecChange{{SpecRef: "076", After: "x"}},
	}
	if !good.Valid() {
		t.Error("a proposal naming a spec with a change and a gate-able category must be Valid")
	}

	cases := []struct {
		name string
		p    SpecProposal
	}{
		{"no target spec", SpecProposal{Category: CategoryCodeGeneration,
			Changes: []SpecChange{{After: "x"}}}},
		{"no changes", SpecProposal{TargetSpec: "076", Category: CategoryCodeGeneration}},
		{"ungated category", SpecProposal{TargetSpec: "076", Category: "delete_prod",
			Changes: []SpecChange{{After: "x"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.p.Valid() {
				t.Errorf("%s: proposal must not be Valid", c.name)
			}
		})
	}
}

// TestProposal_EvidenceIDs proves a proposal exposes its grounding telemetry
// record ids, sorted — the proof an operator follows back (spec 078 FR-004).
func TestProposal_EvidenceIDs(t *testing.T) {
	f := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e3", SourceCI, 12, "failure", "sig", "076"),
		rec("e1", SourceCI, 5, "failure", "sig", "076"))
	p := SpecProposal{Finding: f}
	got := p.EvidenceIDs()
	if len(got) != 2 || got[0] != "e1" || got[1] != "e3" {
		t.Errorf("EvidenceIDs = %v, want [e1 e3] sorted", got)
	}
}

// TestProposalIDForFinding_StableAcrossRecurrence proves a recurring finding
// maps to the SAME proposal id — the key duplicate suppression uses to attach
// new evidence to an existing pending proposal rather than queue a duplicate
// (spec 078 FR-014, SC-006).
func TestProposalIDForFinding_StableAcrossRecurrence(t *testing.T) {
	first := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"))
	recurred := finding(FindingRecurringFailure, "076", "sig", CategoryCodeGeneration,
		rec("e1", SourceCI, 5, "failure", "sig", "076"),
		rec("e2", SourceCI, 9, "failure", "sig", "076"))

	if proposalIDForFinding(first) != proposalIDForFinding(recurred) {
		t.Error("a recurring finding must map to the same proposal id — duplicate suppression keys on it")
	}
}
