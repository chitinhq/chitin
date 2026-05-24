package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/adapter/speckit"
)

func TestTasksLintAgreesWithRunScheduleDAGValidation(t *testing.T) {
	tasksMd := strings.Join([]string{
		"- [ ] T001 [US1] Implement the foo handler in `foo.go`",
		"- [ ] T002 [US1] Create a foo helper in `foo.go`",
		"- [ ] T003 [US1] Do the thing",
	}, "\n")
	repo := fixtureRepo(t, "107-lint-agreement", tasksMd)

	var lintOut, lintErr bytes.Buffer
	lintCode := runTasksLint(context.Background(), []string{"--repo-root", repo, "107"}, &lintOut, &lintErr)
	if lintCode != exitUserError {
		t.Fatalf("tasks-lint exit = %d, want %d; stdout=%q stderr=%q", lintCode, exitUserError, lintOut.String(), lintErr.String())
	}

	cs, err := speckit.New().CompileSpec(repo, "107")
	if err != nil {
		t.Fatalf("compile fixture spec: %v", err)
	}
	registry, err := buildRegistry()
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	validationErrs := ValidateForDispatch(context.Background(), cs.DAG, registry)
	var validationUnclassified []string
	for _, verr := range validationErrs {
		if verr.Kind == "needs_clarification" {
			validationUnclassified = append(validationUnclassified, strings.TrimPrefix(verr.NodeID, "107-lint-agreement/"))
		}
	}

	lintUnclassified := unclassifiedTaskIDsFromRows(taskLintRows(cs.DAG))
	if strings.Join(lintUnclassified, ",") != strings.Join(validationUnclassified, ",") {
		t.Fatalf("tasks-lint unclassified = %v, runSchedule DAG validation unclassified = %v", lintUnclassified, validationUnclassified)
	}
	if len(lintUnclassified) != 1 || lintUnclassified[0] != "T003" {
		t.Fatalf("unclassified task ids = %v, want [T003]", lintUnclassified)
	}

	var schedOut, schedErr bytes.Buffer
	scheduleCode := runSchedule(context.Background(), []string{"--repo-root", repo, "107"}, &schedOut, &schedErr)
	if scheduleCode != exitUserError {
		t.Fatalf("runSchedule exit = %d, want %d; stdout=%q stderr=%q", scheduleCode, exitUserError, schedOut.String(), schedErr.String())
	}
	if !strings.Contains(schedErr.String(), "107-lint-agreement/T003") {
		t.Fatalf("runSchedule stderr = %q, want it to name the same unclassified task", schedErr.String())
	}
	if !strings.Contains(lintErr.String(), "T003") {
		t.Fatalf("tasks-lint stderr = %q, want it to name T003", lintErr.String())
	}
}

func unclassifiedTaskIDsFromRows(rows []taskLintRow) []string {
	var ids []string
	for _, row := range rows {
		if !row.Classified {
			ids = append(ids, row.TaskID)
		}
	}
	return ids
}
