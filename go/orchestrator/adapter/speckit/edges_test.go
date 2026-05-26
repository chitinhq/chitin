package speckit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
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
	if got := edgeReason(edges, "T002", "T001"); got != EdgeReasonFileOverlap {
		t.Errorf("T002→T001 reason = %q, want %q", got, EdgeReasonFileOverlap)
	}
}

// TestFileOverlapEdgesDisjoint asserts parallel siblings with disjoint file
// scopes keep their parallelism — no overlap edge is injected.
func TestFileOverlapEdgesDisjoint(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true,
			Description: "Implement the feature in `pkg/foo/foo.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true,
			Description: "Implement the other feature in `pkg/bar/bar.go`", LineNo: 2},
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

func TestSameDirectoryOverlapEdges(t *testing.T) {
	tests := []struct {
		name      string
		tasks     []Task
		wantEdges map[string]EdgeReason
	}{
		{
			name: "different files same dir",
			tasks: []Task{
				{ID: "T001", Num: 1, Parallel: true, Description: "Edit `pkg/foo/a.go`", LineNo: 1},
				{ID: "T002", Num: 2, Parallel: true, Description: "Edit `pkg/foo/b.go`", LineNo: 2},
			},
			wantEdges: map[string]EdgeReason{"T002→T001": EdgeReasonSameDirectoryOverlap},
		},
		{
			name: "different dirs",
			tasks: []Task{
				{ID: "T001", Num: 1, Parallel: true, Description: "Edit `pkg/foo/a.go`", LineNo: 1},
				{ID: "T002", Num: 2, Parallel: true, Description: "Edit `pkg/bar/b.go`", LineNo: 2},
			},
		},
		{
			name: "same basename wins",
			tasks: []Task{
				{ID: "T001", Num: 1, Parallel: true, Description: "Edit `pkg/foo/a.go`", LineNo: 1},
				{ID: "T002", Num: 2, Parallel: true, Description: "Edit `pkg/foo/a.go`", LineNo: 2},
			},
			wantEdges: map[string]EdgeReason{"T002→T001": EdgeReasonFileOverlap},
		},
		{
			name: "sequential tasks",
			tasks: []Task{
				{ID: "T001", Num: 1, Description: "Edit `pkg/foo/a.go`", LineNo: 1},
				{ID: "T002", Num: 2, Description: "Edit `pkg/foo/b.go`", LineNo: 2},
			},
		},
		{
			name: "empty scope",
			tasks: []Task{
				{ID: "T001", Num: 1, Parallel: true, Description: "Edit `pkg/foo/a.go`", LineNo: 1},
				{ID: "T002", Num: 2, Parallel: true, Description: "Implement related behavior", LineNo: 2},
			},
		},
		{
			name: "leading dot slash",
			tasks: []Task{
				{ID: "T001", Num: 1, Parallel: true, Description: "Edit `./pkg/foo/a.go`", LineNo: 1},
				{ID: "T002", Num: 2, Parallel: true, Description: "Edit `pkg/foo/b.go`", LineNo: 2},
			},
			wantEdges: map[string]EdgeReason{"T002→T001": EdgeReasonSameDirectoryOverlap},
		},
		{
			name: "multiple directories",
			tasks: []Task{
				{ID: "T001", Num: 1, Parallel: true, Description: "Edit `pkg/foo/a.go` and `cmd/app/main.go`", LineNo: 1},
				{ID: "T002", Num: 2, Parallel: true, Description: "Edit `cmd/app/flags.go`", LineNo: 2},
			},
			wantEdges: map[string]EdgeReason{"T002→T001": EdgeReasonSameDirectoryOverlap},
		},
		{
			name: "directory mention only out of scope",
			tasks: []Task{
				{ID: "T001", Num: 1, Parallel: true, Description: "Refactor files in `internal/queue/`", LineNo: 1},
				{ID: "T002", Num: 2, Parallel: true, Description: "Edit `internal/queue/types.go`", LineNo: 2},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveFileOverlapEdges(tc.tasks)
			if len(tc.wantEdges) == 0 {
				if len(got) != 0 {
					t.Fatalf("edges = %+v, want none", got)
				}
				return
			}
			if len(got) != len(tc.wantEdges) {
				t.Fatalf("edges = %+v, want %d edge(s)", got, len(tc.wantEdges))
			}
			for pair, reason := range tc.wantEdges {
				if gotReason := edgeReasonByPair(got, pair); gotReason != reason {
					t.Errorf("%s reason = %q, want %q; edges=%+v", pair, gotReason, reason, got)
				}
			}
		})
	}
}

func TestSameDirectoryOverlapReasonLiteral(t *testing.T) {
	if string(EdgeReasonSameDirectoryOverlap) != "same_directory_overlap" {
		t.Fatalf("EdgeReasonSameDirectoryOverlap = %q, want same_directory_overlap", EdgeReasonSameDirectoryOverlap)
	}

	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true, Description: "Edit `pkg/foo/a.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true, Description: "Edit `pkg/foo/b.go`", LineNo: 2},
	}
	edges := deriveFileOverlapEdges(tasks)
	if got := edgeReason(edges, "T002", "T001"); string(got) != "same_directory_overlap" {
		t.Fatalf("T002→T001 reason = %q, want same_directory_overlap", got)
	}
}

func TestSameDirectoryOverlapSpec114QueueScenario(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true, Description: "Implement `internal/queue/format_table.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true, Description: "Implement `internal/queue/format_md.go`", LineNo: 2},
		{ID: "T003", Num: 3, Parallel: true, Description: "Implement `internal/queue/format_json.go`", LineNo: 3},
	}
	edges := deriveFileOverlapEdges(tasks)
	want := []string{"T002→T001", "T003→T001", "T003→T002"}
	if got := edgePairs(edges); !sliceEqual(got, want) {
		t.Fatalf("edge order = %v, want %v", got, want)
	}
	for _, pair := range want {
		if got := edgeReasonByPair(edges, pair); string(got) != "same_directory_overlap" {
			t.Errorf("%s reason = %q, want same_directory_overlap", pair, got)
		}
	}
}

func TestSameDirectoryOverlapSpec115SpeclintScenario(t *testing.T) {
	tasks := []Task{
		{ID: "T001", Num: 1, Parallel: true, Description: "Implement `internal/speclint/l03_task_fr.go`", LineNo: 1},
		{ID: "T002", Num: 2, Parallel: true, Description: "Implement `internal/speclint/l04_events.go`", LineNo: 2},
	}
	edges := deriveFileOverlapEdges(tasks)
	if got := edgeReason(edges, "T002", "T001"); string(got) != "same_directory_overlap" {
		t.Fatalf("T002→T001 reason = %q, want same_directory_overlap; edges=%+v", got, edges)
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

func edgeReason(edges []derivedEdge, from, to string) EdgeReason {
	for _, e := range edges {
		if e.from == from && e.to == to {
			return e.Reason
		}
	}
	return ""
}

func edgeReasonByPair(edges []derivedEdge, pair string) EdgeReason {
	for _, e := range edges {
		if e.from+"→"+e.to == pair {
			return e.Reason
		}
	}
	return ""
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

func TestDeriveEdgesShippedSpecsSupersetPriorInference(t *testing.T) {
	root := findRepoRoot(t)
	specDirs := shippedSpecDirs(t, root)
	if len(specDirs) == 0 {
		t.Fatal("no shipped spec task files found")
	}

	for _, specDir := range specDirs {
		t.Run(filepath.Base(specDir), func(t *testing.T) {
			tasksPath := filepath.Join(specDir, "tasks.md")
			content, err := os.ReadFile(tasksPath)
			if err != nil {
				t.Fatalf("read tasks.md: %v", err)
			}
			rel, err := filepath.Rel(root, tasksPath)
			if err != nil {
				t.Fatalf("rel tasks.md: %v", err)
			}
			tasks, err := ParseTasks(rel, string(content))
			if err != nil {
				t.Skipf("tasks.md does not parse: %v", err)
			}

			prior, priorDangling := deriveEdgesPriorFileOverlap(tasks)
			current, currentDangling := DeriveEdges(tasks)
			if len(priorDangling) != 0 || len(currentDangling) != 0 {
				t.Skipf("tasks.md has dangling references: prior=%+v current=%+v", priorDangling, currentDangling)
			}

			currentSet := edgeSet(current)
			for _, e := range prior {
				pair := e.from + "→" + e.to
				if _, ok := currentSet[pair]; !ok {
					t.Fatalf("current edges are not a superset: missing %s from prior edges", pair)
				}
			}
		})
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".specify")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root containing .specify")
		}
		dir = parent
	}
}

func shippedSpecDirs(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for i := 89; i <= 116; i++ {
		if i == 99 {
			continue
		}
		pattern := filepath.Join(root, ".specify", "specs", fmt.Sprintf("%03d-*", i))
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob %q: %v", pattern, err)
		}
		for _, match := range matches {
			if info, err := os.Stat(filepath.Join(match, "tasks.md")); err == nil && !info.IsDir() {
				out = append(out, match)
			}
		}
	}
	return out
}

func deriveEdgesPriorFileOverlap(tasks []Task) (edges []derivedEdge, dangling []adapter.DanglingReferenceError) {
	exists := make(map[string]struct{}, len(tasks))
	for _, t := range tasks {
		exists[t.ID] = struct{}{}
	}

	scopes := fileScopes(tasks)
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
			if !t.Parallel {
				barrierByPhase[t.PhaseSeq] = t.ID
			}
		} else {
			if barrier, ok := barrierByPhase[t.PhaseSeq]; ok && barrier != t.ID {
				edges = append(edges, derivedEdge{from: t.ID, to: barrier, lineNo: t.LineNo})
			}
			if !t.Parallel {
				barrierByPhase[t.PhaseSeq] = t.ID
			}
		}

		edges = append(edges, priorFileOverlapEdgesAt(tasks, i, scopes)...)
	}
	return edges, dangling
}

func priorFileOverlapEdgesAt(tasks []Task, i int, scopes map[string]map[string]struct{}) []derivedEdge {
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
			out = append(out, derivedEdge{
				from: tb.ID, to: ta.ID, lineNo: tb.LineNo,
				Reason: EdgeReasonFileOverlap,
			})
		}
	}
	return out
}

func edgeSet(edges []derivedEdge) map[string]struct{} {
	out := make(map[string]struct{}, len(edges))
	for _, e := range edges {
		out[e.from+"→"+e.to] = struct{}{}
	}
	return out
}
