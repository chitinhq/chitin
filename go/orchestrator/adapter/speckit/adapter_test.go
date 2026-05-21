package speckit

import (
	"errors"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/dag"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// fixtureRepo is the well-formed spec-kit fixture tree root.
const fixtureRepo = "../testdata/speckit"

// TestDetect checks the spec-kit detector recognizes a repo with a real
// spec-kit spec and rejects one without.
func TestDetect(t *testing.T) {
	a := New()
	cases := []struct {
		name string
		repo string
		want bool
	}{
		{"speckit repo", fixtureRepo, true},
		{"openspec repo is not speckit", "../testdata/openspec", false},
		{"missing repo", "../testdata/does-not-exist", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := a.Detect(c.repo)
			if err != nil {
				t.Fatalf("Detect(%q) error: %v", c.repo, err)
			}
			if got != c.want {
				t.Errorf("Detect(%q) = %v, want %v", c.repo, got, c.want)
			}
		})
	}
}

// TestCompileNodeCount asserts one DAG node per tasks.md task (FR-004,
// Acceptance Scenario 1) — the fixture has seven tasks.
func TestCompileNodeCount(t *testing.T) {
	d := mustCompile(t, "100")
	if got, want := d.Len(), 7; got != want {
		t.Fatalf("node count = %d, want %d (one node per task)", got, want)
	}
	for _, id := range []string{
		"100-sample-feature/T001", "100-sample-feature/T007",
	} {
		if _, ok := d.Node(id); !ok {
			t.Errorf("expected node %q in the compiled DAG", id)
		}
	}
}

// TestCompileEdges asserts edges follow tasks.md ordering and `[P]` markers
// (FR-004, Acceptance Scenario 2): a sequential task depends on the running
// phase barrier; `[P]` tasks within a phase are parallel siblings.
func TestCompileEdges(t *testing.T) {
	d := mustCompile(t, "100")
	deps := func(id string) []string { return d.Dependencies(id) }

	// Phase 1: T001 sequential (no deps), T002 [P] depends on barrier T001.
	assertDeps(t, "T001", deps("100-sample-feature/T001"), nil)
	assertDeps(t, "T002", deps("100-sample-feature/T002"), []string{"100-sample-feature/T001"})

	// Phase 2: T003 sequential (no deps — new phase, no barrier yet);
	// T004 and T005 are [P] siblings, both depend on barrier T003, not each
	// other.
	assertDeps(t, "T003", deps("100-sample-feature/T003"), nil)
	assertDeps(t, "T004", deps("100-sample-feature/T004"), []string{"100-sample-feature/T003"})
	assertDeps(t, "T005", deps("100-sample-feature/T005"), []string{"100-sample-feature/T003"})

	// Phase 3: T006 sequential (new phase); T007 sequential depends on T006.
	assertDeps(t, "T006", deps("100-sample-feature/T006"), nil)
	assertDeps(t, "T007", deps("100-sample-feature/T007"), []string{"100-sample-feature/T006"})
}

// TestCompileAcyclic asserts the compiled DAG is accepted by the spec-076
// scheduler's structural check — acyclic with no dangling edges (FR-003,
// Acceptance Scenario 3, SC-002).
func TestCompileAcyclic(t *testing.T) {
	if err := mustCompile(t, "100").Acyclic(); err != nil {
		t.Fatalf("compiled DAG is not a valid Work-Unit DAG: %v", err)
	}
}

// TestCompileMetadata asserts capability and priority are carried from task
// metadata (FR-004). Priority steps down per phase; capabilities come from
// the closed spec-075 taxonomy or are NEEDS CLARIFICATION.
func TestCompileMetadata(t *testing.T) {
	d := mustCompile(t, "100")

	// Priority: phase 1 tasks > phase 2 > phase 3.
	p1, _ := d.Node("100-sample-feature/T001")
	p2, _ := d.Node("100-sample-feature/T003")
	p3, _ := d.Node("100-sample-feature/T006")
	if !(p1.Priority > p2.Priority && p2.Priority > p3.Priority) {
		t.Errorf("priority not phase-ordered: T001=%d T003=%d T006=%d",
			p1.Priority, p2.Priority, p3.Priority)
	}

	// Capability: every non-clarification tag is in the closed taxonomy.
	for _, n := range d.Nodes() {
		if n.Capability == adapter.NeedsClarification {
			continue
		}
		if !driver.IsKnownCapability(n.Capability) {
			t.Errorf("node %s has capability %q outside the closed taxonomy",
				n.ID, n.Capability)
		}
	}

	// T004 ("Author tests …") maps to test.author.
	if t4, _ := d.Node("100-sample-feature/T004"); t4.Capability != string(driver.CapTestAuthor) {
		t.Errorf("T004 capability = %q, want %q", t4.Capability, driver.CapTestAuthor)
	}
}

// TestCompileContext asserts the per-node Task Context carries FR references
// and file paths so a driver can act without re-reading the kit (FR-005,
// Acceptance Scenario 4).
func TestCompileContext(t *testing.T) {
	cs, err := New().CompileSpec(fixtureRepo, "100")
	if err != nil {
		t.Fatalf("CompileSpec: %v", err)
	}
	ctx := cs.Contexts["100-sample-feature/T003"]
	if ctx == nil {
		t.Fatal("no context for T003")
	}
	if len(ctx.FRRefs) != 1 || ctx.FRRefs[0] != "FR-001" {
		t.Errorf("T003 FRRefs = %v, want [FR-001]", ctx.FRRefs)
	}
	if !contains(ctx.FilePaths, "core.go") {
		t.Errorf("T003 FilePaths = %v, want to contain core.go", ctx.FilePaths)
	}
	// The user-story task carries the spec.md user-story excerpt.
	usCtx := cs.Contexts["100-sample-feature/T006"]
	if usCtx == nil || usCtx.SpecExcerpt == "" {
		t.Error("T006 should carry a spec.md user-story excerpt")
	}
}

// TestCompileSpecRefByPrefix asserts a numeric prefix resolves to the unique
// matching spec directory.
func TestCompileSpecRefByPrefix(t *testing.T) {
	byPrefix := mustCompile(t, "100")
	byName := mustCompile(t, "100-sample-feature")
	if byPrefix.Len() != byName.Len() {
		t.Errorf("prefix and full-name compile disagree: %d vs %d",
			byPrefix.Len(), byName.Len())
	}
}

// TestCompileDeterministic asserts the transform is deterministic — the same
// spec compiles to an identical DAG every time (plan.md Constraints).
func TestCompileDeterministic(t *testing.T) {
	first := mustCompile(t, "100")
	for i := 0; i < 20; i++ {
		again := mustCompile(t, "100")
		if !dagEqual(first, again) {
			t.Fatalf("compile %d produced a different DAG — not deterministic", i)
		}
	}
}

// mustCompile compiles a fixture spec or fails the test.
func mustCompile(t *testing.T, specRef string) *dag.DAG {
	t.Helper()
	d, err := New().Compile(fixtureRepo, specRef)
	if err != nil {
		t.Fatalf("Compile(%q): %v", specRef, err)
	}
	return d
}

// assertDeps fails the test unless got and want hold the same ids.
func assertDeps(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s deps = %v, want %v", label, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s deps = %v, want %v", label, got, want)
			return
		}
	}
}

// contains reports whether s is in xs.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// dagEqual reports whether two DAGs hold the same nodes and edges.
func dagEqual(a, b *dag.DAG) bool {
	an, bn := a.Nodes(), b.Nodes()
	if len(an) != len(bn) {
		return false
	}
	for i := range an {
		if !an[i].Equal(bn[i]) {
			return false
		}
	}
	ae, be := a.Edges(), b.Edges()
	if len(ae) != len(be) {
		return false
	}
	for i := range ae {
		if ae[i] != be[i] {
			return false
		}
	}
	return true
}

// TestParseMalformedDuplicate asserts a duplicate task id fails with a
// precise file:line location (FR-010, SC-005).
func TestParseMalformedDuplicate(t *testing.T) {
	_, err := New().Compile("../testdata/malformed", "300")
	if err == nil {
		t.Fatal("expected a malformed-artifact error, got nil")
	}
	var mErr *adapter.MalformedArtifactError
	if !errors.As(err, &mErr) {
		t.Fatalf("error %T is not *MalformedArtifactError", err)
	}
	if mErr.Line == 0 {
		t.Errorf("malformed error has no line location: %v", mErr)
	}
	if mErr.File == "" {
		t.Errorf("malformed error names no file: %v", mErr)
	}
}

// TestCompileDanglingDependency asserts a dependency on a non-existent task
// id fails, naming the missing target (FR-011).
func TestCompileDanglingDependency(t *testing.T) {
	_, err := New().Compile("../testdata/dangling", "310")
	if err == nil {
		t.Fatal("expected a dangling-reference error, got nil")
	}
	var dErr *adapter.DanglingReferenceError
	if !errors.As(err, &dErr) {
		t.Fatalf("error %T is not *DanglingReferenceError", err)
	}
	if dErr.MissingTarget != "T099" {
		t.Errorf("dangling error MissingTarget = %q, want T099", dErr.MissingTarget)
	}
	if dErr.From != "T002" {
		t.Errorf("dangling error From = %q, want T002", dErr.From)
	}
}

// TestCompileAmbiguousDependency asserts a task whose dependency is left
// ambiguous by the artifacts is marked NEEDS CLARIFICATION and is not
// auto-edged (FR-009, SC-004).
func TestCompileAmbiguousDependency(t *testing.T) {
	cs, err := New().CompileSpec("../testdata/ambiguous", "200")
	if err != nil {
		t.Fatalf("CompileSpec: %v", err)
	}
	// T002 signals a dependency in prose with no task id.
	t2 := cs.Contexts["200-ambiguous-deps/T002"]
	if t2 == nil || !t2.NeedsClarification() {
		t.Fatal("T002 with a prose-only dependency should be NEEDS CLARIFICATION")
	}
	if !contains(t2.Clarifications, dependencyClarification) {
		t.Errorf("T002 clarifications = %v, want to contain the dependency reason",
			t2.Clarifications)
	}
	// The ambiguous dependency must NOT have produced an invented edge: T002
	// depends only on its phase barrier T001, nothing invented.
	deps := cs.DAG.Dependencies("200-ambiguous-deps/T002")
	if len(deps) != 1 || deps[0] != "200-ambiguous-deps/T001" {
		t.Errorf("T002 deps = %v — an ambiguous dependency must not invent an edge", deps)
	}
}

// TestCompileWholeRepo asserts the spec-kit adapter compiles chitin's own
// specs/ tree shape — many specs into one acyclic DAG (SC-002). The fixture
// tree stands in for the real repo; the real-repo run is exercised by the
// foundation smoke path.
func TestCompileWholeRepo(t *testing.T) {
	d, err := New().Compile(fixtureRepo, "")
	if err != nil {
		t.Fatalf("whole-repo Compile: %v", err)
	}
	if d.Len() == 0 {
		t.Fatal("whole-repo compile produced an empty DAG")
	}
	if err := d.Acyclic(); err != nil {
		t.Fatalf("whole-repo DAG is not acyclic: %v", err)
	}
}
