package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/internal/speclint"
)

// specLintFixtureDir writes a minimal spec.md + tasks.md pair under
// .specify/specs/115-fixture/ and returns the spec directory path.
func specLintFixtureDir(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	specDir := filepath.Join(repo, ".specify", "specs", "115-fixture")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "spec.md"), []byte("# Spec\n"), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "tasks.md"), []byte("- [ ] T001 do it\n"), 0o644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}
	return specDir
}

// withRules swaps in a controlled set of rules for the duration of one
// test. T002 owns the wiring; T003-T009 register the real L01-L07 rules
// in package-init from their own files. We swap so wiring tests see only
// the rules they declare.
func withRules(t *testing.T, rules map[string]func(speclint.SpecPaths) ([]speclint.Violation, error)) {
	t.Helper()
	t.Cleanup(speclint.ResetRulesForTest())
	for name, fn := range rules {
		speclint.Register(name, fn)
	}
}

func TestRunSpecLint_NoArgs(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), nil, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("code=%d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "exactly one positional argument") {
		t.Errorf("stderr=%q", errBuf.String())
	}
}

func TestRunSpecLint_TooManyArgs(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{"a", "b"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("code=%d, want %d", code, exitUserError)
	}
}

func TestRunSpecLint_MissingSpecDir(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{"/no/such/path"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("code=%d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "spec dir") {
		t.Errorf("stderr should mention spec dir; got %q", errBuf.String())
	}
}

func TestRunSpecLint_CleanExitsZeroAndEmitsEmptyJSON(t *testing.T) {
	specDir := specLintFixtureDir(t)
	withRules(t, nil) // no rules registered → no violations

	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{specDir}, &out, &errBuf)
	if code != specLintExitClean {
		t.Fatalf("code=%d, want %d; stderr=%q", code, specLintExitClean, errBuf.String())
	}

	stdout := strings.TrimSpace(out.String())
	if stdout != "[]" {
		t.Errorf("stdout=%q, want %q", stdout, "[]")
	}
}

func TestRunSpecLint_WarningsExitsTwo(t *testing.T) {
	specDir := specLintFixtureDir(t)
	withRules(t, map[string]func(speclint.SpecPaths) ([]speclint.Violation, error){
		"L01": func(speclint.SpecPaths) ([]speclint.Violation, error) {
			return []speclint.Violation{{Rule: "L01", File: "spec.md", Line: 2, Severity: speclint.SeverityWarning, Message: "soft"}}, nil
		},
	})

	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{specDir}, &out, &errBuf)
	if code != specLintExitWarnings {
		t.Fatalf("code=%d, want %d; stderr=%q", code, specLintExitWarnings, errBuf.String())
	}

	var vs []speclint.Violation
	if err := json.Unmarshal(out.Bytes(), &vs); err != nil {
		t.Fatalf("unmarshal: %v\nstdout=%s", err, out.String())
	}
	if len(vs) != 1 || vs[0].Severity != speclint.SeverityWarning {
		t.Errorf("violations=%+v", vs)
	}
}

func TestRunSpecLint_ErrorsExitsThree(t *testing.T) {
	specDir := specLintFixtureDir(t)
	withRules(t, map[string]func(speclint.SpecPaths) ([]speclint.Violation, error){
		"L01": func(speclint.SpecPaths) ([]speclint.Violation, error) {
			return []speclint.Violation{{Rule: "L01", File: "spec.md", Line: 2, Severity: speclint.SeverityWarning, Message: "soft"}}, nil
		},
		"L05": func(speclint.SpecPaths) ([]speclint.Violation, error) {
			return []speclint.Violation{{Rule: "L05", File: "spec.md", Line: 5, Severity: speclint.SeverityError, Message: "hard"}}, nil
		},
	})

	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{specDir}, &out, &errBuf)
	if code != specLintExitErrors {
		t.Fatalf("code=%d, want %d; stderr=%q", code, specLintExitErrors, errBuf.String())
	}

	var vs []speclint.Violation
	if err := json.Unmarshal(out.Bytes(), &vs); err != nil {
		t.Fatalf("unmarshal: %v\nstdout=%s", err, out.String())
	}
	if len(vs) != 2 {
		t.Fatalf("len=%d, want 2: %+v", len(vs), vs)
	}
}

func TestRunSpecLint_OutputShapeIsArrayOfNamedFields(t *testing.T) {
	specDir := specLintFixtureDir(t)
	withRules(t, map[string]func(speclint.SpecPaths) ([]speclint.Violation, error){
		"L01": func(speclint.SpecPaths) ([]speclint.Violation, error) {
			return []speclint.Violation{{
				Rule:     "L01",
				File:     "spec.md",
				Line:     17,
				Severity: speclint.SeverityError,
				Message:  "frontmatter missing 'owner'",
			}}, nil
		},
	})

	var out, errBuf bytes.Buffer
	if code := runSpecLint(context.Background(), []string{specDir}, &out, &errBuf); code != specLintExitErrors {
		t.Fatalf("code=%d, want %d", code, specLintExitErrors)
	}

	// Decode into a raw map to assert the literal JSON keys per FR-003.
	var raw []map[string]any
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("len=%d, want 1: %v", len(raw), raw)
	}
	for _, want := range []string{"rule", "file", "line", "severity", "message"} {
		if _, ok := raw[0][want]; !ok {
			t.Errorf("missing field %q in %v", want, raw[0])
		}
	}
	if raw[0]["rule"] != "L01" || raw[0]["severity"] != "error" {
		t.Errorf("unexpected payload: %v", raw[0])
	}
}

func TestRunSpecLint_RuleErrorReturnsRuntimeError(t *testing.T) {
	specDir := specLintFixtureDir(t)
	withRules(t, map[string]func(speclint.SpecPaths) ([]speclint.Violation, error){
		"L01": func(speclint.SpecPaths) ([]speclint.Violation, error) {
			return nil, errFromTest()
		},
	})

	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{specDir}, &out, &errBuf)
	if code != exitRuntimeError {
		t.Errorf("code=%d, want %d; stderr=%q", code, exitRuntimeError, errBuf.String())
	}
}

func TestRunMain_DispatchesSpecLint(t *testing.T) {
	specDir := specLintFixtureDir(t)
	t.Cleanup(speclint.ResetRulesForTest())
	code := runMain([]string{"chitin-orchestrator", "spec-lint", specDir})
	if code != specLintExitClean {
		t.Fatalf("code=%d, want %d", code, specLintExitClean)
	}
}

// errFromTest returns a sentinel rule error. Defined as a func to keep
// the test file lint-clean (var would unused-warn before reaching the
// test that consumes it).
func errFromTest() error { return &testErr{"rule boom"} }

type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
