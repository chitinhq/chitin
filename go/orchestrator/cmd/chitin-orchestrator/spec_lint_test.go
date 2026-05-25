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

// specLintFixtureDir writes a minimal spec directory (spec.md + tasks.md)
// under a fresh tmpdir and returns its path. The shape mirrors what the
// orchestrator's spec-lint reads in production.
func specLintFixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Frontmatter satisfies L01 (required keys present). Empty US
	// sections + empty FR set keeps L02-L07 silent — they only flag
	// affirmative drift, not absence. This fixture is the "happy
	// minimal" specdir the spec-lint cmd should report as clean.
	specMD := `---
spec_id: 200
title: "spec-lint smoke fixture"
status: "draft"
owner: "test"
created: "2026-05-25"
depends_on: []
related: []
---

# Spec
`
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(specMD), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.md"), []byte("- [ ] T001\n"), 0o644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}
	return dir
}

func TestRunSpecLint_RequiresPositionalArg(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), nil, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "exactly one positional argument") {
		t.Errorf("stderr should mention positional arg requirement; got %q", errBuf.String())
	}
}

func TestRunSpecLint_TooManyArgs(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{"a", "b"}, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}
}

func TestRunSpecLint_NonExistentDir(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{filepath.Join(t.TempDir(), "no-such-dir")}, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "error:") {
		t.Errorf("stderr should surface load error; got %q", errBuf.String())
	}
}

func TestRunSpecLint_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{file}, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}
}

// TestRunSpecLint_CleanSpecEmitsEmptyArray verifies the no-violations exit
// contract: stdout is "[]" (not "null"), exit code is 0. With no rules
// registered (T003-T009 not yet landed), every spec dir is "clean" by this
// definition — which is exactly what we want T002 to verify in isolation.
func TestRunSpecLint_CleanSpecEmitsEmptyArray(t *testing.T) {
	dir := specLintFixtureDir(t)
	var out, errBuf bytes.Buffer
	code := runSpecLint(context.Background(), []string{dir}, &out, &errBuf)
	if code != specLintExitClean {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, specLintExitClean, errBuf.String())
	}
	stdout := strings.TrimSpace(out.String())
	if stdout != "[]" {
		t.Errorf("stdout=%q, want %q", stdout, "[]")
	}
	// Round-trip through json.Unmarshal as the strongest shape assertion.
	var vs []speclint.Violation
	if err := json.Unmarshal(out.Bytes(), &vs); err != nil {
		t.Fatalf("stdout not valid JSON array: %v\nout=%s", err, out.String())
	}
	if len(vs) != 0 {
		t.Errorf("want empty violations slice, got %+v", vs)
	}
}

func TestSpecLintExitCode(t *testing.T) {
	tests := []struct {
		name string
		in   []speclint.Violation
		want int
	}{
		{"empty", nil, specLintExitClean},
		{"warning only", []speclint.Violation{{Severity: speclint.SeverityWarning}}, specLintExitWarnings},
		{"error only", []speclint.Violation{{Severity: speclint.SeverityError}}, specLintExitErrors},
		{
			"mixed prefers error",
			[]speclint.Violation{
				{Severity: speclint.SeverityWarning},
				{Severity: speclint.SeverityError},
			},
			specLintExitErrors,
		},
		{
			"multiple warnings",
			[]speclint.Violation{
				{Severity: speclint.SeverityWarning},
				{Severity: speclint.SeverityWarning},
			},
			specLintExitWarnings,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := specLintExitCode(tc.in)
			if got != tc.want {
				t.Errorf("specLintExitCode(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestRunMain_DispatchesSpecLint(t *testing.T) {
	dir := specLintFixtureDir(t)
	code := runMain([]string{"chitin-orchestrator", "spec-lint", dir})
	if code != specLintExitClean {
		t.Fatalf("exit=%d, want %d", code, specLintExitClean)
	}
}
