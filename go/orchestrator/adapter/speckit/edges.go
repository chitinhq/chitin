package speckit

import (
	"regexp"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
)

// explicitDepRe matches an explicit dependency citation a task description
// may carry — "depends on T004", "depends on T004 and T006", "(after T003)".
// It is intentionally narrow: only an unambiguous "depends on"/"after"
// followed by task ids counts as an explicit edge. Anything looser is left to
// the ordering rule and never invents an edge.
var explicitDepRe = regexp.MustCompile(`(?i)\b(?:depends on|after)\s+((?:T\d+(?:\s*(?:,|and|&)\s*)?)+)`)

// taskIDRe extracts every TNNN token from a fragment.
var taskIDRe = regexp.MustCompile(`T\d+`)

// ambiguousDepRe matches a dependency *phrase* — "depends on", "requires",
// "blocked by", "needs", "after" — that names no task id. A task whose
// description signals a dependency in prose but identifies no target leaves
// its dependency ambiguous: the adapter must mark the node NEEDS
// CLARIFICATION rather than invent an edge or silently drop the hint
// (FR-009). The trailing lookahead rejects the phrase when an explicit TNNN
// token follows, so explicitDepRe — not this — handles a resolvable
// citation.
var ambiguousDepRe = regexp.MustCompile(`(?i)\b(?:depends on|depend on|blocked by|requires|needs)\b`)

// dependencyClarification is the clarification reason recorded on a node
// whose description signals a dependency the kit's artifacts do not resolve
// to a concrete task. Stable text so tests and operators can match on it.
const dependencyClarification = "task description signals a dependency that names no resolvable task id"

// HasAmbiguousDependency reports whether a task description signals a
// dependency in prose but cites no resolvable task id (FR-009). It returns
// true only when a dependency phrase is present AND explicitDeps found no
// task id — a description that explicitly names its dependencies is not
// ambiguous, and one that mentions no dependency at all is governed by the
// ordering rule.
func HasAmbiguousDependency(description string) bool {
	if len(explicitDeps(description)) > 0 {
		return false
	}
	return ambiguousDepRe.MatchString(description)
}

// derivedEdge is one depends_on relation the edge pass produced, kept with
// the source task line so a dangling reference can be reported precisely.
type derivedEdge struct {
	from   string // dependent task id
	to     string // dependency task id
	lineNo int    // line of the `from` task, for error locations
}

// DeriveEdges computes the depends_on edges for a parsed spec-kit task list
// (FR-004). It applies two rules, in this order:
//
//  1. Explicit citation. A task description that says "depends on TNNN" /
//     "after TNNN" gets an edge to each cited task. An explicit citation
//     naming a task id that is not in the list is a dangling reference and
//     is returned as such — the caller fails compilation (FR-011).
//
//  2. tasks.md ordering + `[P]` markers. With no explicit citation, the
//     spec-kit convention is: a sequential (non-`[P]`) task depends on the
//     work before it; `[P]` tasks within a phase are parallel siblings.
//     Concretely, each task depends on the most recent preceding
//     *sequential* (non-`[P]`) task in the same phase — that task is the
//     phase's running "barrier". A run of `[P]` tasks therefore all depend
//     on the same barrier and on nothing from each other, making them
//     siblings; the next sequential task depends on the barrier and then
//     itself becomes the new barrier.
//
// tasks given to DeriveEdges MUST already be in file order (ParseTasks
// guarantees this). The returned edges are in dependent-task file order; the
// returned dangling slice, when non-empty, means compilation must fail.
//
// DeriveEdges never adds an edge to a non-existent task on the ordering rule
// — ordering only ever points at a task already seen in the same list — so
// the only dangling references it can surface come from explicit citations.
func DeriveEdges(tasks []Task) (edges []derivedEdge, dangling []adapter.DanglingReferenceError) {
	exists := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		exists[t.ID] = struct{}{}
	}

	// Spec 112 FR-002: pre-compute every parallel task's file scope once so
	// each iteration of the main loop can emit its overlap edges in-place,
	// preserving the dependent-task file order this function's contract
	// promises. A task absent from scopes is sequential or named no backticked
	// path.
	scopes := fileScopes(tasks)

	// barrierByPhase tracks, per phase ordinal, the id of the most recent
	// sequential (non-[P]) task — the task a following task implicitly
	// depends on. Phase 0 (pre-phase tasks) gets its own barrier.
	barrierByPhase := make(map[int]string)

	for i, t := range tasks {
		explicit := explicitDeps(t.Description)
		if len(explicit) > 0 {
			for _, dep := range explicit {
				if _, ok := exists[dep]; !ok {
					dangling = append(dangling, adapter.DanglingReferenceError{
						From: t.ID, MissingTarget: dep,
					})
					continue
				}
				if dep != t.ID {
					edges = append(edges, derivedEdge{from: t.ID, to: dep, lineNo: t.LineNo})
				}
			}
			// An explicitly-cited task still advances the phase barrier if it
			// is sequential, so later tasks chain correctly.
			if !t.Parallel {
				barrierByPhase[t.PhaseSeq] = t.ID
			}
		} else {
			// Ordering rule: depend on the current phase barrier, if any.
			if barrier, ok := barrierByPhase[t.PhaseSeq]; ok && barrier != t.ID {
				edges = append(edges, derivedEdge{from: t.ID, to: barrier, lineNo: t.LineNo})
			}
			// A sequential task becomes the new barrier; a [P] task does
			// not, so the next [P] task remains its sibling rather than
			// chaining off it.
			if !t.Parallel {
				barrierByPhase[t.PhaseSeq] = t.ID
			}
		}

		// Spec 112 FR-002: file-overlap edges from t to every prior `[P]`
		// task whose file scope overlaps t's. Emitted here, in the main
		// loop, so they land in dependent-task file order alongside the
		// ordering/explicit edges.
		edges = append(edges, fileOverlapEdgesAt(tasks, i, scopes)...)
	}

	return edges, dangling
}

// fileScopes returns the set of repo file paths each parallel task names in
// its description, keyed by task ID. Tasks not present in the result are
// sequential (no file-overlap rule applies) or `[P]` but named no backticked
// path (scope unknown — see deriveFileOverlapEdges for rationale).
func fileScopes(tasks []Task) map[string]map[string]struct{} {
	scopes := make(map[string]map[string]struct{}, len(tasks))
	for _, t := range tasks {
		if !t.Parallel {
			continue
		}
		paths := adapter.ExtractFilePaths(t.Description)
		if len(paths) == 0 {
			continue
		}
		s := make(map[string]struct{}, len(paths))
		for _, p := range paths {
			s[p] = struct{}{}
		}
		scopes[t.ID] = s
	}
	return scopes
}

// fileOverlapEdgesAt returns the depends_on edges from tasks[i] to every
// PRIOR `[P]` task whose file scope overlaps tasks[i]'s. Edges come out in
// dependency-task file order (the earliest overlapping prior task first), so
// the caller can append them and preserve a dependent-task-file-ordered
// edges slice. Spec 112 FR-002.
//
// tasks[i] not having a file scope (sequential, or `[P]` with no backticked
// path) produces no edges — there is nothing to overlap with.
func fileOverlapEdgesAt(tasks []Task, i int, scopes map[string]map[string]struct{}) []derivedEdge {
	tb := tasks[i]
	sb, ok := scopes[tb.ID]
	if !ok {
		return nil
	}
	var out []derivedEdge
	for _, ta := range tasks[:i] {
		sa, ok := scopes[ta.ID]
		if !ok {
			continue
		}
		if filesOverlap(sa, sb) {
			out = append(out, derivedEdge{from: tb.ID, to: ta.ID, lineNo: tb.LineNo})
		}
	}
	return out
}

// deriveFileOverlapEdges returns ALL file-overlap edges across tasks in
// dependent-task file order (spec 112 FR-002). It is a convenience that
// applies fileOverlapEdgesAt across every task; DeriveEdges does the same
// work in-line in its main loop. Kept as a separately-testable helper so the
// rule can be exercised without the surrounding ordering/explicit machinery.
//
// A task's file scope is the set of repo paths cited in backticks in its
// description (adapter.ExtractFilePaths). A task whose description names no
// path contributes no scope and gets no overlap-derived edge — file overlap
// is only injected when BOTH tasks declare a scope. (Description without
// paths is treated as "scope unknown" rather than "scope empty"; serializing
// every such task would be over-aggressive. Spec author should name the file
// in backticks to opt in.)
//
// Three [P] siblings all touching the same file produce the full pairwise
// chain — T2→T1, T3→T1, T3→T2 — which fully serializes them. The redundant
// T3→T1 edge is harmless: the DAG's AddEdge dedups same-pair edges and the
// scheduler tolerates redundant transitive edges, so we do not minimize the
// set here.
func deriveFileOverlapEdges(tasks []Task) []derivedEdge {
	scopes := fileScopes(tasks)
	var out []derivedEdge
	for i := range tasks {
		out = append(out, fileOverlapEdgesAt(tasks, i, scopes)...)
	}
	return out
}

// filesOverlap reports whether two file-path sets share at least one path.
func filesOverlap(a, b map[string]struct{}) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for p := range a {
		if _, hit := b[p]; hit {
			return true
		}
	}
	return false
}

// explicitDeps returns the de-duplicated task ids a description explicitly
// cites as dependencies, in citation order. An empty result means the task
// declared no explicit dependency and the ordering rule applies.
func explicitDeps(description string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, m := range explicitDepRe.FindAllStringSubmatch(description, -1) {
		for _, id := range taskIDRe.FindAllString(m[1], -1) {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}
