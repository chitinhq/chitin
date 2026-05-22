package dag

import "sort"

// Frontier returns the runnable frontier of the DAG: exactly the nodes whose
// every dependency is StatusDone and which are not themselves already in a
// running or terminal state (spec 076 FR-003).
//
// A node is in the frontier iff ALL of the following hold:
//
//   - it is not terminal (done / failed / either blocked state) — a settled
//     node is never re-dispatched;
//   - it is not already StatusRunning — exactly-once dispatch (spec 076
//     FR-009) means a running node is excluded;
//   - every node it depends_on exists and is StatusDone.
//
// A node with zero dependencies is runnable immediately (its "every
// dependency is done" quantifier is vacuously true). A node any of whose
// dependencies has failed or is blocked is NOT runnable — it should be marked
// blocked via PropagateFailure rather than left to linger here.
//
// The returned slice is ordered deterministically by Order — priority
// descending, then node ID ascending (spec 076 FR-004). Frontier is pure: no
// wall clock, no randomness, no dependence on map iteration order. The empty
// DAG yields an empty (non-nil-safe) frontier.
func (d *DAG) Frontier() []Node {
	var frontier []Node
	for _, n := range d.Nodes() { // Nodes() is sorted — deterministic.
		if !d.isRunnable(n) {
			continue
		}
		frontier = append(frontier, n)
	}
	return Order(frontier)
}

// isRunnable reports whether node n satisfies every runnability condition.
// It treats a node whose status is already StatusRunnable the same as
// StatusPending — both are eligible; the scheduler is responsible for
// transitioning a frontier node to StatusRunning on dispatch.
func (d *DAG) isRunnable(n Node) bool {
	if n.Status.Terminal() || n.Status == StatusRunning {
		return false
	}
	for _, depID := range d.Dependencies(n.ID) {
		dep, ok := d.nodes[depID]
		if !ok {
			// A dangling dependency — Acyclic rejects this graph before the
			// scheduler ticks; defensively treat it as not satisfied.
			return false
		}
		if dep.Status != StatusDone {
			return false
		}
	}
	return true
}

// Order returns nodes sorted into the deterministic dispatch order
// (spec 076 FR-004, Key Entities: Runnable Frontier). The total order is two
// keys applied in sequence:
//
//  1. Priority — DESCENDING. A higher Priority is dispatched first.
//  2. ID       — ASCENDING, lexical. The stable final tie-breaker; because
//     node IDs are DAG-unique this key never ties, so the order is total and
//     the result fully determined.
//
// Order is pure and never depends on the input slice's order or on map
// iteration order — N nodes of equal priority always come out sorted by ID.
// It sorts a copy; the caller's slice is left untouched. Ordering an empty or
// single-element slice is a no-op.
func Order(nodes []Node) []Node {
	out := make([]Node, len(nodes))
	copy(out, nodes)
	sort.SliceStable(out, func(i, j int) bool { return lessNode(out[i], out[j]) })
	return out
}

// lessNode is the strict-weak-ordering predicate behind Order: true iff node
// a sorts strictly before node b under the priority-desc, then id-asc total
// order. Because ID is unique within a DAG, the second key never ties — the
// order is total and SliceStable behaves like a full sort.
func lessNode(a, b Node) bool {
	if a.Priority != b.Priority {
		return a.Priority > b.Priority // descending
	}
	return a.ID < b.ID // ascending — the stable tie-breaker
}

// PropagateFailure marks every node transitively dependent on a permanently
// failed node as StatusBlockedDependencyFailed (spec 076 FR-011). A node
// whose dependency has failed can never run, so it must be moved to an
// explicit blocked terminal state — never left runnable, never silently
// skipped (spec 076 edge case "a node's dependency permanently fails").
//
// failedID names the node that permanently failed; it is expected to already
// be StatusFailed (or StatusBlockedDependencyFailed). PropagateFailure walks
// the dependents relation breadth-first from failedID and, for every reachable
// node that is not already terminal and not already running, sets it to
// StatusBlockedDependencyFailed.
//
// A node already StatusRunning is left untouched: its work is in flight and
// the scheduler resolves its outcome when the child workflow returns —
// PropagateFailure does not race with an in-flight dispatch.
//
// It returns the IDs of the nodes it newly blocked, sorted ascending, so the
// caller can project exactly those transitions (spec 076 FR-014) and emit
// telemetry. The result is deterministic: the dependents relation is walked
// in sorted ID order. If failedID does not exist, no node is changed and an
// empty slice is returned.
func (d *DAG) PropagateFailure(failedID string) []string {
	if _, ok := d.nodes[failedID]; !ok {
		return nil
	}

	// blocked accumulates newly blocked IDs; seen guards against revisiting a
	// node reachable by more than one path (a diamond).
	var blocked []string
	seen := map[string]bool{failedID: true}
	queue := []string{failedID}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, depID := range d.Dependents(cur) { // sorted — deterministic.
			if seen[depID] {
				continue
			}
			seen[depID] = true

			n, ok := d.nodes[depID]
			if !ok {
				continue
			}
			// A node already terminal stays as it is; a running node is left
			// for the scheduler to resolve. Either way, still walk past it so
			// transitive dependents below a terminal node are reached.
			if !n.Status.Terminal() && n.Status != StatusRunning {
				n.Status = StatusBlockedDependencyFailed
				d.nodes[depID] = n
				blocked = append(blocked, depID)
			}
			queue = append(queue, depID)
		}
	}

	sort.Strings(blocked)
	return blocked
}

// Stalled reports whether the DAG is stuck: no node is runnable, no node is
// running, yet at least one node has not reached a terminal state
// (spec 076 FR-016). A stalled graph must be surfaced as an explicit state by
// the scheduler — never a silent spin. Stalled is pure and deterministic.
//
// The empty DAG is NOT stalled (there is no undone work) — Stalled returns
// false. A DAG all of whose nodes are terminal is likewise not stalled: it is
// complete.
func (d *DAG) Stalled() bool {
	anyUndone := false
	for _, n := range d.nodes {
		if n.Status == StatusRunning {
			return false // work is in flight — not stalled.
		}
		if !n.Status.Terminal() {
			anyUndone = true
		}
	}
	if !anyUndone {
		return false // every node settled — complete, not stalled.
	}
	// Undone nodes remain and nothing is running; stalled iff the frontier is
	// empty (no undone node can advance).
	return len(d.Frontier()) == 0
}
