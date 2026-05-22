package driver

import (
	"context"
	"testing"
)

// fakeDriver is a minimal AgentDriver for tests: a fixed card, a fixed
// readiness, and an Invoke that returns a fixed Result. It carries no
// mutable state, so it is safe to share across table cases.
type fakeDriver struct {
	id     string
	card   CapabilityCard
	ready  bool
	reason string
	result Result
	err    error
}

func (f *fakeDriver) ID() string           { return f.id }
func (f *fakeDriver) Card() CapabilityCard { return f.card }
func (f *fakeDriver) Ready(context.Context) (bool, string) {
	return f.ready, f.reason
}
func (f *fakeDriver) Invoke(context.Context, WorkUnit) (Result, error) {
	return f.result, f.err
}

// newFake builds a ready fakeDriver whose card carries the given id, tier,
// cost class, and capabilities.
func newFake(id string, tier Tier, cost CostClass, caps ...Capability) *fakeDriver {
	return &fakeDriver{
		id:    id,
		ready: true,
		card: CapabilityCard{
			DriverID:     id,
			Version:      "0.0.0-test",
			AgentRuntime: "fake",
			Model:        "fake-model",
			Capabilities: caps,
			Tier:         tier,
			CostClass:    cost,
		},
	}
}

// TestSelectDriver_TierThenCostThenID proves the three-key total order:
// Tier first, then CostClass, then the lexical driver-id tie-breaker.
func TestSelectDriver_TierThenCostThenID(t *testing.T) {
	tests := []struct {
		name       string
		candidates []AgentDriver
		wantID     string
	}{
		{
			name: "tier wins over cost — frontier+high beats mid+free",
			candidates: []AgentDriver{
				newFake("mid-cheap", TierMid, CostFree),
				newFake("frontier-dear", TierFrontier, CostHigh),
			},
			wantID: "frontier-dear",
		},
		{
			name: "cost breaks a tier tie — cheaper wins",
			candidates: []AgentDriver{
				newFake("frontier-dear", TierFrontier, CostHigh),
				newFake("frontier-cheap", TierFrontier, CostLow),
			},
			wantID: "frontier-cheap",
		},
		{
			name: "id breaks a tier+cost tie — lexically smaller wins",
			candidates: []AgentDriver{
				newFake("zeta", TierMid, CostMedium),
				newFake("alpha", TierMid, CostMedium),
				newFake("mu", TierMid, CostMedium),
			},
			wantID: "alpha",
		},
		{
			name: "local tier sorts last",
			candidates: []AgentDriver{
				newFake("local-free", TierLocal, CostFree),
				newFake("mid-high", TierMid, CostHigh),
			},
			wantID: "mid-high",
		},
		{
			name:       "single candidate is selected",
			candidates: []AgentDriver{newFake("only", TierLocal, CostHigh)},
			wantID:     "only",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reason, err := selectDriver(tc.candidates)
			if err != nil {
				t.Fatalf("selectDriver: unexpected error: %v", err)
			}
			if got.ID() != tc.wantID {
				t.Errorf("selectDriver chose %q, want %q", got.ID(), tc.wantID)
			}
			if reason == "" {
				t.Error("selectDriver returned an empty reason")
			}
		})
	}
}

// TestSelectDriver_Empty proves selecting from no candidates is an error,
// not a panic or a nil driver silently returned.
func TestSelectDriver_Empty(t *testing.T) {
	got, _, err := selectDriver(nil)
	if err == nil {
		t.Fatal("selectDriver(nil): want an error, got nil")
	}
	if got != nil {
		t.Errorf("selectDriver(nil): want a nil driver, got %q", got.ID())
	}
}

// TestSelectDriver_DeterministicAcross100 proves SC-003: given a fixed set
// of candidates, selection is identical across 100 repeated evaluations —
// same driver, same reason — and independent of input ordering.
func TestSelectDriver_DeterministicAcross100(t *testing.T) {
	// A fixed candidate set with deliberate ties at every key:
	// two frontier drivers (one of which ties on cost) and noise below.
	base := []AgentDriver{
		newFake("frontier-bravo", TierFrontier, CostMedium, CapCodeImplement),
		newFake("frontier-alpha", TierFrontier, CostMedium, CapCodeImplement),
		newFake("frontier-cheap", TierFrontier, CostLow, CapCodeImplement),
		newFake("mid-alpha", TierMid, CostFree, CapCodeImplement),
		newFake("local-alpha", TierLocal, CostFree, CapCodeImplement),
	}
	// frontier-cheap: TierFrontier + CostLow — the unique winner.
	const wantID = "frontier-cheap"

	gotID, gotReason, err := selectDriver(base)
	if err != nil {
		t.Fatalf("selectDriver: unexpected error: %v", err)
	}
	if gotID.ID() != wantID {
		t.Fatalf("selectDriver chose %q, want %q", gotID.ID(), wantID)
	}

	for i := 0; i < 100; i++ {
		// Rotate the input slice so no run sees the same order; a
		// determinism bug that depended on input order would surface here.
		rotated := rotate(base, i%len(base))
		d, reason, err := selectDriver(rotated)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		if d.ID() != wantID {
			t.Fatalf("iteration %d: chose %q, want %q (input order must not matter)", i, d.ID(), wantID)
		}
		if reason != gotReason {
			t.Fatalf("iteration %d: reason drift\n got: %q\nwant: %q", i, reason, gotReason)
		}
	}
}

// TestSelectDriver_DoesNotMutateInput proves selectDriver is pure with
// respect to its argument — it sorts a copy, leaving the caller's slice
// order untouched.
func TestSelectDriver_DoesNotMutateInput(t *testing.T) {
	in := []AgentDriver{
		newFake("zeta", TierMid, CostMedium),
		newFake("alpha", TierMid, CostMedium),
	}
	before := []string{in[0].ID(), in[1].ID()}

	if _, _, err := selectDriver(in); err != nil {
		t.Fatalf("selectDriver: unexpected error: %v", err)
	}
	if in[0].ID() != before[0] || in[1].ID() != before[1] {
		t.Errorf("selectDriver mutated its input slice: before %v, after %v",
			before, []string{in[0].ID(), in[1].ID()})
	}
}

// rotate returns a new slice that is s rotated left by n positions.
func rotate(s []AgentDriver, n int) []AgentDriver {
	if len(s) == 0 {
		return nil
	}
	n %= len(s)
	out := make([]AgentDriver, 0, len(s))
	out = append(out, s[n:]...)
	out = append(out, s[:n]...)
	return out
}
