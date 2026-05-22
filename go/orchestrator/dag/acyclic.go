package dag

import (
	"fmt"
	"sort"
	"strings"
)

// Acyclic verifies the DAG's two structural invariants and returns the first
// violation it finds, or nil if the graph is well-formed (spec 076 FR-002).
//
// The two invariants, checked in this order:
//
//  1. No dangling edge — every edge endpoint (From and To) names a node that
//     exists in the DAG. An adapter may add edges and nodes in any order, so
//     this is verified here rather than at AddEdge time.
//  2. No cycle — the transitive closure of the depends_on relation is
//     acyclic. On a cycle, the returned error NAMES the cycle: the node IDs
//     forming it, in dependency order, so an operator can see exactly which
//     work units form the loop (spec 076 SC-003 — "with the cycle named").
//
// Acyclic is pure and deterministic: it sorts every adjacency list and
// visits roots in sorted ID order, so the *same* DAG always yields the *same*
// error — the named cycle and the named dangling edge are stable across runs
// regardless of Go map iteration order. The empty DAG (zero nodes) is
// trivially acyclic and well-formed; Acyclic returns nil.
//
// A *cycleError is returned for a cycle and a *danglingEdgeError for a
// dangling edge; callers may type-assert to inspect the offending nodes, or
// simply use the error message.
func (d *DAG) Acyclic() error {
	if err := d.checkDanglingEdges(); err != nil {
		return err
	}
	return d.checkAcyclic()
}

// danglingEdgeError reports an edge whose endpoint names a non-existent node.
type danglingEdgeError struct {
	// Edge is the offending depends_on edge.
	Edge Edge
	// Missing is the endpoint ID that does not exist in the DAG — either
	// Edge.From or Edge.To.
	Missing string
}

func (e *danglingEdgeError) Error() string {
	return fmt.Sprintf(
		"dag: dangling edge %s depends_on %s — node %q does not exist",
		e.Edge.From, e.Edge.To, e.Missing,
	)
}

// checkDanglingEdges returns the first edge (in deterministic (From, To)
// order) whose From or To names a node absent from the DAG. From is checked
// before To so the error is stable.
func (d *DAG) checkDanglingEdges() error {
	for _, e := range d.Edges() { // Edges() is sorted — deterministic.
		if _, ok := d.nodes[e.From]; !ok {
			return &danglingEdgeError{Edge: e, Missing: e.From}
		}
		if _, ok := d.nodes[e.To]; !ok {
			return &danglingEdgeError{Edge: e, Missing: e.To}
		}
	}
	return nil
}

// cycleError reports a dependency cycle in the DAG and names the nodes that
// form it.
type cycleError struct {
	// Cycle is the node IDs forming the cycle, listed in depends_on order:
	// Cycle[0] depends_on Cycle[1] depends_on … depends_on Cycle[0]. The
	// slice is canonicalized to start at the lexically smallest member so the
	// same cycle always prints identically.
	Cycle []string
}

func (e *cycleError) Error() string {
	// Render as a closed loop: a -> b -> c -> a.
	loop := append(append([]string{}, e.Cycle...), e.Cycle[0])
	return fmt.Sprintf("dag: cycle detected: %s", strings.Join(loop, " -> "))
}

// checkAcyclic runs a depth-first search over the depends_on relation,
// classifying each node white/grey/black. A grey node reached again is a
// back-edge — a cycle — and the grey stack from that node down names it.
//
// Determinism: roots are visited in sorted ID order and each node's
// dependency list is sorted, so the DFS tree, and therefore the *first* cycle
// found, is fully determined by the DAG's contents alone.
func (d *DAG) checkAcyclic() error {
	const (
		white = 0 // unvisited
		grey  = 1 // on the current DFS stack
		black = 2 // fully explored, no cycle through it
	)
	color := make(map[string]int, len(d.nodes))

	// stack holds the grey path from the current DFS root to the node being
	// expanded; it is what names a cycle when a back-edge is found.
	var stack []string

	// ids is every node ID in sorted order — the deterministic visit order.
	ids := make([]string, 0, len(d.nodes))
	for id := range d.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// visit explores id; it returns the named cycle if one is found beneath
	// (or at) id, else nil.
	var visit func(id string) []string
	visit = func(id string) []string {
		color[id] = grey
		stack = append(stack, id)

		// Dependencies() returns the depends_on targets sorted ascending —
		// a deterministic expansion order.
		for _, dep := range d.Dependencies(id) {
			switch color[dep] {
			case grey:
				// Back-edge: dep is on the current stack. The cycle is the
				// stack slice from dep's position to the top, which all
				// depends_on the next, and id depends_on dep closes it.
				return extractCycle(stack, dep)
			case white:
				if c := visit(dep); c != nil {
					return c
				}
			case black:
				// Fully explored already — no cycle through dep.
			}
		}

		stack = stack[:len(stack)-1]
		color[id] = black
		return nil
	}

	for _, id := range ids {
		if color[id] == white {
			if c := visit(id); c != nil {
				return &cycleError{Cycle: canonicalizeCycle(c)}
			}
		}
	}
	return nil
}

// extractCycle returns the segment of stack from the first occurrence of
// start to the end — the node IDs forming the detected cycle, in depends_on
// order. start is guaranteed to be on the stack (it is the grey node a
// back-edge reached).
func extractCycle(stack []string, start string) []string {
	for i, id := range stack {
		if id == start {
			cyc := make([]string, len(stack)-i)
			copy(cyc, stack[i:])
			return cyc
		}
	}
	// Unreachable: start is always on the stack when extractCycle is called.
	return append([]string{}, stack...)
}

// canonicalizeCycle rotates a cycle so it begins at its lexically smallest
// member, preserving the depends_on order. This makes the printed cycle
// identical no matter which DFS root first entered the loop — the same loop
// always reads the same way (spec 076 SC-001 determinism).
func canonicalizeCycle(cycle []string) []string {
	if len(cycle) == 0 {
		return cycle
	}
	min := 0
	for i := 1; i < len(cycle); i++ {
		if cycle[i] < cycle[min] {
			min = i
		}
	}
	out := make([]string, 0, len(cycle))
	out = append(out, cycle[min:]...)
	out = append(out, cycle[:min]...)
	return out
}
