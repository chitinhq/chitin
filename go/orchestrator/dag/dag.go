// Package dag is the pure, deterministic DAG library at the heart of the
// Spec-DAG Scheduler (spec 076). It defines the work-unit dependency graph —
// nodes, dependency edges, and node status — and the deterministic functions
// over it: cycle detection that names the cycle (acyclic.go) and the runnable
// frontier with its named tie-breaker ordering (frontier.go).
//
// The package has NO Temporal import and touches no wall clock. Every
// function is a pure mapping from inputs to outputs with no reliance on Go
// map iteration order — determinism is the whole point (spec 076 SC-001).
// The scheduler workflow (go/orchestrator/workflows/) builds on this library;
// keeping the library pure lets the determinism that matters most — frontier
// computation and ordering — be proven by `go test` without a Temporal
// harness.
package dag

import (
	"fmt"
	"sort"
)

// NodeStatus is the lifecycle state of a DAG node — a closed enumeration
// (spec 076 Key Entities: Node Status). The lifecycle is:
//
//	pending → runnable → running → done
//	                            ↘ failed
//	pending → blocked-unroutable          (no satisfiable driver)
//	pending → blocked-dependency-failed   (a dependency permanently failed)
//
// A node begins pending. Once every dependency is done it becomes runnable;
// when dispatched it is running; it then settles to done or failed. The two
// blocked terminal states record why a node will never run.
type NodeStatus int

const (
	// StatusPending is the zero value — the node has been compiled into the
	// DAG but not yet evaluated against its dependencies.
	StatusPending NodeStatus = iota
	// StatusRunnable means every dependency is StatusDone — the node is
	// eligible for the runnable frontier and may be dispatched.
	StatusRunnable
	// StatusRunning means the node has been dispatched and its work is in
	// flight. A running node MUST NOT be re-dispatched (spec 076 FR-009).
	StatusRunning
	// StatusDone means the node's work completed successfully; dependents may
	// now be evaluated for runnability.
	StatusDone
	// StatusFailed means the node's work ran but did not complete. It is a
	// permanent failure for the purpose of dependent propagation
	// (spec 076 FR-011).
	StatusFailed
	// StatusBlockedUnroutable means no driver satisfies the node's required
	// capability (spec 076 FR-010). It is terminal — the node will not run.
	StatusBlockedUnroutable
	// StatusBlockedDependencyFailed means a transitive dependency permanently
	// failed, so this node can never run (spec 076 FR-011). It is terminal.
	StatusBlockedDependencyFailed
)

// nodeStatusNames is indexed by NodeStatus; kept in sync with the constants
// above. The names are the spec's hyphenated wire forms.
var nodeStatusNames = [...]string{
	StatusPending:                 "pending",
	StatusRunnable:                "runnable",
	StatusRunning:                 "running",
	StatusDone:                    "done",
	StatusFailed:                  "failed",
	StatusBlockedUnroutable:       "blocked-unroutable",
	StatusBlockedDependencyFailed: "blocked-dependency-failed",
}

// String renders the status as its declared spec name. An out-of-range
// NodeStatus renders as "status(N)" rather than panicking.
func (s NodeStatus) String() string {
	if int(s) < 0 || int(s) >= len(nodeStatusNames) {
		return "status(" + itoa(int(s)) + ")"
	}
	return nodeStatusNames[s]
}

// Valid reports whether s is one of the declared node statuses.
func (s NodeStatus) Valid() bool {
	return int(s) >= 0 && int(s) < len(nodeStatusNames)
}

// Terminal reports whether s is a settled state from which the node will
// never transition again — done, failed, or either blocked state. A node in
// a terminal state is never re-evaluated for runnability.
func (s NodeStatus) Terminal() bool {
	switch s {
	case StatusDone, StatusFailed, StatusBlockedUnroutable, StatusBlockedDependencyFailed:
		return true
	default:
		return false
	}
}

// NodeKind classifies a node by HOW its work runs — the "workflows over
// agents" distinction made part of the DAG schema (spec 076 FR-017). It is a
// closed enumeration of exactly two kinds:
//
//   - NodeKindAgent: genuinely ambiguous coding work — routed to a
//     capability-matched driver (spec 075) and run as an agent invocation.
//   - NodeKindDeterministic: a mappable mechanical step — gofmt, go test, a
//     lint pass, a version bump — run as a plain Temporal activity with no
//     driver and no token cost.
//
// NodeKindAgent is the zero value, so a Node — or a serialized DAG — that
// declares no kind is an agent node; the deterministic kind is a strictly
// backward-compatible addition (spec 076 FR-017).
type NodeKind int

const (
	// NodeKindAgent is the zero value — the node is genuinely ambiguous
	// coding work, routed to a driver by its Capability (spec 076 FR-007).
	NodeKindAgent NodeKind = iota
	// NodeKindDeterministic means the node is a mappable mechanical step; it
	// runs as a plain Temporal activity over its Command, never a driver, and
	// costs no agent tokens (spec 076 FR-017).
	NodeKindDeterministic
)

// nodeKindNames is indexed by NodeKind; kept in sync with the constants
// above. The names are the spec's wire forms.
var nodeKindNames = [...]string{
	NodeKindAgent:         "agent",
	NodeKindDeterministic: "deterministic",
}

// String renders the kind as its declared spec name. An out-of-range
// NodeKind renders as "kind(N)" rather than panicking.
func (k NodeKind) String() string {
	if int(k) < 0 || int(k) >= len(nodeKindNames) {
		return "kind(" + itoa(int(k)) + ")"
	}
	return nodeKindNames[k]
}

// Valid reports whether k is one of the declared node kinds.
func (k NodeKind) Valid() bool {
	return int(k) >= 0 && int(k) < len(nodeKindNames)
}

// Node is one work unit in the DAG — the smallest dispatchable piece of work
// (spec 076 Key Entities: DAG Node). It carries the routing inputs the
// scheduler needs to select a driver and create a worktree, plus its
// lifecycle status. A Node is a value; the DAG owns the canonical copy and
// callers mutate status only through DAG.SetStatus.
//
// A node's Kind decides HOW it runs (spec 076 FR-017): an agent node is
// routed by Capability to a driver; a deterministic node instead runs its
// Command as a plain Temporal activity. The two kinds use disjoint fields —
// Capability and Description are the agent-node routing key and instruction,
// Command/Args the deterministic-node step spec — though a node may carry both
// for telemetry context.
type Node struct {
	// ID is the stable, DAG-unique node identifier. It is the dependency-edge
	// key and the final, stable tie-breaker in frontier ordering
	// (spec 076 FR-004). It MUST be non-empty and unique within a DAG.
	ID string `json:"id"`
	// SpecRef is the source spec the work unit derives from — e.g. "076".
	SpecRef string `json:"spec_ref"`
	// TaskRef is the task within the spec — e.g. "T006"; empty if the node is
	// not task-scoped.
	TaskRef string `json:"task_ref"`
	// Kind is how the node's work runs (spec 076 FR-017): NodeKindAgent —
	// ambiguous coding work routed to a driver — or NodeKindDeterministic — a
	// mechanical step run as a plain activity. The zero value is
	// NodeKindAgent, so a node that omits the field is an agent node; the
	// deterministic kind is a backward-compatible addition.
	Kind NodeKind `json:"kind"`
	// Capability is the capability tag a driver MUST declare to be eligible
	// for this node (spec 076 FR-007). It is the routing key for an
	// NodeKindAgent node; a node whose capability no driver satisfies becomes
	// StatusBlockedUnroutable. It is unused for routing on a
	// NodeKindDeterministic node (which carries Command instead) but may be
	// set for telemetry context.
	Capability string `json:"capability"`
	// Command is the mechanical command an NodeKindDeterministic node runs —
	// the program name of the deterministic step (e.g. "gofmt", "go"). It is
	// the deterministic-node analogue of Capability and is ignored for an
	// NodeKindAgent node. A deterministic node with an empty Command cannot
	// run and is settled failed (spec 076 FR-017 edge case).
	Command string `json:"command,omitempty"`
	// Args are the arguments passed to Command for an NodeKindDeterministic
	// node — e.g. ["test", "./..."] for `go test ./...`. Ignored for an
	// NodeKindAgent node.
	Args []string `json:"args,omitempty"`
	// Description is the human- and driver-facing instruction for an
	// NodeKindAgent node — the task's one-line description as written in the
	// source kit artifact. It is the agent-node analogue of an
	// NodeKindDeterministic node's Command/Args: the irreducible statement of
	// what the work unit must do. The scheduler carries it to the driver so a
	// dispatched agent acts on the task itself, not merely its id. Empty for a
	// deterministic node, and for an agent node whose adapter supplied none.
	Description string `json:"description,omitempty"`
	// Priority orders the runnable frontier: higher priority is dispatched
	// first (spec 076 FR-004). It is a declared property of the node, never
	// inferred heuristically (spec 070 FR-015). Ties on priority are broken
	// by ID.
	Priority int `json:"priority"`
	// TierHint is an optional preferred driver tier for the work — advisory
	// input to driver selection. Zero means no hint.
	TierHint int `json:"tier_hint"`
	// TargetRepo is the repository the work unit operates on (spec 076
	// FR-013). It is an input, never hard-coded — the same scheduler runs
	// work over any repo.
	TargetRepo string `json:"target_repo"`
	// BaseRef is the git ref the work unit's worktree is created from
	// (spec 076 FR-013).
	BaseRef string `json:"base_ref"`
	// WorktreeRequired is true if the work unit MUST run in a dedicated fresh
	// git worktree (spec 076 FR-008; 070 FR-013/14).
	WorktreeRequired bool `json:"worktree_required"`
	// Status is the node's lifecycle state. A freshly compiled node is
	// StatusPending (the zero value).
	Status NodeStatus `json:"status"`
}

// Equal reports whether two nodes are field-for-field equal. It exists because
// the Args slice makes Node uncomparable with == (a struct containing a slice
// is not a comparable type); Equal restores a total equality so callers — and
// the package's own determinism tests — can compare two builds of the same
// node. Two nodes are equal iff every scalar field matches and their Args
// slices are element-wise equal (a nil and an empty Args are treated equal).
func (n Node) Equal(other Node) bool {
	if n.ID != other.ID ||
		n.SpecRef != other.SpecRef ||
		n.TaskRef != other.TaskRef ||
		n.Kind != other.Kind ||
		n.Capability != other.Capability ||
		n.Command != other.Command ||
		n.Description != other.Description ||
		n.Priority != other.Priority ||
		n.TierHint != other.TierHint ||
		n.TargetRepo != other.TargetRepo ||
		n.BaseRef != other.BaseRef ||
		n.WorktreeRequired != other.WorktreeRequired ||
		n.Status != other.Status {
		return false
	}
	if len(n.Args) != len(other.Args) {
		return false
	}
	for i := range n.Args {
		if n.Args[i] != other.Args[i] {
			return false
		}
	}
	return true
}

// Edge is a depends_on dependency relation: the node named by From depends on
// the node named by To, so To must reach StatusDone before From can become
// runnable (spec 076 Key Entities: Dependency Edge). The transitive closure
// of all edges in a DAG MUST be acyclic — see Acyclic.
type Edge struct {
	// From is the ID of the dependent node — the node that waits.
	From string `json:"from"`
	// To is the ID of the dependency node — the node that must finish first.
	To string `json:"to"`
}

// DAG is the work-unit dependency graph — the scheduler's input contract
// (spec 076 Key Entities: Work-Unit DAG). It is the normalized form every
// spec-kit adapter (spec 077) must produce: a set of nodes plus depends_on
// edges whose transitive closure is acyclic.
//
// A DAG is mutable only through its methods; the nodes map and edges slice
// are unexported so every read goes through an accessor that imposes a
// deterministic order. The empty DAG (zero nodes) is a valid, acyclic DAG
// with an empty frontier.
type DAG struct {
	// nodes maps node ID to the canonical Node value. Map iteration order is
	// never relied upon — every accessor sorts.
	nodes map[string]Node
	// edges is the depends_on relation. It is kept as a slice (not a set of
	// maps) so iteration is deterministic; AddEdge dedupes.
	edges []Edge
}

// New returns an empty DAG — zero nodes, zero edges. The empty DAG is valid
// and acyclic; its runnable frontier is empty.
func New() *DAG {
	return &DAG{nodes: make(map[string]Node)}
}

// AddNode inserts n into the DAG. It returns an error if n.ID is empty or if
// a node with the same ID already exists — IDs are the DAG's primary key and
// must be unique. The inserted node is stored by value; later mutation of the
// caller's copy does not affect the DAG.
func (d *DAG) AddNode(n Node) error {
	if n.ID == "" {
		return fmt.Errorf("dag: node ID must not be empty")
	}
	if _, exists := d.nodes[n.ID]; exists {
		return fmt.Errorf("dag: duplicate node ID %q", n.ID)
	}
	d.nodes[n.ID] = n
	return nil
}

// AddEdge records a depends_on edge: from depends on to. It returns an error
// if either ID is empty or if from == to (a node cannot depend on itself — a
// trivial cycle). It does NOT verify that the endpoints exist; missing
// endpoints are reported by Acyclic as dangling edges, so an adapter may add
// edges and nodes in any order. A duplicate edge is silently ignored — the
// relation is a set.
func (d *DAG) AddEdge(from, to string) error {
	if from == "" || to == "" {
		return fmt.Errorf("dag: edge endpoints must not be empty (from=%q to=%q)", from, to)
	}
	if from == to {
		return fmt.Errorf("dag: node %q cannot depend on itself", from)
	}
	for _, e := range d.edges {
		if e.From == from && e.To == to {
			return nil
		}
	}
	d.edges = append(d.edges, Edge{From: from, To: to})
	return nil
}

// Node returns the node with the given ID and whether it exists. The returned
// Node is a copy; mutating it does not affect the DAG — use SetStatus to
// change a node's lifecycle state.
func (d *DAG) Node(id string) (Node, bool) {
	n, ok := d.nodes[id]
	return n, ok
}

// Len reports the number of nodes in the DAG.
func (d *DAG) Len() int { return len(d.nodes) }

// Nodes returns every node, ordered deterministically by ID ascending. It
// allocates a fresh slice on each call and never exposes the backing map, so
// the result has no dependence on map iteration order.
func (d *DAG) Nodes() []Node {
	out := make([]Node, 0, len(d.nodes))
	for _, n := range d.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Edges returns every depends_on edge, ordered deterministically by (From,
// To) ascending. It allocates a fresh slice on each call; the result has no
// dependence on insertion order beyond the dedupe AddEdge already applied.
func (d *DAG) Edges() []Edge {
	out := make([]Edge, len(d.edges))
	copy(out, d.edges)
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].To < out[j].To
	})
	return out
}

// SetStatus updates the lifecycle status of the node with the given ID. It
// returns an error if no such node exists or if status is not a valid
// NodeStatus — an invalid status must never enter the graph.
func (d *DAG) SetStatus(id string, status NodeStatus) error {
	if !status.Valid() {
		return fmt.Errorf("dag: invalid node status %d for node %q", int(status), id)
	}
	n, ok := d.nodes[id]
	if !ok {
		return fmt.Errorf("dag: no such node %q", id)
	}
	n.Status = status
	d.nodes[id] = n
	return nil
}

// Dependencies returns the IDs of the nodes the given node directly depends
// on — the To endpoint of every edge whose From is id — sorted ascending. The
// result is deterministic and allocates fresh on each call. A node with no
// dependencies (a root) yields an empty slice. Missing endpoints are not
// filtered here; Acyclic is responsible for dangling-edge detection.
func (d *DAG) Dependencies(id string) []string {
	var deps []string
	for _, e := range d.edges {
		if e.From == id {
			deps = append(deps, e.To)
		}
	}
	sort.Strings(deps)
	return deps
}

// Dependents returns the IDs of the nodes that directly depend on the given
// node — the From endpoint of every edge whose To is id — sorted ascending.
// The result is deterministic and allocates fresh on each call.
func (d *DAG) Dependents(id string) []string {
	var deps []string
	for _, e := range d.edges {
		if e.To == id {
			deps = append(deps, e.From)
		}
	}
	sort.Strings(deps)
	return deps
}

// itoa is a tiny strconv.Itoa avoiding an import in the String fallback.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
