package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter/speckit"
)

// fixtureRepo writes a minimal chitin-shaped repo with one spec under
// .specify/specs/ for use by schedule tests. The spec name is supplied;
// the caller chooses whether to also populate tasks.md.
func fixtureRepo(t *testing.T, specName string, tasksMd string) string {
	t.Helper()
	repo := t.TempDir()
	specDir := filepath.Join(repo, ".specify", "specs", specName)
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("setup mkdir: %v", err)
	}
	// Minimal spec-kit artifacts. The adapter checks for tasks.md presence
	// to decide whether to compile.
	if err := os.WriteFile(filepath.Join(specDir, "spec.md"), []byte("# Spec\n"), 0o644); err != nil {
		t.Fatalf("setup spec.md: %v", err)
	}
	if tasksMd != "" {
		if err := os.WriteFile(filepath.Join(specDir, "tasks.md"), []byte(tasksMd), 0o644); err != nil {
			t.Fatalf("setup tasks.md: %v", err)
		}
	}
	return repo
}

// fakeKernelForSchedule installs a fake chitin-kernel that swallows emit
// successfully (exit 0), so emit doesn't print a warning during schedule
// tests that hit the real flow up to the Temporal dial.
func fakeKernelForSchedule(t *testing.T) {
	t.Helper()
	bin, _ := fakeKernelBin(t, 0)
	t.Setenv("CHITIN_KERNEL_BIN", bin)
}

func TestRunSchedule_NoArgs(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), nil, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "exactly one positional argument") {
		t.Errorf("stderr = %q; want it to mention positional arg requirement", errBuf.String())
	}
}

func TestRunSchedule_TooManyArgs(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{"a", "b"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d", code, exitUserError)
	}
}

func TestRunSchedule_UnknownFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{"--bogus", "x", "096"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d", code, exitUserError)
	}
}

func TestRunSchedule_SpecRefNotFound(t *testing.T) {
	repo := fixtureRepo(t, "091-known", "- [ ] T001 [P] [US1] Implement the thing")
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{"--repo-root", repo, "999"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), `no spec matching ref "999"`) {
		t.Errorf("stderr should report missing ref; got: %q", errBuf.String())
	}
}

func TestRunSchedule_AmbiguousRef(t *testing.T) {
	repo := t.TempDir()
	for _, name := range []string{"091-a", "092-b", "095-c"} {
		dir := filepath.Join(repo, ".specify", "specs", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte("# Spec\n"), 0o644); err != nil {
			t.Fatalf("setup spec.md: %v", err)
		}
	}
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{"--repo-root", repo, "09"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "ambiguous") {
		t.Errorf("stderr should report ambiguity; got: %q", errBuf.String())
	}
	// Expect all three candidates listed.
	for _, name := range []string{"091-a", "092-b", "095-c"} {
		if !strings.Contains(errBuf.String(), name) {
			t.Errorf("candidate %q missing from stderr: %q", name, errBuf.String())
		}
	}
}

func TestRunSchedule_MissingTasksMD(t *testing.T) {
	// Spec dir exists but has no tasks.md → adapter rejects.
	repo := fixtureRepo(t, "091-no-tasks", "")
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{"--repo-root", repo, "091"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "compile failed") {
		t.Errorf("stderr should report compile failure; got: %q", errBuf.String())
	}
}

func TestRunSchedule_DAGValidationFailure_Unroutable(t *testing.T) {
	// Use a task description that doesn't match any capability keyword set —
	// the adapter then marks the node NeedsClarification, which the
	// validator surfaces as a needs_clarification failure.
	tasksMd := "- [ ] T001 [P] [US1] foo bar baz xyzzy"
	repo := fixtureRepo(t, "091-needs-clarif", tasksMd)
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{"--repo-root", repo, "091"}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "DAG validation failed") {
		t.Errorf("stderr should report validation failure; got: %q", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "unclassified capability") {
		t.Errorf("stderr should call out unclassified capability; got: %q", errBuf.String())
	}
}

func TestRunSchedule_TemporalUnreachable(t *testing.T) {
	// Valid spec, valid DAG, but Temporal at an impossible port.
	tasksMd := "- [ ] T001 [P] [US1] Implement the placeholder"
	repo := fixtureRepo(t, "091-good", tasksMd)
	fakeKernelForSchedule(t)
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--temporal-host", "127.0.0.1:1", // refused
		"091",
	}, &out, &errBuf)
	if code != exitRuntimeError {
		t.Errorf("exit code = %d, want %d (runtime); stderr=%q", code, exitRuntimeError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "Temporal unreachable") {
		t.Errorf("stderr should report Temporal unreachable; got: %q", errBuf.String())
	}
}

func TestCollectCapabilities_DedupesAndSorts(t *testing.T) {
	// Just exercise the helper directly — light contract test.
	repo := fixtureRepo(t, "091-cap", "- [ ] T001 [P] [US1] Implement the thing\n- [ ] T002 [P] [US1] Implement the other thing")
	cs, err := speckit.New().CompileSpec(repo, "091")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	caps := collectCapabilities(cs.DAG)
	if len(caps) != 1 {
		t.Fatalf("expected 1 unique capability, got %d: %v", len(caps), caps)
	}
	if caps[0] != "code.implement" {
		t.Errorf("expected code.implement, got %v", caps)
	}
}
