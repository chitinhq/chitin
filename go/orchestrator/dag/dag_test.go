package dag

import "testing"

// node is a tiny constructor for a Node carrying just the fields the type
// tests exercise — an id, a priority, and a status. Routing fields default
// to their zero values, which is what most cases want.
func node(id string, priority int, status NodeStatus) Node {
	return Node{ID: id, Priority: priority, Status: status}
}

// TestNodeStatus_String proves every declared status renders to its spec
// wire-form name, and an out-of-range value renders without panicking.
func TestNodeStatus_String(t *testing.T) {
	tests := []struct {
		status NodeStatus
		want   string
	}{
		{StatusPending, "pending"},
		{StatusRunnable, "runnable"},
		{StatusRunning, "running"},
		{StatusDone, "done"},
		{StatusFailed, "failed"},
		{StatusBlockedUnroutable, "blocked-unroutable"},
		{StatusBlockedDependencyFailed, "blocked-dependency-failed"},
		{NodeStatus(-1), "status(-1)"},
		{NodeStatus(99), "status(99)"},
	}
	for _, tc := range tests {
		if got := tc.status.String(); got != tc.want {
			t.Errorf("NodeStatus(%d).String() = %q, want %q", int(tc.status), got, tc.want)
		}
	}
}

// TestNodeStatus_Valid proves Valid accepts exactly the declared statuses.
func TestNodeStatus_Valid(t *testing.T) {
	tests := []struct {
		status NodeStatus
		want   bool
	}{
		{StatusPending, true},
		{StatusRunnable, true},
		{StatusRunning, true},
		{StatusDone, true},
		{StatusFailed, true},
		{StatusBlockedUnroutable, true},
		{StatusBlockedDependencyFailed, true},
		{NodeStatus(-1), false},
		{NodeStatus(7), false},
	}
	for _, tc := range tests {
		if got := tc.status.Valid(); got != tc.want {
			t.Errorf("NodeStatus(%d).Valid() = %v, want %v", int(tc.status), got, tc.want)
		}
	}
}

// TestNodeStatus_Terminal proves Terminal is true for exactly the four
// settled states and false for the three live ones.
func TestNodeStatus_Terminal(t *testing.T) {
	tests := []struct {
		status NodeStatus
		want   bool
	}{
		{StatusPending, false},
		{StatusRunnable, false},
		{StatusRunning, false},
		{StatusDone, true},
		{StatusFailed, true},
		{StatusBlockedUnroutable, true},
		{StatusBlockedDependencyFailed, true},
	}
	for _, tc := range tests {
		if got := tc.status.Terminal(); got != tc.want {
			t.Errorf("%s.Terminal() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

// TestNew_EmptyDAG proves the empty DAG is a valid, queryable zero value:
// zero nodes, zero edges, an empty frontier, and acyclic.
func TestNew_EmptyDAG(t *testing.T) {
	d := New()
	if d.Len() != 0 {
		t.Errorf("New().Len() = %d, want 0", d.Len())
	}
	if got := d.Nodes(); len(got) != 0 {
		t.Errorf("New().Nodes() = %v, want empty", got)
	}
	if got := d.Edges(); len(got) != 0 {
		t.Errorf("New().Edges() = %v, want empty", got)
	}
	if err := d.Acyclic(); err != nil {
		t.Errorf("New().Acyclic() = %v, want nil (empty DAG is acyclic)", err)
	}
	if got := d.Frontier(); len(got) != 0 {
		t.Errorf("New().Frontier() = %v, want empty", got)
	}
}

// TestAddNode proves node insertion, the non-empty-ID rule, and the
// duplicate-ID rejection — IDs are the DAG's primary key.
func TestAddNode(t *testing.T) {
	t.Run("inserts and reads back", func(t *testing.T) {
		d := New()
		want := Node{ID: "a", SpecRef: "076", TaskRef: "T001", Priority: 5}
		if err := d.AddNode(want); err != nil {
			t.Fatalf("AddNode: unexpected error: %v", err)
		}
		got, ok := d.Node("a")
		if !ok {
			t.Fatal("Node(\"a\"): not found after AddNode")
		}
		if got != want {
			t.Errorf("Node(\"a\") = %+v, want %+v", got, want)
		}
	})

	t.Run("rejects empty ID", func(t *testing.T) {
		d := New()
		if err := d.AddNode(Node{ID: ""}); err == nil {
			t.Error("AddNode with empty ID: want an error, got nil")
		}
	})

	t.Run("rejects duplicate ID", func(t *testing.T) {
		d := New()
		if err := d.AddNode(node("a", 0, StatusPending)); err != nil {
			t.Fatalf("first AddNode: unexpected error: %v", err)
		}
		if err := d.AddNode(node("a", 9, StatusDone)); err == nil {
			t.Error("AddNode with duplicate ID: want an error, got nil")
		}
		// The original node must be untouched by the rejected insert.
		got, _ := d.Node("a")
		if got.Priority != 0 || got.Status != StatusPending {
			t.Errorf("rejected duplicate mutated the original node: %+v", got)
		}
	})
}

// TestAddNode_StoresByValue proves a node is stored by value — mutating the
// caller's copy after AddNode does not change the DAG.
func TestAddNode_StoresByValue(t *testing.T) {
	d := New()
	n := node("a", 1, StatusPending)
	if err := d.AddNode(n); err != nil {
		t.Fatalf("AddNode: unexpected error: %v", err)
	}
	n.Priority = 999 // mutate the caller's copy.
	got, _ := d.Node("a")
	if got.Priority != 1 {
		t.Errorf("DAG node followed a caller mutation: Priority = %d, want 1", got.Priority)
	}
}

// TestAddEdge proves edge insertion, the self-loop rejection, the empty-
// endpoint rejection, and the duplicate-edge dedupe.
func TestAddEdge(t *testing.T) {
	t.Run("inserts an edge", func(t *testing.T) {
		d := New()
		if err := d.AddEdge("b", "a"); err != nil {
			t.Fatalf("AddEdge: unexpected error: %v", err)
		}
		if got := d.Edges(); len(got) != 1 || got[0] != (Edge{From: "b", To: "a"}) {
			t.Errorf("Edges() = %v, want one edge b->a", got)
		}
	})

	t.Run("rejects a self-loop", func(t *testing.T) {
		d := New()
		if err := d.AddEdge("a", "a"); err == nil {
			t.Error("AddEdge(a,a): want an error (trivial cycle), got nil")
		}
	})

	t.Run("rejects empty endpoints", func(t *testing.T) {
		d := New()
		if err := d.AddEdge("", "a"); err == nil {
			t.Error("AddEdge(\"\",a): want an error, got nil")
		}
		if err := d.AddEdge("a", ""); err == nil {
			t.Error("AddEdge(a,\"\"): want an error, got nil")
		}
	})

	t.Run("dedupes a duplicate edge", func(t *testing.T) {
		d := New()
		if err := d.AddEdge("b", "a"); err != nil {
			t.Fatalf("AddEdge: unexpected error: %v", err)
		}
		if err := d.AddEdge("b", "a"); err != nil {
			t.Fatalf("duplicate AddEdge: unexpected error: %v", err)
		}
		if got := d.Edges(); len(got) != 1 {
			t.Errorf("Edges() after a duplicate = %d edges, want 1", len(got))
		}
	})
}

// TestNodes_DeterministicOrder proves Nodes() returns nodes sorted by ID
// regardless of insertion order — no dependence on map iteration order.
func TestNodes_DeterministicOrder(t *testing.T) {
	d := New()
	for _, id := range []string{"zeta", "alpha", "mu", "beta"} {
		if err := d.AddNode(node(id, 0, StatusPending)); err != nil {
			t.Fatalf("AddNode(%q): %v", id, err)
		}
	}
	got := d.Nodes()
	want := []string{"alpha", "beta", "mu", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Nodes() returned %d nodes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("Nodes()[%d].ID = %q, want %q", i, got[i].ID, want[i])
		}
	}
}

// TestEdges_DeterministicOrder proves Edges() returns edges sorted by
// (From, To) regardless of insertion order.
func TestEdges_DeterministicOrder(t *testing.T) {
	d := New()
	inserts := [][2]string{{"z", "a"}, {"a", "b"}, {"a", "a2"}, {"m", "n"}}
	for _, e := range inserts {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatalf("AddEdge(%q,%q): %v", e[0], e[1], err)
		}
	}
	got := d.Edges()
	want := []Edge{
		{From: "a", To: "a2"},
		{From: "a", To: "b"},
		{From: "m", To: "n"},
		{From: "z", To: "a"},
	}
	if len(got) != len(want) {
		t.Fatalf("Edges() returned %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Edges()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestSetStatus proves a status update is applied, an invalid status is
// rejected, and an unknown node is rejected.
func TestSetStatus(t *testing.T) {
	t.Run("applies a valid status", func(t *testing.T) {
		d := New()
		if err := d.AddNode(node("a", 0, StatusPending)); err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		if err := d.SetStatus("a", StatusRunning); err != nil {
			t.Fatalf("SetStatus: unexpected error: %v", err)
		}
		got, _ := d.Node("a")
		if got.Status != StatusRunning {
			t.Errorf("after SetStatus: Status = %s, want running", got.Status)
		}
	})

	t.Run("rejects an invalid status", func(t *testing.T) {
		d := New()
		if err := d.AddNode(node("a", 0, StatusPending)); err != nil {
			t.Fatalf("AddNode: %v", err)
		}
		if err := d.SetStatus("a", NodeStatus(42)); err == nil {
			t.Error("SetStatus with an invalid status: want an error, got nil")
		}
	})

	t.Run("rejects an unknown node", func(t *testing.T) {
		d := New()
		if err := d.SetStatus("ghost", StatusDone); err == nil {
			t.Error("SetStatus on a missing node: want an error, got nil")
		}
	})
}

// TestDependencies_And_Dependents proves the two adjacency accessors return
// the right neighbors in sorted order. The graph: c and d both depend on a;
// d also depends on b.
func TestDependencies_And_Dependents(t *testing.T) {
	d := New()
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := d.AddNode(node(id, 0, StatusPending)); err != nil {
			t.Fatalf("AddNode(%q): %v", id, err)
		}
	}
	for _, e := range [][2]string{{"c", "a"}, {"d", "a"}, {"d", "b"}} {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatalf("AddEdge(%q,%q): %v", e[0], e[1], err)
		}
	}

	if got := d.Dependencies("d"); !equalStrings(got, []string{"a", "b"}) {
		t.Errorf("Dependencies(\"d\") = %v, want [a b]", got)
	}
	if got := d.Dependencies("a"); len(got) != 0 {
		t.Errorf("Dependencies(\"a\") = %v, want empty (a is a root)", got)
	}
	if got := d.Dependents("a"); !equalStrings(got, []string{"c", "d"}) {
		t.Errorf("Dependents(\"a\") = %v, want [c d]", got)
	}
	if got := d.Dependents("d"); len(got) != 0 {
		t.Errorf("Dependents(\"d\") = %v, want empty (d is a leaf)", got)
	}
}

// equalStrings reports whether two string slices are element-wise equal.
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
