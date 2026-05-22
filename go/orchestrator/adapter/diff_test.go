package adapter_test

import (
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/adapter/speckit"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// TestDiffBeforeAfter compiles a known before/after spec pair and asserts the
// DAG diff reports exactly the added / removed / changed nodes (FR-012,
// SC-006). The fixture's before tree has T001,T002,T003; the after tree has
// T001 (unchanged), T002 (changed — capability flips implement→test.author),
// and T004 (added); T003 is removed.
func TestDiffBeforeAfter(t *testing.T) {
	a := speckit.New()
	before, err := a.Compile("testdata/diff/before", "400")
	if err != nil {
		t.Fatalf("compile before: %v", err)
	}
	after, err := a.Compile("testdata/diff/after", "400")
	if err != nil {
		t.Fatalf("compile after: %v", err)
	}

	diff := adapter.Diff(before, after)

	if got := nodeIDs(diff.Added); !idsEqual(got, []string{"400-evolving/T004"}) {
		t.Errorf("Added = %v, want [400-evolving/T004]", got)
	}
	if got := nodeIDs(diff.Removed); !idsEqual(got, []string{"400-evolving/T003"}) {
		t.Errorf("Removed = %v, want [400-evolving/T003]", got)
	}
	if len(diff.Changed) != 1 || diff.Changed[0].ID != "400-evolving/T002" {
		t.Fatalf("Changed = %v, want [400-evolving/T002]", changeIDs(diff.Changed))
	}
	// The change is a real capability flip — implement → test.author.
	ch := diff.Changed[0]
	if ch.Old.Capability == ch.New.Capability {
		t.Errorf("T002 capability did not change: %q", ch.Old.Capability)
	}
}

// TestDiffFirstCompilation asserts that diffing against a nil prior DAG —
// the very first compilation of a spec — reports every node as added.
func TestDiffFirstCompilation(t *testing.T) {
	fresh, err := speckit.New().Compile("testdata/diff/after", "400")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	diff := adapter.Diff(nil, fresh)
	if len(diff.Added) != fresh.Len() {
		t.Errorf("first compilation: Added = %d, want all %d nodes",
			len(diff.Added), fresh.Len())
	}
	if len(diff.Removed) != 0 || len(diff.Changed) != 0 {
		t.Error("first compilation should report nothing removed or changed")
	}
}

// TestDiffIdenticalIsEmpty asserts that recompiling an unchanged spec yields
// an empty diff — no spurious wholesale replacement (FR-012).
func TestDiffIdenticalIsEmpty(t *testing.T) {
	a := speckit.New()
	first, _ := a.Compile("testdata/diff/before", "400")
	second, _ := a.Compile("testdata/diff/before", "400")
	if diff := adapter.Diff(first, second); !diff.Empty() {
		t.Errorf("diff of identical compilations is not empty: %+v", diff)
	}
}

// TestDiffIgnoresStatus asserts a node that merely advanced through its
// lifecycle is not flagged as a spec change — Diff compares compiled content,
// not runtime status (FR-012).
func TestDiffIgnoresStatus(t *testing.T) {
	a := speckit.New()
	prior, _ := a.Compile("testdata/diff/before", "400")
	fresh, _ := a.Compile("testdata/diff/before", "400")
	// Simulate the scheduler having advanced a node on the prior DAG.
	if err := prior.SetStatus("400-evolving/T001", dag.StatusRunning); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if diff := adapter.Diff(prior, fresh); !diff.Empty() {
		t.Errorf("a status-only change must not register as a spec change: %+v", diff)
	}
}

// nodeIDs extracts the sorted IDs of a node slice.
func nodeIDs(ns []dag.Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.ID
	}
	return out
}

// changeIDs extracts the IDs of a NodeChange slice for error messages.
func changeIDs(cs []adapter.NodeChange) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

// idsEqual reports whether two id slices hold the same elements in order.
func idsEqual(a, b []string) bool {
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
