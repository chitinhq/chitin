package speclint

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// withFreshRegistry swaps the package-level registry for the duration of
// the test so individual tests don't leak rules into each other or pick
// up rules registered by sibling rule files (T003-T009) once they land.
func withFreshRegistry(t *testing.T) {
	t.Helper()
	saved := registry
	registry = nil
	t.Cleanup(func() { registry = saved })
}

func writeSpec(t *testing.T, dir string) string {
	t.Helper()
	specDir := filepath.Join(dir, ".specify", "specs", "999-fixture-spec")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "spec.md"), []byte("# Spec\n"), 0o644); err != nil {
		t.Fatalf("spec.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "tasks.md"), []byte("- [ ] T001 do it\n"), 0o644); err != nil {
		t.Fatalf("tasks.md: %v", err)
	}
	return specDir
}

func TestResolveSpecPaths_FindsFilesAndInfersRepoRoot(t *testing.T) {
	repo := t.TempDir()
	specDir := writeSpec(t, repo)

	paths, err := ResolveSpecPaths(specDir)
	if err != nil {
		t.Fatalf("ResolveSpecPaths: %v", err)
	}
	if paths.SpecDir != specDir {
		t.Errorf("SpecDir=%q, want %q", paths.SpecDir, specDir)
	}
	if paths.SpecMD != filepath.Join(specDir, "spec.md") {
		t.Errorf("SpecMD=%q", paths.SpecMD)
	}
	if paths.TasksMD != filepath.Join(specDir, "tasks.md") {
		t.Errorf("TasksMD=%q", paths.TasksMD)
	}
	wantRoot, _ := filepath.Abs(repo)
	if paths.RepoRoot != wantRoot {
		t.Errorf("RepoRoot=%q, want %q", paths.RepoRoot, wantRoot)
	}
}

func TestResolveSpecPaths_MissingSpecMD(t *testing.T) {
	dir := t.TempDir()
	// Only tasks.md, no spec.md.
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ResolveSpecPaths(dir); err == nil {
		t.Fatal("err = nil, want missing spec.md")
	}
}

func TestResolveSpecPaths_MissingTasksMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("# x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ResolveSpecPaths(dir); err == nil {
		t.Fatal("err = nil, want missing tasks.md")
	}
}

func TestResolveSpecPaths_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := ResolveSpecPaths(f); err == nil {
		t.Fatal("err = nil, want not-a-directory")
	}
}

func TestRegisterAndRun_PreservesOrderAndMergesViolations(t *testing.T) {
	withFreshRegistry(t)

	Register("L01", func(SpecPaths) ([]Violation, error) {
		return []Violation{{Rule: "L01", File: "spec.md", Line: 3, Severity: SeverityError, Message: "missing frontmatter"}}, nil
	})
	Register("L02", func(SpecPaths) ([]Violation, error) {
		return []Violation{{Rule: "L02", File: "spec.md", Line: 1, Severity: SeverityWarning, Message: "broken ref"}}, nil
	})

	vs, err := Run(SpecPaths{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Sorted by (file, line, rule): L02 (line 1) before L01 (line 3).
	want := []Violation{
		{Rule: "L02", File: "spec.md", Line: 1, Severity: SeverityWarning, Message: "broken ref"},
		{Rule: "L01", File: "spec.md", Line: 3, Severity: SeverityError, Message: "missing frontmatter"},
	}
	if !reflect.DeepEqual(vs, want) {
		t.Errorf("violations=%+v\nwant=%+v", vs, want)
	}
	if got := Rules(); !reflect.DeepEqual(got, []string{"L01", "L02"}) {
		t.Errorf("Rules()=%v, want [L01 L02]", got)
	}
}

func TestRun_PropagatesRuleError(t *testing.T) {
	withFreshRegistry(t)

	sentinel := errors.New("yaml parse failed")
	Register("L01", func(SpecPaths) ([]Violation, error) {
		return nil, sentinel
	})
	Register("L02", func(SpecPaths) ([]Violation, error) {
		t.Error("L02 should not run after L01 errors")
		return nil, nil
	})

	_, err := Run(SpecPaths{})
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("err=%v, want wrapping %v", err, sentinel)
	}
}

func TestRun_NoRulesRegisteredReturnsEmpty(t *testing.T) {
	withFreshRegistry(t)
	vs, err := Run(SpecPaths{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(vs) != 0 {
		t.Errorf("len(violations)=%d, want 0", len(vs))
	}
}

func TestRegister_DuplicateNameOverwritesInPlace(t *testing.T) {
	withFreshRegistry(t)

	Register("L01", func(SpecPaths) ([]Violation, error) {
		return []Violation{{Rule: "L01", Severity: SeverityWarning, Message: "v1"}}, nil
	})
	Register("L02", func(SpecPaths) ([]Violation, error) { return nil, nil })
	Register("L01", func(SpecPaths) ([]Violation, error) {
		return []Violation{{Rule: "L01", Severity: SeverityError, Message: "v2"}}, nil
	})

	if got := Rules(); !reflect.DeepEqual(got, []string{"L01", "L02"}) {
		t.Errorf("Rules()=%v, want [L01 L02] (in-place overwrite preserves order)", got)
	}
	vs, err := Run(SpecPaths{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(vs) != 1 || vs[0].Message != "v2" {
		t.Errorf("violations=%+v, want one with message 'v2'", vs)
	}
}

func TestWorst(t *testing.T) {
	cases := []struct {
		name string
		vs   []Violation
		want Severity
	}{
		{"empty", nil, ""},
		{"info only", []Violation{{Severity: SeverityInfo}}, SeverityInfo},
		{"info+warning", []Violation{{Severity: SeverityInfo}, {Severity: SeverityWarning}}, SeverityWarning},
		{"warning+error", []Violation{{Severity: SeverityWarning}, {Severity: SeverityError}}, SeverityError},
		{"error first", []Violation{{Severity: SeverityError}, {Severity: SeverityInfo}}, SeverityError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Worst(c.vs); got != c.want {
				t.Errorf("Worst=%q, want %q", got, c.want)
			}
		})
	}
}

func TestInferRepoRoot_NoSpecifyAncestor(t *testing.T) {
	dir := t.TempDir()
	if got := inferRepoRoot(dir); got != "" {
		t.Errorf("inferRepoRoot(%q)=%q, want empty", dir, got)
	}
}
