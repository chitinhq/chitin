package copilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflight_AllGreen(t *testing.T) {
	dir := t.TempDir()

	// Fake copilot binary
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeCopilot := filepath.Join(binDir, "copilot")
	if err := os.WriteFile(fakeCopilot, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Fake gh (auth status must succeed)
	fakeGh := filepath.Join(binDir, "gh")
	if err := os.WriteFile(fakeGh, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	// Fake HOME with .chitin/ writable
	chitinDir := filepath.Join(dir, ".chitin")
	if err := os.MkdirAll(chitinDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)

	// Minimal chitin.yaml so policy load succeeds
	policyPath := filepath.Join(dir, "chitin.yaml")
	policyContent := `id: test
mode: guide
bounds:
  max_files_changed: 10
  max_lines_changed: 100
  max_runtime_seconds: 60
escalation:
  elevated_threshold: 3
  high_threshold: 7
  lockdown_threshold: 10
rules:
  - id: allow-all-test
    action: shell.exec
    effect: allow
`
	if err := os.WriteFile(policyPath, []byte(policyContent), 0644); err != nil {
		t.Fatal(err)
	}

	report, err := Preflight(PreflightOpts{Cwd: dir})
	if err != nil {
		t.Fatalf("Preflight failed unexpectedly: %v\nreport: %s", err, report)
	}
	if !strings.Contains(report, "preflight OK") {
		t.Errorf("report should contain 'preflight OK', got: %s", report)
	}
}

func TestPreflight_MissingCopilotBinary(t *testing.T) {
	dir := t.TempDir()
	// Empty PATH — no copilot binary resolvable
	t.Setenv("PATH", filepath.Join(dir, "empty"))
	t.Setenv("HOME", dir)

	_, err := Preflight(PreflightOpts{Cwd: dir})
	if err == nil {
		t.Fatal("expected preflight failure on missing binary")
	}
	if !strings.Contains(err.Error(), "copilot") {
		t.Errorf("error should mention copilot: %v", err)
	}
}
