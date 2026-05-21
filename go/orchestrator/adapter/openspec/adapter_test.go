package openspec

import (
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// fixtureRepo is the OpenSpec fixture tree root.
const fixtureRepo = "../testdata/openspec"

// TestDetect asserts the OpenSpec detector recognizes an `openspec/` repo and
// rejects one without it.
func TestDetect(t *testing.T) {
	a := New()
	if ok, err := a.Detect(fixtureRepo); err != nil || !ok {
		t.Errorf("Detect(openspec fixture) = %v, %v; want true, nil", ok, err)
	}
	if ok, err := a.Detect("../testdata/speckit"); err != nil || ok {
		t.Errorf("Detect(speckit fixture) = %v, %v; want false, nil", ok, err)
	}
}

// TestCompileNodePerDelta asserts one DAG node per ADDED/MODIFIED/REMOVED
// requirement delta (FR-006). The fixture declares three deltas.
func TestCompileNodePerDelta(t *testing.T) {
	d := mustCompile(t, "add-auth")
	if got, want := d.Len(), 3; got != want {
		t.Fatalf("node count = %d, want %d (one node per delta)", got, want)
	}
	if err := d.Acyclic(); err != nil {
		t.Errorf("OpenSpec DAG is not a valid Work-Unit DAG: %v", err)
	}
}

// TestChangeKindPreserved asserts each delta's ADDED/MODIFIED/REMOVED
// change-kind is preserved as node metadata (FR-007). The change-kind rides
// in the node's TaskRef as "<kind>:<area>".
func TestChangeKindPreserved(t *testing.T) {
	d := mustCompile(t, "add-auth")
	kinds := map[string]bool{}
	for _, n := range d.Nodes() {
		kind, _, ok := strings.Cut(n.TaskRef, ":")
		if !ok {
			t.Errorf("node %s TaskRef %q does not encode a change-kind", n.ID, n.TaskRef)
			continue
		}
		kinds[kind] = true
	}
	for _, want := range []string{"ADDED", "MODIFIED", "REMOVED"} {
		if !kinds[want] {
			t.Errorf("change-kind %q not preserved on any node; got %v", want, kinds)
		}
	}
}

// TestChangeKindPriority asserts REMOVED is scheduled before MODIFIED before
// ADDED — a brownfield change tears down before it rebuilds.
func TestChangeKindPriority(t *testing.T) {
	d := mustCompile(t, "add-auth")
	prio := map[string]int{}
	for _, n := range d.Nodes() {
		kind, _, _ := strings.Cut(n.TaskRef, ":")
		prio[kind] = n.Priority
	}
	if !(prio["REMOVED"] > prio["MODIFIED"] && prio["MODIFIED"] > prio["ADDED"]) {
		t.Errorf("change-kind priority order wrong: %v", prio)
	}
}

// TestCompileDeterministic asserts the OpenSpec transform is deterministic.
func TestCompileDeterministic(t *testing.T) {
	first := mustCompile(t, "add-auth")
	for i := 0; i < 10; i++ {
		again := mustCompile(t, "add-auth")
		if first.Len() != again.Len() {
			t.Fatalf("compile %d produced a different DAG", i)
		}
		for j, n := range first.Nodes() {
			if n != again.Nodes()[j] {
				t.Fatalf("compile %d node %d differs — not deterministic", i, j)
			}
		}
	}
}

// mustCompile compiles a fixture change or fails the test.
func mustCompile(t *testing.T, change string) *dag.DAG {
	t.Helper()
	d, err := New().Compile(fixtureRepo, change)
	if err != nil {
		t.Fatalf("Compile(%q): %v", change, err)
	}
	return d
}
