package dag

import (
	"errors"
	"strings"
	"testing"
)

// build constructs a DAG from a list of node IDs and a list of (from, to)
// edges, failing the test on any constructor error. It is the table-test
// fixture builder for acyclic and frontier tests.
func build(t *testing.T, ids []string, edges [][2]string) *DAG {
	t.Helper()
	d := New()
	for _, id := range ids {
		if err := d.AddNode(node(id, 0, StatusPending)); err != nil {
			t.Fatalf("build: AddNode(%q): %v", id, err)
		}
	}
	for _, e := range edges {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatalf("build: AddEdge(%q,%q): %v", e[0], e[1], err)
		}
	}
	return d
}

// TestAcyclic_WellFormed proves Acyclic returns nil for graphs that are both
// dangle-free and acyclic — across the structural boundaries.
func TestAcyclic_WellFormed(t *testing.T) {
	tests := []struct {
		name  string
		ids   []string
		edges [][2]string
	}{
		{
			name: "empty DAG — zero nodes",
			ids:  nil,
		},
		{
			name: "single node, no edges",
			ids:  []string{"a"},
		},
		{
			name:  "single edge chain a<-b",
			ids:   []string{"a", "b"},
			edges: [][2]string{{"b", "a"}},
		},
		{
			name:  "diamond — d depends on b and c, both depend on a",
			ids:   []string{"a", "b", "c", "d"},
			edges: [][2]string{{"b", "a"}, {"c", "a"}, {"d", "b"}, {"d", "c"}},
		},
		{
			name: "disjoint components",
			ids:  []string{"a", "b", "x", "y"},
			edges: [][2]string{
				{"b", "a"}, // component 1
				{"y", "x"}, // component 2
			},
		},
		{
			name:  "wide fan-out and fan-in",
			ids:   []string{"root", "l1", "l2", "l3", "sink"},
			edges: [][2]string{{"l1", "root"}, {"l2", "root"}, {"l3", "root"}, {"sink", "l1"}, {"sink", "l2"}, {"sink", "l3"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := build(t, tc.ids, tc.edges)
			if err := d.Acyclic(); err != nil {
				t.Errorf("Acyclic() = %v, want nil", err)
			}
		})
	}
}

// TestAcyclic_NamesCycle proves a cyclic graph is rejected AND the error
// names the node IDs forming the cycle (spec 076 FR-002, SC-003).
func TestAcyclic_NamesCycle(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		// edges form a cycle; AddEdge forbids the a->a self-loop so the
		// shortest cycle here is two nodes.
		edges [][2]string
		// wantMembers are the node IDs that MUST all appear in the error.
		wantMembers []string
	}{
		{
			name:        "two-node cycle a<->b",
			ids:         []string{"a", "b"},
			edges:       [][2]string{{"a", "b"}, {"b", "a"}},
			wantMembers: []string{"a", "b"},
		},
		{
			name:        "three-node cycle a->b->c->a",
			ids:         []string{"a", "b", "c"},
			edges:       [][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}},
			wantMembers: []string{"a", "b", "c"},
		},
		{
			name: "cycle buried among acyclic nodes",
			ids:  []string{"root", "x", "y", "z", "leaf"},
			edges: [][2]string{
				{"x", "root"},
				{"x", "y"}, {"y", "z"}, {"z", "x"}, // the cycle
				{"leaf", "x"},
			},
			wantMembers: []string{"x", "y", "z"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := build(t, tc.ids, tc.edges)
			err := d.Acyclic()
			if err == nil {
				t.Fatal("Acyclic() = nil, want a cycle error")
			}

			// The error must be a *cycleError and must name every member.
			var ce *cycleError
			if !errors.As(err, &ce) {
				t.Fatalf("Acyclic() returned %T, want *cycleError", err)
			}
			for _, m := range tc.wantMembers {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("cycle error %q does not name member %q", err.Error(), m)
				}
			}
			// The named cycle must be exactly the offending members (no
			// acyclic noise dragged in).
			if len(ce.Cycle) != len(tc.wantMembers) {
				t.Errorf("cycle has %d members %v, want %d %v",
					len(ce.Cycle), ce.Cycle, len(tc.wantMembers), tc.wantMembers)
			}
			cset := map[string]bool{}
			for _, m := range ce.Cycle {
				cset[m] = true
			}
			for _, m := range tc.wantMembers {
				if !cset[m] {
					t.Errorf("cycle %v is missing expected member %q", ce.Cycle, m)
				}
			}
		})
	}
}

// TestAcyclic_CycleNamingIsDeterministic proves the named cycle is identical
// across 100 detections of the same graph — canonicalized to start at the
// lexically smallest member, with no dependence on map iteration order
// (spec 076 SC-001, SC-003).
func TestAcyclic_CycleNamingIsDeterministic(t *testing.T) {
	ids := []string{"c", "a", "b"} // deliberately not pre-sorted
	edges := [][2]string{{"a", "b"}, {"b", "c"}, {"c", "a"}}

	var first string
	for i := 0; i < 100; i++ {
		d := build(t, ids, edges)
		err := d.Acyclic()
		if err == nil {
			t.Fatalf("iteration %d: Acyclic() = nil, want a cycle error", i)
		}
		if i == 0 {
			first = err.Error()
			continue
		}
		if err.Error() != first {
			t.Fatalf("iteration %d: cycle error drifted\n got: %q\nwant: %q",
				i, err.Error(), first)
		}
	}

	// The canonical form starts at the smallest member, "a".
	want := "dag: cycle detected: a -> b -> c -> a"
	if first != want {
		t.Errorf("cycle error = %q, want canonical form %q", first, want)
	}
}

// TestAcyclic_DanglingEdge proves an edge whose endpoint names a missing
// node is rejected, and the error names the missing node (spec 076 FR-002).
func TestAcyclic_DanglingEdge(t *testing.T) {
	tests := []struct {
		name        string
		ids         []string
		edges       [][2]string
		wantMissing string
	}{
		{
			name:        "dangling To endpoint",
			ids:         []string{"a"},
			edges:       [][2]string{{"a", "ghost"}},
			wantMissing: "ghost",
		},
		{
			name:        "dangling From endpoint",
			ids:         []string{"a"},
			edges:       [][2]string{{"phantom", "a"}},
			wantMissing: "phantom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := build(t, tc.ids, tc.edges)
			err := d.Acyclic()
			if err == nil {
				t.Fatal("Acyclic() = nil, want a dangling-edge error")
			}
			var de *danglingEdgeError
			if !errors.As(err, &de) {
				t.Fatalf("Acyclic() returned %T, want *danglingEdgeError", err)
			}
			if de.Missing != tc.wantMissing {
				t.Errorf("dangling edge missing node = %q, want %q", de.Missing, tc.wantMissing)
			}
			if !strings.Contains(err.Error(), tc.wantMissing) {
				t.Errorf("error %q does not name missing node %q", err.Error(), tc.wantMissing)
			}
		})
	}
}

// TestAcyclic_DanglingCheckedBeforeCycle proves the dangling-edge check runs
// before the cycle check: a graph with both faults reports the dangling edge,
// deterministically.
func TestAcyclic_DanglingCheckedBeforeCycle(t *testing.T) {
	d := New()
	// a<->b is a cycle; a also depends on a missing node "ghost".
	for _, id := range []string{"a", "b"} {
		if err := d.AddNode(node(id, 0, StatusPending)); err != nil {
			t.Fatalf("AddNode(%q): %v", id, err)
		}
	}
	for _, e := range [][2]string{{"a", "b"}, {"b", "a"}, {"a", "ghost"}} {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatalf("AddEdge(%q,%q): %v", e[0], e[1], err)
		}
	}
	err := d.Acyclic()
	if err == nil {
		t.Fatal("Acyclic() = nil, want an error")
	}
	var de *danglingEdgeError
	if !errors.As(err, &de) {
		t.Fatalf("Acyclic() returned %T, want *danglingEdgeError (dangling is checked first)", err)
	}
}
