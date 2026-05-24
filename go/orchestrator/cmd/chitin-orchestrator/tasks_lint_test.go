package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunTasksLint_UnclassifiedTaskFailsAndPrintsRows(t *testing.T) {
	tasksMd := strings.Join([]string{
		"- [ ] T001 [P] [US1] Implement the foo handler in `foo.go`",
		"- [ ] T002 [P] [US1] Update docs for the foo handler in `docs/foo.md`",
		"- [ ] T003 [US1] Do the thing",
	}, "\n")
	repo := fixtureRepo(t, "107-tasks-lint", tasksMd)

	var out, errBuf bytes.Buffer
	code := runTasksLint(context.Background(), []string{"--repo-root", repo, "107"}, &out, &errBuf)

	if code != exitUserError {
		t.Fatalf("exit code = %d, want %d; stdout=%q stderr=%q", code, exitUserError, out.String(), errBuf.String())
	}

	stdout := out.String()
	for _, want := range []string{
		"T001",
		"code.implement",
		"Implement the foo handler",
		"T002",
		"docs.write",
		"Update docs for the foo handler",
		"T003",
		"unclassified",
		"Do the thing",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, stdout)
		}
	}

	stderr := errBuf.String()
	if !strings.Contains(stderr, "T003") {
		t.Errorf("stderr should name unclassified task id T003; got: %q", stderr)
	}
}
