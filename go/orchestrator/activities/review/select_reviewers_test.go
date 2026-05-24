package review

import (
	"context"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// fakeDriver is a minimal AgentDriver for activity-level tests. Mirrors
// the shape used in driver/select_test.go but kept local so this package
// doesn't gain a test dependency on the driver-package's test helpers.
type fakeDriver struct {
	id    string
	card  driver.CapabilityCard
	ready bool
}

func (f *fakeDriver) ID() string                                 { return f.id }
func (f *fakeDriver) Card() driver.CapabilityCard                { return f.card }
func (f *fakeDriver) Ready(context.Context) (bool, string)       { return f.ready, "" }
func (f *fakeDriver) Invoke(context.Context, driver.WorkUnit) (driver.Result, error) {
	return driver.Result{}, nil
}

// newReviewerDriver builds a ready fake driver declaring CapCodeReview
// plus a GitIdentity for exclusion testing.
func newReviewerDriver(id, gitID string) *fakeDriver {
	return &fakeDriver{
		id:    id,
		ready: true,
		card: driver.CapabilityCard{
			DriverID:     id,
			Version:      "0.0.0-test",
			AgentRuntime: "fake",
			Model:        "fake-model",
			Capabilities: []driver.Capability{driver.CapCodeReview},
			Tier:         driver.TierMid,
			CostClass:    driver.CostLow,
			GitIdentity:  gitID,
		},
	}
}

// TestSelectReviewers_HappyPath covers SC-001 / US1 selection. Two
// reviewer-tagged drivers in the pool, a PR author whose identity doesn't
// match either, ArbiterType=operator → slate fills both primaries from
// the deterministic pool order, Arbiter remains empty (operator surface
// handles it), ExcludedAuthor empty (no driver matched).
func TestSelectReviewers_HappyPath(t *testing.T) {
	reg := driver.NewRegistry()
	for _, d := range []driver.AgentDriver{
		newReviewerDriver("hermes", "hermes-bot"),
		newReviewerDriver("openclaw", "clawta-bot"),
	} {
		if err := reg.Register(d); err != nil {
			t.Fatalf("Register(%s): %v", d.ID(), err)
		}
	}
	a := NewSelectReviewers(reg)
	slate, err := a.Execute(context.Background(), SelectReviewersInput{
		PRAuthor: "jpleva91", ArbiterType: ArbiterOperator, PrimariesRequired: 2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if IsShortfall(slate) {
		t.Errorf("slate is shortfall, want filled: %+v", slate)
	}
	if slate.Primary1 != "hermes" || slate.Primary2 != "openclaw" {
		t.Errorf("primaries = (%s, %s), want (hermes, openclaw) by deterministic id order",
			slate.Primary1, slate.Primary2)
	}
	if slate.ExcludedAuthor != "" {
		t.Errorf("ExcludedAuthor = %q, want empty (unmapped PR author)", slate.ExcludedAuthor)
	}
	if slate.Arbiter != "" {
		t.Errorf("Arbiter = %q, want empty for ArbiterOperator", slate.Arbiter)
	}
}

// TestSelectReviewers_AuthorExcluded covers FR-005 + Acceptance
// Scenario 4.1 — a PR authored by a reviewer-tagged driver excludes
// that driver from selection.
func TestSelectReviewers_AuthorExcluded(t *testing.T) {
	reg := driver.NewRegistry()
	for _, d := range []driver.AgentDriver{
		newReviewerDriver("hermes", "hermes-bot"),
		newReviewerDriver("openclaw", "clawta-bot"),
		newReviewerDriver("codex", "codex-bot"),
	} {
		if err := reg.Register(d); err != nil {
			t.Fatalf("Register(%s): %v", d.ID(), err)
		}
	}
	a := NewSelectReviewers(reg)
	slate, err := a.Execute(context.Background(), SelectReviewersInput{
		PRAuthor: "hermes-bot", ArbiterType: ArbiterOperator, PrimariesRequired: 2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if slate.ExcludedAuthor != "hermes" {
		t.Errorf("ExcludedAuthor = %q, want %q", slate.ExcludedAuthor, "hermes")
	}
	// With hermes excluded, the deterministic pool order is codex < openclaw
	// (lexical), so primaries are (codex, openclaw).
	if slate.Primary1 != "codex" || slate.Primary2 != "openclaw" {
		t.Errorf("primaries = (%s, %s), want (codex, openclaw) after excluding hermes",
			slate.Primary1, slate.Primary2)
	}
	if got := len(slate.EligibleAfterExclusion); got != 2 {
		t.Errorf("len(EligibleAfterExclusion) = %d, want 2 (hermes filtered)", got)
	}
}

// TestSelectReviewers_Shortfall covers FR-007 + Acceptance Scenario 4.2.
// v1 pool of 2 + author is one of them → 1 eligible after exclusion <
// 2 required → shortfall slate (Primary1 empty as state flag).
func TestSelectReviewers_Shortfall(t *testing.T) {
	reg := driver.NewRegistry()
	for _, d := range []driver.AgentDriver{
		newReviewerDriver("hermes", "hermes-bot"),
		newReviewerDriver("openclaw", "clawta-bot"),
	} {
		if err := reg.Register(d); err != nil {
			t.Fatalf("Register(%s): %v", d.ID(), err)
		}
	}
	a := NewSelectReviewers(reg)
	slate, err := a.Execute(context.Background(), SelectReviewersInput{
		PRAuthor: "hermes-bot", ArbiterType: ArbiterOperator, PrimariesRequired: 2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !IsShortfall(slate) {
		t.Errorf("expected shortfall slate, got %+v", slate)
	}
	if slate.ExcludedAuthor != "hermes" {
		t.Errorf("ExcludedAuthor = %q, want %q", slate.ExcludedAuthor, "hermes")
	}
	reason := ShortfallReason(slate, 2)
	if reason == "" {
		t.Error("ShortfallReason returned empty string")
	}
}

// TestSelectReviewers_MachineArbiterFromThirdDriver covers SC-003 /
// US2 dispatch path — ArbiterType=machine with three eligible drivers
// fills the arbiter slot from the third entry.
func TestSelectReviewers_MachineArbiterFromThirdDriver(t *testing.T) {
	reg := driver.NewRegistry()
	for _, d := range []driver.AgentDriver{
		newReviewerDriver("codex", "codex-bot"),
		newReviewerDriver("hermes", "hermes-bot"),
		newReviewerDriver("openclaw", "clawta-bot"),
	} {
		if err := reg.Register(d); err != nil {
			t.Fatalf("Register(%s): %v", d.ID(), err)
		}
	}
	a := NewSelectReviewers(reg)
	slate, err := a.Execute(context.Background(), SelectReviewersInput{
		PRAuthor: "jpleva91", ArbiterType: ArbiterMachine, PrimariesRequired: 2,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Pool order is lexical: codex < hermes < openclaw.
	if slate.Primary1 != "codex" || slate.Primary2 != "hermes" || slate.Arbiter != "openclaw" {
		t.Errorf("slate = (%s, %s, arb=%s), want (codex, hermes, openclaw)",
			slate.Primary1, slate.Primary2, slate.Arbiter)
	}
}

// TestSelectReviewers_NoBoundRegistry covers the configuration-fault
// path — Execute with a nil registry returns an activity-level error.
func TestSelectReviewers_NoBoundRegistry(t *testing.T) {
	a := &SelectReviewers{} // no registry bound
	_, err := a.Execute(context.Background(), SelectReviewersInput{PrimariesRequired: 2})
	if err == nil {
		t.Fatal("Execute on unbound activity must return an error")
	}
}

// TestSelectReviewers_BadPrimariesRequired covers input validation —
// PrimariesRequired < 2 is a programming error, surfaced as an
// activity-level error rather than a silent zero-slot slate.
func TestSelectReviewers_BadPrimariesRequired(t *testing.T) {
	a := NewSelectReviewers(driver.NewRegistry())
	_, err := a.Execute(context.Background(), SelectReviewersInput{PrimariesRequired: 1})
	if err == nil {
		t.Fatal("Execute with PrimariesRequired=1 must return an error")
	}
}
