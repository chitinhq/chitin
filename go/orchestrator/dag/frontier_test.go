package dag

import "testing"

// pnode is a Node constructor carrying an explicit priority and status —
// the two fields the frontier and ordering tests vary.
func pnode(id string, priority int, status NodeStatus) Node {
	return Node{ID: id, Priority: priority, Status: status}
}

// frontierDAG builds a DAG from explicit nodes and (from, to) edges, failing
// the test on any constructor error.
func frontierDAG(t *testing.T, nodes []Node, edges [][2]string) *DAG {
	t.Helper()
	d := New()
	for _, n := range nodes {
		if err := d.AddNode(n); err != nil {
			t.Fatalf("frontierDAG: AddNode(%q): %v", n.ID, err)
		}
	}
	for _, e := range edges {
		if err := d.AddEdge(e[0], e[1]); err != nil {
			t.Fatalf("frontierDAG: AddEdge(%q,%q): %v", e[0], e[1], err)
		}
	}
	return d
}

// ids extracts the ID of every node in order — the comparison shape for
// frontier assertions.
func ids(nodes []Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	return out
}

// TestFrontier_Boundaries proves the runnable-frontier computation across the
// structural boundaries: empty graph, single node, all/partial/none of a
// node's dependencies satisfied, and a diamond (spec 076 FR-003).
func TestFrontier_Boundaries(t *testing.T) {
	tests := []struct {
		name  string
		nodes []Node
		edges [][2]string
		want  []string
	}{
		{
			name:  "empty DAG — empty frontier",
			nodes: nil,
			want:  nil,
		},
		{
			name:  "single pending node, no deps — runnable",
			nodes: []Node{pnode("a", 0, StatusPending)},
			want:  []string{"a"},
		},
		{
			name:  "single running node — excluded (already dispatched)",
			nodes: []Node{pnode("a", 0, StatusRunning)},
			want:  nil,
		},
		{
			name:  "single done node — excluded (terminal)",
			nodes: []Node{pnode("a", 0, StatusDone)},
			want:  nil,
		},
		{
			name:  "single failed node — excluded (terminal)",
			nodes: []Node{pnode("a", 0, StatusFailed)},
			want:  nil,
		},
		{
			name: "dependency done — dependent is runnable",
			nodes: []Node{
				pnode("a", 0, StatusDone),
				pnode("b", 0, StatusPending),
			},
			edges: [][2]string{{"b", "a"}},
			want:  []string{"b"},
		},
		{
			name: "dependency pending — dependent NOT runnable",
			nodes: []Node{
				pnode("a", 0, StatusPending),
				pnode("b", 0, StatusPending),
			},
			edges: [][2]string{{"b", "a"}},
			want:  []string{"a"}, // only the root is runnable
		},
		{
			name: "one of two deps done — dependent NOT runnable",
			nodes: []Node{
				pnode("a", 0, StatusDone),
				pnode("b", 0, StatusPending),
				pnode("c", 0, StatusPending),
			},
			edges: [][2]string{{"c", "a"}, {"c", "b"}},
			want:  []string{"b"}, // c waits on b; a is done; b is the frontier
		},
		{
			name: "all deps done — dependent runnable",
			nodes: []Node{
				pnode("a", 0, StatusDone),
				pnode("b", 0, StatusDone),
				pnode("c", 0, StatusPending),
			},
			edges: [][2]string{{"c", "a"}, {"c", "b"}},
			want:  []string{"c"},
		},
		{
			name: "diamond — top done unlocks both middles, sink waits",
			nodes: []Node{
				pnode("top", 0, StatusDone),
				pnode("left", 0, StatusPending),
				pnode("right", 0, StatusPending),
				pnode("sink", 0, StatusPending),
			},
			edges: [][2]string{
				{"left", "top"}, {"right", "top"},
				{"sink", "left"}, {"sink", "right"},
			},
			want: []string{"left", "right"}, // sink still blocked
		},
		{
			name: "diamond fully unlocked — sink is the frontier",
			nodes: []Node{
				pnode("top", 0, StatusDone),
				pnode("left", 0, StatusDone),
				pnode("right", 0, StatusDone),
				pnode("sink", 0, StatusPending),
			},
			edges: [][2]string{
				{"left", "top"}, {"right", "top"},
				{"sink", "left"}, {"sink", "right"},
			},
			want: []string{"sink"},
		},
		{
			name: "dependency failed — dependent NOT runnable",
			nodes: []Node{
				pnode("a", 0, StatusFailed),
				pnode("b", 0, StatusPending),
			},
			edges: [][2]string{{"b", "a"}},
			want:  nil, // b cannot run; a is terminal-failed
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := frontierDAG(t, tc.nodes, tc.edges)
			got := ids(d.Frontier())
			if !equalStrings(got, tc.want) {
				t.Errorf("Frontier() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOrder_PriorityThenID proves the deterministic two-key order: priority
// descending, then node ID ascending (spec 076 FR-004).
func TestOrder_PriorityThenID(t *testing.T) {
	tests := []struct {
		name string
		in   []Node
		want []string
	}{
		{
			name: "empty slice",
			in:   nil,
			want: nil,
		},
		{
			name: "single node",
			in:   []Node{pnode("a", 5, StatusRunnable)},
			want: []string{"a"},
		},
		{
			name: "priority descending",
			in: []Node{
				pnode("low", 1, StatusRunnable),
				pnode("high", 9, StatusRunnable),
				pnode("mid", 5, StatusRunnable),
			},
			want: []string{"high", "mid", "low"},
		},
		{
			name: "equal priority — ID ascending tie-breaker",
			in: []Node{
				pnode("zeta", 3, StatusRunnable),
				pnode("alpha", 3, StatusRunnable),
				pnode("mu", 3, StatusRunnable),
			},
			want: []string{"alpha", "mu", "zeta"},
		},
		{
			name: "priority dominates ID",
			in: []Node{
				pnode("aaa", 1, StatusRunnable), // lowest priority, smallest ID
				pnode("zzz", 9, StatusRunnable), // highest priority, largest ID
			},
			want: []string{"zzz", "aaa"},
		},
		{
			name: "mixed — priority groups, ID within each group",
			in: []Node{
				pnode("d", 1, StatusRunnable),
				pnode("b", 5, StatusRunnable),
				pnode("a", 5, StatusRunnable),
				pnode("c", 1, StatusRunnable),
			},
			want: []string{"a", "b", "c", "d"},
		},
		{
			name: "negative priorities ordered correctly",
			in: []Node{
				pnode("x", -1, StatusRunnable),
				pnode("y", -5, StatusRunnable),
				pnode("z", 0, StatusRunnable),
			},
			want: []string{"z", "x", "y"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ids(Order(tc.in))
			if !equalStrings(got, tc.want) {
				t.Errorf("Order() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOrder_DoesNotMutateInput proves Order sorts a copy — the caller's
// slice order is left untouched.
func TestOrder_DoesNotMutateInput(t *testing.T) {
	in := []Node{
		pnode("zeta", 1, StatusRunnable),
		pnode("alpha", 1, StatusRunnable),
	}
	before := ids(in)
	_ = Order(in)
	if !equalStrings(ids(in), before) {
		t.Errorf("Order mutated its input: before %v, after %v", before, ids(in))
	}
}

// TestFrontier_DeterministicAcross100 proves SC-001: computing the frontier
// over the same DAG yields an identical ordered result across 100 runs — with
// N nodes of EQUAL priority, the node-ID tie-breaker must order them
// identically every time, with no dependence on map iteration order.
func TestFrontier_DeterministicAcross100(t *testing.T) {
	// Six runnable roots, all priority 5 — every position decided purely by
	// the node-ID tie-breaker. IDs are inserted in a deliberately scrambled
	// order so any map-iteration dependence would surface as drift.
	insertOrder := []string{"n-delta", "n-alpha", "n-foxtrot", "n-charlie", "n-bravo", "n-echo"}
	want := []string{"n-alpha", "n-bravo", "n-charlie", "n-delta", "n-echo", "n-foxtrot"}

	for run := 0; run < 100; run++ {
		d := New()
		// Rotate the insertion order each run so no two runs build the map
		// the same way — a determinism bug would diverge here.
		rotated := make([]string, 0, len(insertOrder))
		shift := run % len(insertOrder)
		rotated = append(rotated, insertOrder[shift:]...)
		rotated = append(rotated, insertOrder[:shift]...)
		for _, id := range rotated {
			if err := d.AddNode(pnode(id, 5, StatusPending)); err != nil {
				t.Fatalf("run %d: AddNode(%q): %v", run, id, err)
			}
		}

		got := ids(d.Frontier())
		if !equalStrings(got, want) {
			t.Fatalf("run %d: Frontier() = %v, want %v (ordering must be deterministic)",
				run, got, want)
		}
	}
}

// TestFrontier_DeterministicWithMixedPriority proves determinism holds for a
// frontier mixing priorities AND ties — across 100 runs the (priority desc,
// ID asc) order never drifts.
func TestFrontier_DeterministicWithMixedPriority(t *testing.T) {
	type spec struct {
		id       string
		priority int
	}
	specs := []spec{
		{"task-z", 9}, {"task-a", 9}, // tie at 9 -> a before z
		{"task-m", 5},
		{"task-c", 1}, {"task-b", 1}, // tie at 1 -> b before c
	}
	want := []string{"task-a", "task-z", "task-m", "task-b", "task-c"}

	for run := 0; run < 100; run++ {
		d := New()
		// Insert in reverse on odd runs to vary map construction order.
		order := make([]spec, len(specs))
		copy(order, specs)
		if run%2 == 1 {
			for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
				order[i], order[j] = order[j], order[i]
			}
		}
		for _, s := range order {
			if err := d.AddNode(pnode(s.id, s.priority, StatusPending)); err != nil {
				t.Fatalf("run %d: AddNode(%q): %v", run, s.id, err)
			}
		}
		got := ids(d.Frontier())
		if !equalStrings(got, want) {
			t.Fatalf("run %d: Frontier() = %v, want %v", run, got, want)
		}
	}
}

// TestPropagateFailure proves a permanently failed node blocks its transitive
// dependents — they become blocked-dependency-failed, never left runnable
// (spec 076 FR-011).
func TestPropagateFailure(t *testing.T) {
	t.Run("direct dependent is blocked", func(t *testing.T) {
		// b depends on a; a fails.
		d := frontierDAG(t,
			[]Node{pnode("a", 0, StatusFailed), pnode("b", 0, StatusPending)},
			[][2]string{{"b", "a"}},
		)
		blocked := d.PropagateFailure("a")
		if !equalStrings(blocked, []string{"b"}) {
			t.Fatalf("PropagateFailure returned %v, want [b]", blocked)
		}
		nb, _ := d.Node("b")
		if nb.Status != StatusBlockedDependencyFailed {
			t.Errorf("node b status = %s, want blocked-dependency-failed", nb.Status)
		}
	})

	t.Run("transitive dependents are blocked through a chain", func(t *testing.T) {
		// chain: a (failed) <- b <- c <- d
		d := frontierDAG(t,
			[]Node{
				pnode("a", 0, StatusFailed),
				pnode("b", 0, StatusPending),
				pnode("c", 0, StatusPending),
				pnode("d", 0, StatusPending),
			},
			[][2]string{{"b", "a"}, {"c", "b"}, {"d", "c"}},
		)
		blocked := d.PropagateFailure("a")
		if !equalStrings(blocked, []string{"b", "c", "d"}) {
			t.Fatalf("PropagateFailure returned %v, want [b c d]", blocked)
		}
		for _, id := range []string{"b", "c", "d"} {
			n, _ := d.Node(id)
			if n.Status != StatusBlockedDependencyFailed {
				t.Errorf("node %s status = %s, want blocked-dependency-failed", id, n.Status)
			}
		}
	})

	t.Run("diamond — a shared dependent is blocked once", func(t *testing.T) {
		// a (failed) <- b, a <- c, and d depends on both b and c.
		d := frontierDAG(t,
			[]Node{
				pnode("a", 0, StatusFailed),
				pnode("b", 0, StatusPending),
				pnode("c", 0, StatusPending),
				pnode("d", 0, StatusPending),
			},
			[][2]string{{"b", "a"}, {"c", "a"}, {"d", "b"}, {"d", "c"}},
		)
		blocked := d.PropagateFailure("a")
		// d is reachable via both b and c but must appear exactly once.
		if !equalStrings(blocked, []string{"b", "c", "d"}) {
			t.Fatalf("PropagateFailure returned %v, want [b c d] (no duplicate d)", blocked)
		}
	})

	t.Run("a running dependent is left for the scheduler", func(t *testing.T) {
		// b is already running when its dependency a fails — PropagateFailure
		// must not yank an in-flight node.
		d := frontierDAG(t,
			[]Node{pnode("a", 0, StatusFailed), pnode("b", 0, StatusRunning)},
			[][2]string{{"b", "a"}},
		)
		blocked := d.PropagateFailure("a")
		if len(blocked) != 0 {
			t.Errorf("PropagateFailure blocked %v, want none (b is running)", blocked)
		}
		nb, _ := d.Node("b")
		if nb.Status != StatusRunning {
			t.Errorf("running node b changed to %s, want still running", nb.Status)
		}
	})

	t.Run("an already-done dependent is left untouched", func(t *testing.T) {
		d := frontierDAG(t,
			[]Node{pnode("a", 0, StatusFailed), pnode("b", 0, StatusDone)},
			[][2]string{{"b", "a"}},
		)
		blocked := d.PropagateFailure("a")
		if len(blocked) != 0 {
			t.Errorf("PropagateFailure blocked %v, want none (b is already done)", blocked)
		}
		nb, _ := d.Node("b")
		if nb.Status != StatusDone {
			t.Errorf("done node b changed to %s, want still done", nb.Status)
		}
	})

	t.Run("an unknown failed node changes nothing", func(t *testing.T) {
		d := frontierDAG(t,
			[]Node{pnode("a", 0, StatusPending)},
			nil,
		)
		blocked := d.PropagateFailure("ghost")
		if len(blocked) != 0 {
			t.Errorf("PropagateFailure(ghost) blocked %v, want none", blocked)
		}
		na, _ := d.Node("a")
		if na.Status != StatusPending {
			t.Errorf("node a changed to %s, want still pending", na.Status)
		}
	})
}

// TestPropagateFailure_FrontierExcludesBlocked proves the end-to-end
// invariant: after propagation, a blocked dependent never appears in the
// runnable frontier — it is never left runnable or silently skipped.
func TestPropagateFailure_FrontierExcludesBlocked(t *testing.T) {
	// a (failed) <- b; an independent root c is runnable. After propagation
	// the frontier must be exactly [c].
	d := frontierDAG(t,
		[]Node{
			pnode("a", 0, StatusFailed),
			pnode("b", 0, StatusPending),
			pnode("c", 0, StatusPending),
		},
		[][2]string{{"b", "a"}},
	)
	d.PropagateFailure("a")
	got := ids(d.Frontier())
	if !equalStrings(got, []string{"c"}) {
		t.Errorf("Frontier() after propagation = %v, want [c] (b must be blocked, c proceeds)", got)
	}
}

// TestStalled proves the explicit stalled-graph detection (spec 076 FR-016):
// no runnable, no running, undone nodes remain.
func TestStalled(t *testing.T) {
	tests := []struct {
		name  string
		nodes []Node
		edges [][2]string
		want  bool
	}{
		{
			name:  "empty DAG is not stalled",
			nodes: nil,
			want:  false,
		},
		{
			name:  "all nodes done — complete, not stalled",
			nodes: []Node{pnode("a", 0, StatusDone), pnode("b", 0, StatusDone)},
			want:  false,
		},
		{
			name:  "a runnable node — not stalled",
			nodes: []Node{pnode("a", 0, StatusPending)},
			want:  false,
		},
		{
			name:  "a running node — not stalled",
			nodes: []Node{pnode("a", 0, StatusRunning)},
			want:  false,
		},
		{
			name: "blocked dependent with nothing else to run — STALLED",
			nodes: []Node{
				pnode("a", 0, StatusFailed),
				pnode("b", 0, StatusBlockedDependencyFailed),
				pnode("c", 0, StatusPending),
			},
			edges: [][2]string{{"b", "a"}, {"c", "b"}},
			want:  true, // c depends on blocked b; nothing runnable, nothing running
		},
		{
			name: "blocked dependent but an independent node runnable — not stalled",
			nodes: []Node{
				pnode("a", 0, StatusFailed),
				pnode("b", 0, StatusBlockedDependencyFailed),
				pnode("c", 0, StatusPending), // independent root
			},
			edges: [][2]string{{"b", "a"}},
			want:  false,
		},
		{
			name: "undone node waiting on a still-running node — not stalled",
			nodes: []Node{
				pnode("a", 0, StatusRunning),
				pnode("b", 0, StatusPending),
			},
			edges: [][2]string{{"b", "a"}},
			want:  false,
		},
		{
			name: "unroutable blocked node leaves a true stall",
			nodes: []Node{
				pnode("a", 0, StatusBlockedUnroutable),
				pnode("b", 0, StatusPending),
			},
			edges: [][2]string{{"b", "a"}},
			want:  true, // b waits on a, which can never run
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := frontierDAG(t, tc.nodes, tc.edges)
			if got := d.Stalled(); got != tc.want {
				t.Errorf("Stalled() = %v, want %v", got, tc.want)
			}
		})
	}
}
