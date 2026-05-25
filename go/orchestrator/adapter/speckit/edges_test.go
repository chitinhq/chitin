package speckit

import (
	"testing"
)

// TestFileOverlapEdgesParallel asserts spec 112 FR-002: two parallel siblings
// whose descriptions cite the same backtick-quoted file get an injected
// depends_on edge — the later task depends on the earlier so its driver sees
// the earlier task's writes after merge.
func TestFileOverlapEdgesParallel(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true,
			Description: "Implement the feature in `foo.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true,
			Description: "Extend the feature in `foo.go`", LineNo: 2},
	}
	edges, dangling := DeriveEdges(tasks)
	if len(dangling) != 0 {
		t.Fatalf("unexpected dangling references: %+v", dangling)
	}
	if !hasEdge(edges, "T002", "T001") {
		t.Errorf("expected T002→T001 file-overlap edge, got %+v", edges)
	}
}

// TestFileOverlapEdgesDisjoint asserts parallel siblings with disjoint file
// scopes keep their parallelism — no overlap edge is injected.
func TestFileOverlapEdgesDisjoint(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true,
			Description: "Implement the feature in `foo.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true,
			Description: "Implement the other feature in `bar.go`", LineNo: 2},
	}
	edges, _ := DeriveEdges(tasks)
	if hasEdge(edges, "T002", "T001") || hasEdge(edges, "T001", "T002") {
		t.Errorf("unexpected edge between disjoint parallel tasks: %+v", edges)
	}
}

// TestFileOverlapEdgesTransitive asserts three parallel siblings touching the
// same file get the full pairwise chain — T2→T1, T3→T1, T3→T2 — which
// serializes them while still leaving the DAG acyclic.
func TestFileOverlapEdgesTransitive(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true,
			Description: "First write to `foo.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true,
			Description: "Second write to `foo.go`", LineNo: 2},
		{ID: "T003", Num: 3, Parallel: true,
			Description: "Third write to `foo.go`", LineNo: 3},
	}
	edges, _ := DeriveEdges(tasks)
	for _, want := range [][2]string{{"T002", "T001"}, {"T003", "T001"}, {"T003", "T002"}} {
		if !hasEdge(edges, want[0], want[1]) {
			t.Errorf("expected %s→%s edge among %+v", want[0], want[1], edges)
		}
	}
}

// TestFileOverlapEdgesBareVsFullPath asserts overlap is detected when one
// task cites the full repo-relative path and a sibling cites just the
// basename — the dogfood pattern seen on spec 109 (T001 cited the full
// `go/orchestrator/driver/claudecode/review_mode.go`; T002 cited bare
// `review_mode.go` — same file, different strings). Without basename
// normalization the rule misses the overlap and lets the two PRs race.
func TestFileOverlapEdgesBareVsFullPath(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true,
			Description: "Implement in `go/orchestrator/driver/claudecode/review_mode.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true,
			Description: "Extend in `review_mode.go`", LineNo: 2},
	}
	edges, _ := DeriveEdges(tasks)
	if !hasEdge(edges, "T002", "T001") {
		t.Errorf("expected T002→T001 edge from bare-vs-full-path overlap, got %+v", edges)
	}
}

// TestFileOverlapEdgesDifferentDirsSameBasename asserts the basename-
// normalization tradeoff: two parallel tasks naming files with the same
// basename in different directories (e.g. `pkg/a/index.ts` and
// `pkg/b/index.ts`) are treated as overlapping and serialized. Over-
// serialization is the conservative side of the tradeoff — slower but still
// correct, versus false-negative collisions which are spec 112's whole
// motivation.
func TestFileOverlapEdgesDifferentDirsSameBasename(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true,
			Description: "Implement `pkg/a/index.ts`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true,
			Description: "Implement `pkg/b/index.ts`", LineNo: 2},
	}
	edges, _ := DeriveEdges(tasks)
	if !hasEdge(edges, "T002", "T001") {
		t.Errorf("expected basename-collision serialization on shared `index.ts`, got %+v", edges)
	}
}

// TestFileOverlapEdgesMultiFile asserts overlap is detected when sets
// intersect on any path, not just on a single shared path: T1 cites
// `foo.go`+`bar.go`, T2 cites `bar.go`+`baz.go` — they share `bar.go`.
func TestFileOverlapEdgesMultiFile(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true,
			Description: "Edit `foo.go` and `bar.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true,
			Description: "Edit `bar.go` and `baz.go`", LineNo: 2},
	}
	edges, _ := DeriveEdges(tasks)
	if !hasEdge(edges, "T002", "T001") {
		t.Errorf("expected T002→T001 edge from bar.go overlap, got %+v", edges)
	}
}

// TestFileOverlapEdgesSequentialUnaffected asserts the new rule only fires on
// `[P]` tasks — two sequential tasks already chain via the barrier rule and
// don't pick up a redundant overlap edge from this pass. (The pre-existing
// ordering rule still produces its T002→T001 edge, separately.)
func TestFileOverlapEdgesSequentialUnaffected(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: false,
			Description: "Sequential write to `foo.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: false,
			Description: "Sequential edit to `foo.go`", LineNo: 2},
	}
	// Drop the file-overlap helper directly so we observe only its output.
	overlapEdges := deriveFileOverlapEdges(tasks)
	if len(overlapEdges) != 0 {
		t.Errorf("file-overlap pass added edges between sequential tasks: %+v", overlapEdges)
	}
}

// TestFileOverlapEdgesEmptyScopeNoEdge asserts a task whose description names
// no backtick-quoted file contributes no scope — and therefore no overlap
// edge — even when its sibling does name a file. "No scope declared" must not
// be treated as "scope = empty"; serializing every undeclared task would be
// over-aggressive.
func TestFileOverlapEdgesEmptyScopeNoEdge(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true,
			Description: "Implement the feature in `foo.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true,
			Description: "Implement a related thing — no file cited", LineNo: 2},
	}
	overlapEdges := deriveFileOverlapEdges(tasks)
	if len(overlapEdges) != 0 {
		t.Errorf("unexpected overlap edges when one task has no scope: %+v", overlapEdges)
	}
}

// hasEdge reports whether the slice contains the given (from, to) edge.
func hasEdge(edges []derivedEdge, from, to string) bool {
	for _, e := range edges {
		if e.from == from && e.to == to {
			return true
		}
	}
	return false
}

// TestFileOverlapEdgesDeterministic asserts the injected edges come out in
// the same order on every run — a precondition for compilation determinism
// the kit's plan.md Constraints require. The expected order is dependent-
// task file order: T2's overlap edges before T3's; within one dependent
// task, oldest dependency first.
func TestFileOverlapEdgesDeterministic(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true, Description: "Edit `a.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true, Description: "Edit `a.go`", LineNo: 2},
		{ID: "T003", Num: 3, Parallel: true, Description: "Edit `a.go`", LineNo: 3},
	}
	want := []string{"T002→T001", "T003→T001", "T003→T002"}
	first := edgePairs(deriveFileOverlapEdges(tasks))
	if !sliceEqual(first, want) {
		t.Fatalf("first run order = %v, want %v", first, want)
	}
	for i := 0; i < 5; i++ {
		got := edgePairs(deriveFileOverlapEdges(tasks))
		if !sliceEqual(first, got) {
			t.Fatalf("iteration %d differs from first run: %v vs %v", i, first, got)
		}
	}
}

// edgePairs renders the edges in their original slice order — no sort. The
// test relies on the order produced by the helper, not on set equality.
func edgePairs(edges []derivedEdge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.from+"→"+e.to)
	}
	return out
}

func sliceEqual(a, b []string) bool {
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
