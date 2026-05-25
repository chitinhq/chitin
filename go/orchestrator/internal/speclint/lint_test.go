package speclint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ReadsSpecAndTasks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("# Spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [ ] T001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(s.SpecMD) != "# Spec\n" {
		t.Errorf("SpecMD=%q", s.SpecMD)
	}
	if string(s.TasksMD) != "- [ ] T001\n" {
		t.Errorf("TasksMD=%q", s.TasksMD)
	}
	abs, _ := filepath.Abs(dir)
	if s.Path != abs {
		t.Errorf("Path=%q, want %q", s.Path, abs)
	}
}

func TestLoad_MissingFilesAreEmptyNotErrors(t *testing.T) {
	dir := t.TempDir() // intentionally empty
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.SpecMD) != 0 || len(s.TasksMD) != 0 {
		t.Errorf("expected empty SpecMD+TasksMD, got %d / %d bytes", len(s.SpecMD), len(s.TasksMD))
	}
}

func TestLoad_NonExistentDirReturnsError(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "no-such"))
	if err == nil {
		t.Fatal("expected error for non-existent dir")
	}
}

func TestLoad_FileNotDirReturnsError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(file)
	if err == nil {
		t.Fatal("expected error when path is a file, not a dir")
	}
}

// withRules swaps the package-level rule registry for the duration of a test,
// so tests can register fake rules without leaking state into one another.
// Run *cannot* be parallelised against this helper.
func withRules(t *testing.T, rules ...RuleFunc) {
	t.Helper()
	saved := registered
	registered = nil
	for _, r := range rules {
		Register(r)
	}
	t.Cleanup(func() { registered = saved })
}

func TestRun_NoRulesYieldsNil(t *testing.T) {
	withRules(t)
	out := Run(&SpecDir{})
	if out != nil {
		t.Errorf("expected nil, got %+v", out)
	}
}

func TestRun_DeterministicOrdering(t *testing.T) {
	// Two rules emit out-of-order findings; Run must sort by
	// (rule, file, line, message).
	withRules(t,
		func(*SpecDir) []Violation {
			return []Violation{
				{Rule: "L02", File: "spec.md", Line: 30, Severity: SeverityError, Message: "z"},
				{Rule: "L01", File: "spec.md", Line: 10, Severity: SeverityError, Message: "b"},
			}
		},
		func(*SpecDir) []Violation {
			return []Violation{
				{Rule: "L01", File: "tasks.md", Line: 5, Severity: SeverityWarning, Message: "a"},
				{Rule: "L01", File: "spec.md", Line: 10, Severity: SeverityError, Message: "a"},
			}
		},
	)
	out := Run(&SpecDir{})
	if len(out) != 4 {
		t.Fatalf("len=%d, want 4: %+v", len(out), out)
	}
	want := []Violation{
		{Rule: "L01", File: "spec.md", Line: 10, Severity: SeverityError, Message: "a"},
		{Rule: "L01", File: "spec.md", Line: 10, Severity: SeverityError, Message: "b"},
		{Rule: "L01", File: "tasks.md", Line: 5, Severity: SeverityWarning, Message: "a"},
		{Rule: "L02", File: "spec.md", Line: 30, Severity: SeverityError, Message: "z"},
	}
	for i, w := range want {
		if out[i] != w {
			t.Errorf("[%d] got %+v, want %+v", i, out[i], w)
		}
	}
}
