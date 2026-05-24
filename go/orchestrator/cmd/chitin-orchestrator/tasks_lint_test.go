package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func tasksLintFixtureRepo(t *testing.T) string {
	t.Helper()
	tasksMd := strings.Join([]string{
		"- [ ] T001 [P] [US1] Implement the foo handler in foo.go",
		"- [ ] T002 [P] [US1] Update docs to mention the foo handler",
		"- [ ] T003 [US1] Do the thing",
	}, "\n")
	return fixtureRepo(t, "107-tasks-lint-fixture", tasksMd)
}

func TestRunTasksLint_TableOutputAndExitCode(t *testing.T) {
	repo := tasksLintFixtureRepo(t)
	var out, errBuf bytes.Buffer

	code := runTasksLint(context.Background(), []string{"--repo-root", repo, "107"}, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}

	stdout := out.String()
	for _, want := range []string{
		"task_id",
		"T001",
		"code.implement",
		"T002",
		"docs.write",
		"T003",
		"unclassified",
		"Do the thing",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(errBuf.String(), "unclassified task(s): T003") {
		t.Errorf("stderr should name unclassified task id; got %q", errBuf.String())
	}
}

func TestRunTasksLint_JSONOutput(t *testing.T) {
	repo := tasksLintFixtureRepo(t)
	var out, errBuf bytes.Buffer

	code := runTasksLint(context.Background(), []string{"107", "--json", "--repo-root", repo}, &out, &errBuf)
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitUserError, errBuf.String())
	}

	var rows []TasksLintRow
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("json unmarshal: %v\nout=%s", err, out.String())
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows)=%d, want 3: %+v", len(rows), rows)
	}
	wantClassified := map[string]bool{"T001": true, "T002": true, "T003": false}
	for _, row := range rows {
		if row.Classified != wantClassified[row.TaskID] {
			t.Errorf("%s classified=%v, want %v", row.TaskID, row.Classified, wantClassified[row.TaskID])
		}
		if row.TaskID == "T003" && row.Capability != nil {
			t.Errorf("T003 capability=%v, want nil", *row.Capability)
		}
		if row.TaskID == "T001" && (row.Capability == nil || *row.Capability != "code.implement") {
			t.Errorf("T001 capability=%v, want code.implement", row.Capability)
		}
	}
}

func TestRunTasksLint_ExitsZeroWhenAllTasksClassify(t *testing.T) {
	tasksMd := strings.Join([]string{
		"- [ ] T001 [P] [US1] Implement the foo handler in foo.go",
		"- [ ] T002 [P] [US1] Update docs to mention the foo handler",
	}, "\n")
	repo := fixtureRepo(t, "107-all-good", tasksMd)
	var out, errBuf bytes.Buffer

	code := runTasksLint(context.Background(), []string{"--repo-root", repo, "107"}, &out, &errBuf)
	if code != exitSuccess {
		t.Fatalf("exit=%d, want %d; stderr=%q", code, exitSuccess, errBuf.String())
	}
	if errBuf.Len() != 0 {
		t.Errorf("stderr=%q, want empty", errBuf.String())
	}
}

func TestRunMain_DispatchesTasksLint(t *testing.T) {
	repo := tasksLintFixtureRepo(t)
	code := runMain([]string{"chitin-orchestrator", "tasks-lint", "--repo-root", repo, "107"})
	if code != exitUserError {
		t.Fatalf("exit=%d, want %d", code, exitUserError)
	}
}
