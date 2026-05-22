package driver

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRegister_RejectsUnknownCapability proves FR-015: a card carrying a
// tag outside the closed taxonomy is rejected at registration, and the
// error names the offending tag.
func TestRegister_RejectsUnknownCapability(t *testing.T) {
	r := NewRegistry()
	bad := &fakeDriver{
		id:    "bogus",
		ready: true,
		card: CapabilityCard{
			DriverID:     "bogus",
			AgentRuntime: "fake",
			Capabilities: []Capability{CapCodeImplement, "code.teleport"},
			Tier:         TierMid,
			CostClass:    CostLow,
		},
	}

	err := r.Register(bad)
	if err == nil {
		t.Fatal("Register: want rejection of an unknown capability, got nil")
	}
	if !strings.Contains(err.Error(), "code.teleport") {
		t.Errorf("Register error must name the offending tag; got %q", err.Error())
	}
	if r.Len() != 0 {
		t.Errorf("Register: registry must be unchanged after rejection, Len()=%d", r.Len())
	}
}

// TestRegister_AcceptsKnownCapabilities proves a card whose every tag is in
// the taxonomy registers cleanly.
func TestRegister_AcceptsKnownCapabilities(t *testing.T) {
	r := NewRegistry()
	d := newFake("good", TierMid, CostLow, CapCodeImplement, CapCodeReview)
	if err := r.Register(d); err != nil {
		t.Fatalf("Register: unexpected rejection of a valid driver: %v", err)
	}
	if r.Len() != 1 {
		t.Errorf("Register: want Len()=1 after one registration, got %d", r.Len())
	}
	if got, ok := r.Driver("good"); !ok || got.ID() != "good" {
		t.Errorf("Driver(%q): want the registered driver, got ok=%v", "good", ok)
	}
}

// TestRegister_RejectsBadInputs covers the rest of the registration
// contract: nil driver, empty id, id/card mismatch, duplicate id, and
// invalid enum values.
func TestRegister_RejectsBadInputs(t *testing.T) {
	t.Run("nil driver", func(t *testing.T) {
		if err := NewRegistry().Register(nil); err == nil {
			t.Error("Register(nil): want an error, got nil")
		}
	})

	t.Run("empty id", func(t *testing.T) {
		d := newFake("", TierMid, CostLow)
		if err := NewRegistry().Register(d); err == nil {
			t.Error("Register: want rejection of an empty id, got nil")
		}
	})

	t.Run("id does not match card DriverID", func(t *testing.T) {
		d := newFake("real-id", TierMid, CostLow)
		d.card.DriverID = "other-id"
		if err := NewRegistry().Register(d); err == nil {
			t.Error("Register: want rejection of an id/card mismatch, got nil")
		}
	})

	t.Run("duplicate id", func(t *testing.T) {
		r := NewRegistry()
		if err := r.Register(newFake("dup", TierMid, CostLow)); err != nil {
			t.Fatalf("first Register: unexpected error: %v", err)
		}
		if err := r.Register(newFake("dup", TierFrontier, CostHigh)); err == nil {
			t.Error("Register: want rejection of a duplicate id, got nil")
		}
		if r.Len() != 1 {
			t.Errorf("Register: duplicate must not be admitted, Len()=%d", r.Len())
		}
	})

	t.Run("invalid tier", func(t *testing.T) {
		d := newFake("badtier", Tier(99), CostLow)
		if err := NewRegistry().Register(d); err == nil {
			t.Error("Register: want rejection of an invalid tier, got nil")
		}
	})

	t.Run("invalid cost class", func(t *testing.T) {
		d := newFake("badcost", TierMid, CostClass(99))
		if err := NewRegistry().Register(d); err == nil {
			t.Error("Register: want rejection of an invalid cost class, got nil")
		}
	})
}

// TestDriversFor_FiltersByCapabilityAndReadiness proves FR-004 + FR-008:
// DriversFor returns exactly the drivers whose card declares the capability
// AND that report ready — a not-ready driver is omitted so the scheduler
// routes elsewhere.
func TestDriversFor_FiltersByCapabilityAndReadiness(t *testing.T) {
	ctx := context.Background()
	r := NewRegistry()

	// implements + ready
	dImpl := newFake("impl-ready", TierMid, CostLow, CapCodeImplement)
	// implements but NOT ready (agent down / quota exhausted)
	dDown := newFake("impl-down", TierFrontier, CostHigh, CapCodeImplement)
	dDown.ready = false
	dDown.reason = "agent process down"
	// reviews + ready — distinct capability
	dReview := newFake("review-ready", TierMid, CostLow, CapCodeReview)
	// implements AND reviews + ready — overlapping capability
	dBoth := newFake("both-ready", TierLocal, CostFree, CapCodeImplement, CapCodeReview)

	for _, d := range []AgentDriver{dImpl, dDown, dReview, dBoth} {
		if err := r.Register(d); err != nil {
			t.Fatalf("Register(%s): %v", d.ID(), err)
		}
	}

	gotImpl := ids(r.DriversFor(ctx, CapCodeImplement))
	wantImpl := []string{"both-ready", "impl-ready"} // id-ordered; impl-down omitted (not ready)
	if !equalStrings(gotImpl, wantImpl) {
		t.Errorf("DriversFor(code.implement) = %v, want %v", gotImpl, wantImpl)
	}

	gotReview := ids(r.DriversFor(ctx, CapCodeReview))
	wantReview := []string{"both-ready", "review-ready"}
	if !equalStrings(gotReview, wantReview) {
		t.Errorf("DriversFor(code.review) = %v, want %v", gotReview, wantReview)
	}

	// A capability no card declares yields an empty slice, not an error.
	if got := r.DriversFor(ctx, CapResearchX); len(got) != 0 {
		t.Errorf("DriversFor(research.x) = %v, want empty", ids(got))
	}
}

// TestSelect_DeterministicWithReason proves FR-004 + FR-005: Select routes
// a capability requirement to a ready, matching driver deterministically,
// and returns a non-empty selection reason for the audit record.
func TestSelect_DeterministicWithReason(t *testing.T) {
	ctx := context.Background()
	r := NewRegistry()

	// Three drivers satisfy code.implement; frontier+low is the winner.
	for _, d := range []AgentDriver{
		newFake("frontier-low", TierFrontier, CostLow, CapCodeImplement),
		newFake("frontier-high", TierFrontier, CostHigh, CapCodeImplement),
		newFake("mid-free", TierMid, CostFree, CapCodeImplement),
	} {
		if err := r.Register(d); err != nil {
			t.Fatalf("Register(%s): %v", d.ID(), err)
		}
	}

	chosen, reason, err := r.Select(ctx, CapCodeImplement)
	if err != nil {
		t.Fatalf("Select: unexpected error: %v", err)
	}
	if chosen.ID() != "frontier-low" {
		t.Errorf("Select chose %q, want %q", chosen.ID(), "frontier-low")
	}
	if reason == "" {
		t.Error("Select returned an empty reason; the chosen driver's reason must be recorded")
	}

	// Determinism: 100 repeated Selects yield the identical driver + reason.
	for i := 0; i < 100; i++ {
		d, rr, err := r.Select(ctx, CapCodeImplement)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if d.ID() != chosen.ID() || rr != reason {
			t.Fatalf("iteration %d: Select drifted — driver %q reason %q, want %q / %q",
				i, d.ID(), rr, chosen.ID(), reason)
		}
	}
}

// TestSelect_BlockedUnroutable proves FR-012: when no registered, ready
// driver satisfies the required capability, Select returns a
// *BlockedUnroutableError naming the missing capability — never a nil
// driver silently or an arbitrary assignment.
func TestSelect_BlockedUnroutable(t *testing.T) {
	ctx := context.Background()

	t.Run("no driver declares the capability", func(t *testing.T) {
		r := NewRegistry()
		if err := r.Register(newFake("impl", TierMid, CostLow, CapCodeImplement)); err != nil {
			t.Fatalf("Register: %v", err)
		}

		chosen, _, err := r.Select(ctx, CapResearchWeb)
		if chosen != nil {
			t.Errorf("Select: want a nil driver on blocked-unroutable, got %q", chosen.ID())
		}
		var bu *BlockedUnroutableError
		if !errors.As(err, &bu) {
			t.Fatalf("Select: want *BlockedUnroutableError, got %T (%v)", err, err)
		}
		if bu.Capability != CapResearchWeb {
			t.Errorf("BlockedUnroutableError names %q, want %q", bu.Capability, CapResearchWeb)
		}
		if !strings.Contains(bu.Error(), string(CapResearchWeb)) {
			t.Errorf("BlockedUnroutableError message must name the missing capability; got %q", bu.Error())
		}
	})

	t.Run("the only matching driver is not ready", func(t *testing.T) {
		r := NewRegistry()
		down := newFake("research-down", TierFrontier, CostHigh, CapResearchWeb)
		down.ready = false
		if err := r.Register(down); err != nil {
			t.Fatalf("Register: %v", err)
		}

		_, _, err := r.Select(ctx, CapResearchWeb)
		var bu *BlockedUnroutableError
		if !errors.As(err, &bu) {
			t.Fatalf("Select: a capability with only an unready driver must be blocked-unroutable; got %T (%v)", err, err)
		}
		if bu.Capability != CapResearchWeb {
			t.Errorf("BlockedUnroutableError names %q, want %q", bu.Capability, CapResearchWeb)
		}
	})
}

// ids extracts driver ids from a slice, preserving order.
func ids(ds []AgentDriver) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.ID()
	}
	return out
}

// equalStrings reports whether two string slices are equal element-wise.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
