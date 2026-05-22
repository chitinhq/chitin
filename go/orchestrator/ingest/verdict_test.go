package ingest

import "testing"

// Spec 079 T007 / FR-007 / FR-010 tests for the Filter Verdict: a verdict
// covers kept/dropped/held EXHAUSTIVELY, a dropped or held verdict ALWAYS
// carries a recorded reason, and only a kept verdict can become a
// KnowledgeItem (SC-005 — noise never reaches the knowledge base).

// TestDisposition_Exhaustive proves the three dispositions are the closed set
// — the spec forbids a fourth, silent outcome.
func TestDisposition_Exhaustive(t *testing.T) {
	for _, d := range []Disposition{DispositionKept, DispositionDropped, DispositionHeld} {
		if !d.Valid() {
			t.Errorf("%q must be a valid disposition", d)
		}
	}
	if Disposition("").Valid() || Disposition("ignored").Valid() {
		t.Error("an unknown disposition must be invalid — keep/drop/hold is the closed set")
	}
}

// TestDroppedVerdict_AlwaysHasReason proves FR-007: a dropped verdict always
// carries a recorded reason, even when the caller passes none — a silent drop
// is impossible to construct.
func TestDroppedVerdict_AlwaysHasReason(t *testing.T) {
	it := IngestItem{SourceRef: "https://example.com/x", Trust: TrustGathered}
	v := DroppedVerdict(it, 0.2, 0.2, 0.2, 0.2, "")
	if v.Disposition != DispositionDropped {
		t.Fatalf("Disposition = %q, want dropped", v.Disposition)
	}
	if v.Reason == "" {
		t.Error("a dropped verdict must always carry a reason (FR-007) — never silent")
	}
	if err := v.Validate(); err != nil {
		t.Errorf("a constructed dropped verdict must validate: %v", err)
	}
}

// TestHeldVerdict_AlwaysHasReason proves FR-010: a held verdict always
// carries a reason explaining the uncertainty.
func TestHeldVerdict_AlwaysHasReason(t *testing.T) {
	it := IngestItem{SourceRef: "https://example.com/x", Trust: TrustGathered}
	v := HeldVerdict(it, 0.5, 0.5, 0.5, 0.5, "")
	if v.Reason == "" {
		t.Error("a held verdict must always carry a reason (FR-010)")
	}
	if err := v.Validate(); err != nil {
		t.Errorf("a constructed held verdict must validate: %v", err)
	}
}

// TestVerdict_OperatorSeededItemCanStillDrop proves FR-008 at the type level:
// an operator-seeded item can still be constructed into a DROPPED verdict —
// the marker raises trust but does not force a keep.
func TestVerdict_OperatorSeededItemCanStillDrop(t *testing.T) {
	it := IngestItem{SourceRef: "https://example.com/x", Trust: TrustOperatorSeeded}
	v := DroppedVerdict(it, 0.1, 0.1, 0.1, 0.1, "low signal despite the operator seed")
	if v.Disposition != DispositionDropped {
		t.Error("an operator-seeded item must still be droppable (FR-008)")
	}
	if v.Trust != TrustOperatorSeeded {
		t.Error("the verdict must carry the item's provenance for audit")
	}
}

// TestVerdict_Validate proves a malformed verdict — unknown disposition, or a
// drop with no reason — is caught by Validate, the workflow-boundary guard.
func TestVerdict_Validate(t *testing.T) {
	if err := (Verdict{}).Validate(); err == nil {
		t.Error("an empty verdict must fail validation")
	}
	bad := Verdict{SourceRef: "https://example.com/x", Disposition: DispositionDropped, Reason: ""}
	if err := bad.Validate(); err == nil {
		t.Error("a dropped verdict with no reason must fail validation (FR-007)")
	}
	unknown := Verdict{SourceRef: "https://example.com/x", Disposition: Disposition("???")}
	if err := unknown.Validate(); err == nil {
		t.Error("a verdict with an unknown disposition must fail validation")
	}
}

// TestNewKnowledgeItem_OnlyFromKept proves SC-005: a KnowledgeItem can be
// built ONLY from a kept verdict — a dropped or held verdict cannot surface.
func TestNewKnowledgeItem_OnlyFromKept(t *testing.T) {
	it := IngestItem{SourceRef: "https://example.com/x", Content: "body", Trust: TrustOperatorSeeded}

	kept := KeptVerdict(it, 0.8, 0.8, 0.8, 0.8, "kept")
	ki, err := NewKnowledgeItem(it, kept)
	if err != nil {
		t.Fatalf("a kept verdict must produce a KnowledgeItem: %v", err)
	}
	if ki.SourceRef != it.SourceRef || ki.Rank != 0.8 {
		t.Errorf("KnowledgeItem did not carry the kept item's fields: %+v", ki)
	}

	for _, v := range []Verdict{
		DroppedVerdict(it, 0.2, 0.2, 0.2, 0.2, "low signal"),
		HeldVerdict(it, 0.5, 0.5, 0.5, 0.5, "uncertain"),
	} {
		if _, err := NewKnowledgeItem(it, v); err == nil {
			t.Errorf("a %s verdict must NOT become a KnowledgeItem (SC-005)", v.Disposition)
		}
	}
}
