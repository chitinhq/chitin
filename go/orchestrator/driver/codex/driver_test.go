package codex

import (
	"bytes"
	"context"
	"fmt"
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
	store, err := blob.NewFSStore(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("x"), 2_621_440)
	res, err := resultFromCommand(context.Background(), store, driver.WorkUnit{ID: "wu-large"}, "codex", string(body), "", nil)
	if err != nil {
		t.Fatalf("resultFromCommand: %v", err)
	}
	if !blob.IsRef(res.OutputRef) {
		t.Fatalf("OutputRef = %q, want blob ref", res.OutputRef)
	}
	got, err := blob.Resolve(context.Background(), store, res.OutputRef)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("resolved body differed from stdout")
	}
}

func TestResultFromCommandLeavesSmallOutputInlineWithoutStore(t *testing.T) {
	body := strings.Repeat("s", 4096)
	res, err := resultFromCommand(context.Background(), nil, driver.WorkUnit{ID: "wu-small"}, "codex", body, "", nil)
	if err != nil {
		t.Fatalf("resultFromCommand: %v", err)
	}
	if res.OutputRef != body {
		t.Fatalf("OutputRef = %q, want inline body", res.OutputRef)
	}
}

func TestResultFromCommandExternalizesLargeExplanation(t *testing.T) {
	store, err := blob.NewFSStore(filepath.Join(t.TempDir(), "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	stderr := strings.Repeat("e", blob.InlineThreshold+1)
	res, err := resultFromCommand(context.Background(), store, driver.WorkUnit{ID: "wu-fail"}, "codex", "short", stderr, fmt.Errorf("boom"))
	if err != nil {
		t.Fatalf("resultFromCommand: %v", err)
	}
	if res.OutputRef != "short" {
		t.Fatalf("OutputRef = %q, want short inline output", res.OutputRef)
	}
	if !blob.IsRef(res.Explanation) {
		t.Fatalf("Explanation = %q, want blob ref", res.Explanation)
	}
	got, err := blob.Resolve(context.Background(), store, res.Explanation)
	if err != nil {
		t.Fatalf("Resolve explanation: %v", err)
	}
	if !strings.Contains(string(got), stderr) {
		t.Fatal("resolved explanation does not contain stderr")
	}
}
