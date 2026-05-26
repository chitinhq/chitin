package codex

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/internal/blob"
)

func TestCardDeclaresCodexContract(t *testing.T) {
	d := New()
	card := d.Card()

	if d.ID() != "codex" {
		t.Fatalf("ID() = %q, want codex", d.ID())
	}
	if card.DriverID != d.ID() {
		t.Fatalf("card DriverID = %q, want %q", card.DriverID, d.ID())
	}
	if card.Tier != driver.TierFrontier {
		t.Fatalf("tier = %s, want frontier", card.Tier)
	}
	// Spec 105 FR-001: codex declares CapTestAuthor (test-authoring is
	// in scope for a frontier code model).
	for _, cap := range []driver.Capability{driver.CapCodeImplement, driver.CapCodeReview, driver.CapCodeRefactor, driver.CapTestAuthor} {
		if !card.HasCapability(cap) {
			t.Fatalf("card missing capability %q", cap)
		}
	}
	if !card.Constraints.WorktreeRequired || !card.Constraints.NetworkRequired || !card.Constraints.QuotaBounded {
		t.Fatalf("constraints = %+v, want governed worktree, network, quota-bounded", card.Constraints)
	}
}

func TestReadyReportsUnavailableRuntime(t *testing.T) {
	d := New(WithCommand("definitely-not-a-real-codex-binary"))

	ready, reason := d.Ready(context.Background())
	if ready {
		t.Fatal("Ready() = true for a missing runtime, want false")
	}
	if !strings.Contains(reason, "not found") {
		t.Fatalf("Ready() reason = %q, want it to explain the runtime was not found", reason)
	}
}

func TestResultFromCommandExternalizesLargeOutput(t *testing.T) {
	ctx := context.Background()
	store := blob.NewFSStore(blob.WithDir(t.TempDir()), blob.WithEmitter(nil))
	stdout := strings.Repeat("x", 2_621_440)

	res := resultFromCommand(ctx, store, driver.WorkUnit{ID: "large"}, "codex", stdout, "", nil)
	if !blob.IsRef(res.OutputRef) {
		t.Fatalf("OutputRef = %q, want blob ref", res.OutputRef)
	}
	body, err := blob.Resolve(ctx, store, res.OutputRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(body) != stdout {
		t.Fatal("resolved output did not match original stdout")
	}
}

func TestResultFromCommandKeepsSmallOutputInline(t *testing.T) {
	ctx := context.Background()
	blobDir := filepath.Join(t.TempDir(), "blobs")
	store := blob.NewFSStore(blob.WithDir(blobDir), blob.WithEmitter(nil))
	stdout := strings.Repeat("s", 4096)

	res := resultFromCommand(ctx, store, driver.WorkUnit{ID: "small"}, "codex", stdout, "", nil)
	if blob.IsRef(res.OutputRef) {
		t.Fatalf("OutputRef = %q, want inline", res.OutputRef)
	}
	if res.OutputRef != stdout {
		t.Fatal("inline output did not match stdout")
	}
}
