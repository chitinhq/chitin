package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

const tasksLintFixtureTasks = `## Phase 1: Fixture

- [ ] T001 [P] [US1] Implement the thing in ` + "`foo.go`" + `
- [ ] T002 [P] [US1] Update docs for the thing in ` + "`README.md`" + `
- [ ] T003 [US1] Do the thing
`

func TestTasksLint_JSONFlagEmitsTaskArray(t *testing.T) {
	repo := fixtureRepo(t, "107-tasks-lint-json", tasksLintFixtureTasks)
	var stdout, stderr bytes.Buffer

	code := runTasksLint(context.Background(), []string{
		"--repo-root", repo,
		"--json",
		"107",
	}, &stdout, &stderr)
	if code != exitUserError {
		t.Fatalf("exit code = %d, want %d\nstdout=%q\nstderr=%q", code, exitUserError, stdout.String(), stderr.String())
	}

	var rows []struct {
		TaskID     string  `json:"task_id"`
		Capability *string `json:"capability"`
		Classified *bool   `json:"classified"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("stdout is not a parseable JSON array: %v\nstdout=%q", err, stdout.String())
	}
	if len(rows) != 3 {
		t.Fatalf("JSON row count = %d, want 3; rows=%+v", len(rows), rows)
	}

	classifiedByTask := map[string]bool{}
	capabilityByTask := map[string]*string{}
	for _, row := range rows {
		if row.Classified == nil {
			t.Fatalf("task %s missing classified field", row.TaskID)
		}
		classifiedByTask[row.TaskID] = *row.Classified
		capabilityByTask[row.TaskID] = row.Capability
	}

	assertTaskClassified(t, classifiedByTask, capabilityByTask, "T001", true)
	assertTaskClassified(t, classifiedByTask, capabilityByTask, "T002", true)
	assertTaskClassified(t, classifiedByTask, capabilityByTask, "T003", false)
}

func assertTaskClassified(t *testing.T, classifiedByTask map[string]bool, capabilityByTask map[string]*string, taskID string, want bool) {
	t.Helper()
	got, ok := classifiedByTask[taskID]
	if !ok {
		t.Fatalf("JSON output missing task %s; got tasks=%v", taskID, classifiedByTask)
	}
	if got != want {
		t.Fatalf("task %s classified = %v, want %v", taskID, got, want)
	}
	if want && capabilityByTask[taskID] == nil {
		t.Fatalf("task %s classified true but capability is null", taskID)
	}
	if !want && capabilityByTask[taskID] != nil {
		t.Fatalf("task %s classified false but capability = %q, want null", taskID, *capabilityByTask[taskID])
	}
}
