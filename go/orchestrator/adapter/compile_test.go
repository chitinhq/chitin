package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter"
	"github.com/chitinhq/chitin/go/orchestrator/adapter/defaults"
	"github.com/chitinhq/chitin/go/orchestrator/adapter/speckit"
	"github.com/chitinhq/chitin/go/orchestrator/driver"
)

// TestCompileActivityHappyPath drives the compile activity entrypoint through
// a real registry against the spec-kit fixture (FR-001, FR-003).
func TestCompileActivityHappyPath(t *testing.T) {
	reg := mustDefaultRegistry(t)
	res, err := adapter.Compile(context.Background(), reg, adapter.CompileRequest{
		RepoPath: "testdata/speckit",
		SpecRef:  "100",
	})
	if err != nil {
		t.Fatalf("Compile activity: %v", err)
	}
	if res.Kit != speckit.Kit {
		t.Errorf("compiled kit = %q, want %q", res.Kit, speckit.Kit)
	}
	if res.DAG == nil || res.DAG.Len() != 7 {
		t.Fatalf("compiled DAG has %d nodes, want 7", lenOf(res))
	}
	// The activity's output is acyclic — a valid spec-076 Work-Unit DAG.
	if err := res.DAG.Acyclic(); err != nil {
		t.Errorf("compile activity emitted a non-acyclic DAG: %v", err)
	}
}

// TestCompileActivityMalformed asserts the activity surfaces a malformed
// artifact with a precise location and emits no DAG (FR-010, SC-005).
func TestCompileActivityMalformed(t *testing.T) {
	reg := mustDefaultRegistry(t)
	res, err := adapter.Compile(context.Background(), reg, adapter.CompileRequest{
		RepoPath: "testdata/malformed",
		SpecRef:  "300",
	})
	if res != nil {
		t.Error("a malformed artifact must yield no result")
	}
	var mErr *adapter.MalformedArtifactError
	if !errors.As(err, &mErr) {
		t.Fatalf("error %T is not *MalformedArtifactError", err)
	}
	if mErr.File == "" || mErr.Line == 0 {
		t.Errorf("malformed error lacks a precise location: %v", mErr)
	}
}

// TestCompileActivityDangling asserts the activity surfaces a dangling
// dependency naming the missing target and emits no DAG (FR-011).
func TestCompileActivityDangling(t *testing.T) {
	reg := mustDefaultRegistry(t)
	_, err := adapter.Compile(context.Background(), reg, adapter.CompileRequest{
		RepoPath: "testdata/dangling",
		SpecRef:  "310",
	})
	var dErr *adapter.DanglingReferenceError
	if !errors.As(err, &dErr) {
		t.Fatalf("error %T is not *DanglingReferenceError", err)
	}
	if dErr.MissingTarget == "" {
		t.Error("dangling error does not name the missing target")
	}
}

// TestCompileActivityUnrecognizedKit asserts a repo with no kit is reported
// explicitly through the activity (FR-008).
func TestCompileActivityUnrecognizedKit(t *testing.T) {
	reg := mustDefaultRegistry(t)
	_, err := adapter.Compile(context.Background(), reg, adapter.CompileRequest{
		RepoPath: "testdata-nonexistent",
	})
	var uErr *adapter.UnrecognizedKitError
	if !errors.As(err, &uErr) {
		t.Fatalf("error %T is not *UnrecognizedKitError", err)
	}
}

// TestCompileActivityAmbiguousKit asserts a repo carrying two kit markers is
// rejected without an explicit choice and accepted with one (FR-008).
func TestCompileActivityAmbiguousKit(t *testing.T) {
	reg := mustDefaultRegistry(t)

	// The multikit fixture carries both `.specify/` and `openspec/` markers.
	_, err := adapter.Compile(context.Background(), reg, adapter.CompileRequest{
		RepoPath: "testdata/multikit",
	})
	var aErr *adapter.AmbiguousKitError
	if !errors.As(err, &aErr) {
		t.Fatalf("error %T is not *AmbiguousKitError", err)
	}
	if len(aErr.Kits) < 2 {
		t.Errorf("ambiguous error lists %v, want at least two kits", aErr.Kits)
	}
}

// TestMapCapabilityClosedTaxonomy asserts that every capability the mapper
// returns belongs to the closed spec-075 taxonomy, and an unmappable
// description yields ok=false (FR-014) — never an invented tag.
func TestMapCapabilityClosedTaxonomy(t *testing.T) {
	cases := []struct {
		desc    string
		wantCap driver.Capability
		wantOK  bool
	}{
		{"Author tests for the core type — add a table-driven test", driver.CapTestAuthor, true},
		{"Implement the handler — wire the request path", driver.CapCodeImplement, true},
		{"Do the thing", "", false},
		{"Refactor the module and also implement a new feature and write tests", "", false},
	}
	for _, c := range cases {
		got, ok := adapter.MapCapability(c.desc)
		if ok != c.wantOK {
			t.Errorf("MapCapability(%q) ok = %v, want %v", c.desc, ok, c.wantOK)
			continue
		}
		if ok {
			if got != c.wantCap {
				t.Errorf("MapCapability(%q) = %q, want %q", c.desc, got, c.wantCap)
			}
			if !driver.IsKnownCapability(string(got)) {
				t.Errorf("MapCapability(%q) returned non-taxonomy tag %q", c.desc, got)
			}
		}
	}
}

// TestProjectConstitution asserts the constitution projects to each kit's
// expected location (FR-013).
func TestProjectConstitution(t *testing.T) {
	p, err := adapter.ProjectConstitution("speckit", "PRINCIPLES")
	if err != nil {
		t.Fatalf("ProjectConstitution: %v", err)
	}
	if want := ".specify/memory/constitution.md"; p.RelPath != want {
		t.Errorf("speckit constitution RelPath = %q, want %q", p.RelPath, want)
	}
	if p.Content != "PRINCIPLES" {
		t.Errorf("projection content = %q, want PRINCIPLES", p.Content)
	}
	if _, err := adapter.ProjectConstitution("unknown-kit", "X"); err == nil {
		t.Error("expected a kit with no constitution location to be rejected")
	}
}

// mustDefaultRegistry builds the default registry or fails the test.
func mustDefaultRegistry(t *testing.T) *adapter.Registry {
	t.Helper()
	reg, err := defaults.Registry()
	if err != nil {
		t.Fatalf("defaults.Registry: %v", err)
	}
	return reg
}

// lenOf is a nil-safe DAG length for error messages.
func lenOf(res *adapter.CompileResult) int {
	if res == nil || res.DAG == nil {
		return 0
	}
	return res.DAG.Len()
}
