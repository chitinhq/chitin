package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildWholeSpecDAG_HappyPath proves the single-node DAG carries
// the expected shape: one node, no edges, ID matches the convention,
// capability is CapSpecImplement, Description contains spec + tasks
// content, and the unchecked-task count is returned for telemetry.
func TestBuildWholeSpecDAG_HappyPath(t *testing.T) {
	specRef := "199-whole-spec-test"
	dir := t.TempDir()
	specDir := filepath.Join(dir, specRef)
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir spec dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "spec.md"),
		[]byte("# Spec\n\nFR-001 do the thing.\n"), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "tasks.md"),
		[]byte("- [ ] T001 do A\n- [ ] T002 do B\n- [x] T003 already done\n"), 0o644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}

	d, count, err := buildWholeSpecDAG(specRef, specDir, "/some/repo", "main")
	if err != nil {
		t.Fatalf("buildWholeSpecDAG: %v", err)
	}
	if count != 2 {
		t.Errorf("unchecked count = %d, want 2 (T001+T002; T003 is checked)", count)
	}
	nodes := d.Nodes()
	if len(nodes) != 1 {
		t.Fatalf("node count = %d, want 1", len(nodes))
	}
	n := nodes[0]
	if n.ID != "wu-199-whole-spec-test-whole" {
		t.Errorf("node ID = %q, want wu-199-whole-spec-test-whole", n.ID)
	}
	if n.Capability != "code.spec-implement" {
		t.Errorf("capability = %q, want code.spec-implement", n.Capability)
	}
	if n.TargetRepo != "/some/repo" || n.BaseRef != "main" {
		t.Errorf("target/base = (%q,%q), want (/some/repo, main)", n.TargetRepo, n.BaseRef)
	}
	if !n.WorktreeRequired {
		t.Error("WorktreeRequired = false; want true")
	}
	if !strings.Contains(n.Description, "FR-001 do the thing") {
		t.Error("Description missing spec.md content")
	}
	if !strings.Contains(n.Description, "- T001") || !strings.Contains(n.Description, "- T002") {
		t.Error("Description missing unchecked task ids in the listed section")
	}
	if !strings.Contains(n.Description, "spec 199-whole-spec-test") {
		t.Error("Description should name the spec being implemented")
	}
	if len(d.Edges()) != 0 {
		t.Errorf("edge count = %d, want 0 (single-node DAG)", len(d.Edges()))
	}
}

// TestBuildWholeSpecDAG_WithPlan proves plan.md is included when present.
func TestBuildWholeSpecDAG_WithPlan(t *testing.T) {
	specRef := "199-plan-test"
	dir := t.TempDir()
	specDir := filepath.Join(dir, specRef)
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWrite := func(name, body string) {
		if err := os.WriteFile(filepath.Join(specDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	mustWrite("spec.md", "# Spec\n")
	mustWrite("tasks.md", "- [ ] T001 a\n")
	mustWrite("plan.md", "# Plan\n\nPHASE_X notes\n")

	d, _, err := buildWholeSpecDAG(specRef, specDir, "/r", "main")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	desc := d.Nodes()[0].Description
	if !strings.Contains(desc, "PHASE_X notes") {
		t.Error("Description should include plan.md content when plan.md exists")
	}
}

// TestBuildWholeSpecDAG_NoPlan proves the absence of plan.md is tolerated.
func TestBuildWholeSpecDAG_NoPlan(t *testing.T) {
	specRef := "199-no-plan-test"
	dir := t.TempDir()
	specDir := filepath.Join(dir, specRef)
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "spec.md"), []byte("# Spec\n"), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(specDir, "tasks.md"), []byte("- [ ] T001 a\n"), 0o644); err != nil {
		t.Fatalf("write tasks.md: %v", err)
	}
	if _, _, err := buildWholeSpecDAG(specRef, specDir, "/r", "main"); err != nil {
		t.Errorf("missing plan.md should be tolerated; got: %v", err)
	}
}

// TestBuildWholeSpecDAG_MissingSpecMD fails fast — the whole-spec
// invocation can't proceed without the spec text.
func TestBuildWholeSpecDAG_MissingSpecMD(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := buildWholeSpecDAG("199-x", dir, "/r", "main"); err == nil {
		t.Error("missing spec.md should produce an error")
	}
}

// TestExtractUncheckedTaskIDs covers the regex's tasks.md parsing.
// Pinned because spec 119 telemetry depends on it (WholeSpecTaskCount
// is the result).
func TestExtractUncheckedTaskIDs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"all unchecked", "- [ ] T001 a\n- [ ] T002 b\n", []string{"T001", "T002"}},
		{"mixed", "- [x] T001 done\n- [ ] T002 open\n- [x] T003 done\n", []string{"T002"}},
		{"non-task lines ignored", "# Notes\n- [ ] T001 a\nprose\n- [ ] T999 z\n", []string{"T001", "T999"}},
		{"non-T id ignored", "- [ ] FOO bar\n- [ ] T001 a\n", []string{"T001"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractUncheckedTaskIDs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRunSchedule_DefaultModeIsWholeSpec proves the spec 119 FR-001
// default: omitting both mode flags selects whole-spec.
func TestRunSchedule_DefaultModeIsWholeSpec(t *testing.T) {
	tasksMd := "- [ ] T001 [P] [US1] Implement the placeholder"
	repo := fixtureRepo(t, "091-default-mode", tasksMd)
	fakeKernelForSchedule(t)
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--temporal-host", "127.0.0.1:1", // we expect Temporal-unreachable; the mode is set BEFORE that
		"091",
	}, &out, &errBuf)
	_ = code // exit code is runtime-error; we only care about the mode resolution succeeding
	// The deprecation note for --per-task MUST NOT appear when no flag is given.
	if strings.Contains(errBuf.String(), "legacy fragmented-dispatch mode") {
		t.Error("default mode should be whole-spec; per-task deprecation note appeared on a no-flag invocation")
	}
}

// TestRunSchedule_PerTaskEmitsDeprecationNote proves FR-009 — the
// --per-task flag emits a one-line nudge to stderr (not an error).
func TestRunSchedule_PerTaskEmitsDeprecationNote(t *testing.T) {
	tasksMd := "- [ ] T001 [P] [US1] Implement the placeholder"
	repo := fixtureRepo(t, "091-per-task-note", tasksMd)
	fakeKernelForSchedule(t)
	var out, errBuf bytes.Buffer
	_ = runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--per-task",
		"--temporal-host", "127.0.0.1:1",
		"091",
	}, &out, &errBuf)
	if !strings.Contains(errBuf.String(), "legacy fragmented-dispatch mode") {
		t.Errorf("--per-task should emit the deprecation note; stderr=%q", errBuf.String())
	}
}

// TestRunSchedule_MutuallyExclusiveModeFlags proves giving both
// --whole-spec and --per-task is a user error.
func TestRunSchedule_MutuallyExclusiveModeFlags(t *testing.T) {
	tasksMd := "- [ ] T001 [P] [US1] Implement the placeholder"
	repo := fixtureRepo(t, "091-mutex", tasksMd)
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo, "--whole-spec", "--per-task", "091",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit = %d, want exitUserError (%d)", code, exitUserError)
	}
	if !strings.Contains(errBuf.String(), "mutually exclusive") {
		t.Errorf("stderr should explain the mutual-exclusion; got: %q", errBuf.String())
	}
}
