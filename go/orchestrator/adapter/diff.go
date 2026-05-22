package adapter

import (
	"sort"

	"github.com/chitinhq/chitin/go/orchestrator/dag"
)

// NodeChange records one node that exists in both the prior and the freshly
// compiled DAG but whose compiled-from-spec content differs (FR-012). It
// carries both node values so a caller can see exactly what moved without
// re-reading either DAG.
type NodeChange struct {
	// ID is the node identifier — identical in Old and New.
	ID string
	// Old is the node as it stood in the prior DAG.
	Old dag.Node
	// New is the node as it stands in the freshly compiled DAG.
	New dag.Node
}

// DAGDiff is the result of recompiling a changed spec: the nodes added,
// removed, and changed between a prior compilation and a fresh one (FR-012).
// It exists so a spec edit produces a precise delta the scheduler can apply
// to in-flight work — never a silent wholesale replacement.
//
// Every slice is sorted by node ID, so the diff of a given before/after pair
// is deterministic.
type DAGDiff struct {
	// Added is the nodes present in the new DAG but absent from the prior one,
	// sorted by ID.
	Added []dag.Node
	// Removed is the nodes present in the prior DAG but absent from the new
	// one, sorted by ID.
	Removed []dag.Node
	// Changed is the nodes present in both whose compiled content differs,
	// sorted by ID.
	Changed []NodeChange
}

// Empty reports whether the diff records no change at all — the two DAGs
// compiled to the same set of nodes with the same content.
func (d *DAGDiff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}

// Diff compares a prior DAG against a freshly compiled one and returns the
// added / removed / changed nodes (FR-012). It is a pure, deterministic
// function: a nil DAG is treated as an empty DAG (so the first compilation of
// a spec diffs as all-added), and the result is sorted by node ID.
//
// "Changed" is decided by comparing the nodes' compiled-from-spec content,
// NOT their lifecycle status: a node whose Status moved pending→running as
// the scheduler ran it has not "changed" in the spec sense, and Diff must not
// flag it — otherwise every recompilation of a running graph would look like
// a wholesale rewrite. nodeContentEqual makes that distinction explicit.
func Diff(prior, fresh *dag.DAG) *DAGDiff {
	priorNodes := nodeMap(prior)
	freshNodes := nodeMap(fresh)

	out := &DAGDiff{}

	for id, freshNode := range freshNodes {
		priorNode, existed := priorNodes[id]
		switch {
		case !existed:
			out.Added = append(out.Added, freshNode)
		case !nodeContentEqual(priorNode, freshNode):
			out.Changed = append(out.Changed, NodeChange{ID: id, Old: priorNode, New: freshNode})
		}
	}
	for id, priorNode := range priorNodes {
		if _, stillThere := freshNodes[id]; !stillThere {
			out.Removed = append(out.Removed, priorNode)
		}
	}

	sort.Slice(out.Added, func(i, j int) bool { return out.Added[i].ID < out.Added[j].ID })
	sort.Slice(out.Removed, func(i, j int) bool { return out.Removed[i].ID < out.Removed[j].ID })
	sort.Slice(out.Changed, func(i, j int) bool { return out.Changed[i].ID < out.Changed[j].ID })
	return out
}

// nodeMap returns the DAG's nodes keyed by ID. A nil DAG yields an empty map,
// so Diff treats "no prior DAG" as "every node is new".
func nodeMap(d *dag.DAG) map[string]dag.Node {
	if d == nil {
		return map[string]dag.Node{}
	}
	out := make(map[string]dag.Node, d.Len())
	for _, n := range d.Nodes() {
		out[n.ID] = n
	}
	return out
}

// nodeContentEqual reports whether two nodes are equal in their
// compiled-from-spec content — every field a recompilation derives from the
// spec. It deliberately ignores Status: Status is the scheduler's runtime
// state, not a property of the spec, so a node that merely advanced through
// its lifecycle is not a spec change (see Diff).
func nodeContentEqual(a, b dag.Node) bool {
	return a.ID == b.ID &&
		a.SpecRef == b.SpecRef &&
		a.TaskRef == b.TaskRef &&
		a.Capability == b.Capability &&
		a.Priority == b.Priority &&
		a.TierHint == b.TierHint &&
		a.TargetRepo == b.TargetRepo &&
		a.BaseRef == b.BaseRef &&
		a.WorktreeRequired == b.WorktreeRequired
}
