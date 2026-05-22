package copilot

import (
	"context"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

func TestCardDeclaresCopilotContract(t *testing.T) {
	d := New()
	card := d.Card()

	if d.ID() != "copilot" {
		t.Fatalf("ID() = %q, want copilot", d.ID())
	}
	if card.DriverID != d.ID() {
		t.Fatalf("card DriverID = %q, want %q", card.DriverID, d.ID())
	}
	if card.Tier != driver.TierFrontier {
		t.Fatalf("tier = %s, want frontier", card.Tier)
	}
	for _, cap := range []driver.Capability{driver.CapCodeImplement, driver.CapCodeReview} {
		if !card.HasCapability(cap) {
			t.Fatalf("card missing capability %q", cap)
		}
		if !driver.IsKnownCapability(string(cap)) {
			t.Fatalf("capability %q is outside the closed taxonomy", cap)
		}
	}
	if !card.Constraints.WorktreeRequired || !card.Constraints.NetworkRequired {
		t.Fatalf("constraints = %+v, want governed worktree + network", card.Constraints)
	}
}

func TestReadyReportsUnavailableRuntime(t *testing.T) {
	d := New(WithCommand("definitely-not-a-real-copilot-binary"))

	ready, reason := d.Ready(context.Background())
	if ready {
		t.Fatal("Ready() = true for a missing runtime, want false")
	}
	if !strings.Contains(reason, "not found") {
		t.Fatalf("Ready() reason = %q, want it to explain the runtime was not found", reason)
	}
}
