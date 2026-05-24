// copilot_dispatch_test.go — spec 099 slice 1 tests for the --driver
// flag routing in runSchedule. Slice 1 verifies the routing skeleton:
// the Copilot branch is reached on --driver copilot, validates required
// inputs, and does NOT dial Temporal or start a SchedulerWorkflow.
//
// Actual gh issue create + chain emit lands in slice 2.

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunSchedule_UnknownDriverValue(t *testing.T) {
	// --driver value that's neither "" (default) nor "copilot" is
	// rejected as user error per contracts/cli-driver-flag.md.
	repo := fixtureRepo(t, "091-good", "- [ ] T001 [P] [US1] Implement the placeholder")
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--driver", "foobar",
		"091",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d (user); stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "unknown driver") {
		t.Errorf("stderr should report unknown driver; got: %q", errBuf.String())
	}
}

func TestRunSchedule_DriverCopilot_MissingRepo(t *testing.T) {
	// --driver copilot requires --repo <owner/name>. Missing → user error.
	repo := fixtureRepo(t, "091-good", "- [ ] T001 [P] [US1] Implement the placeholder")
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--driver", "copilot",
		"091",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d (user); stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "--repo") {
		t.Errorf("stderr should name the missing --repo flag; got: %q", errBuf.String())
	}
}

func TestRunSchedule_DriverCopilot_ValidatesSpecBeforeCopilotBranch(t *testing.T) {
	// Spec resolution + DAG validation must still run on the Copilot path —
	// catch obvious typos before consuming Copilot's slot (R7 in research.md).
	repo := fixtureRepo(t, "091-no-tasks", "")
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--driver", "copilot",
		"--repo", "owner/name",
		"091",
	}, &out, &errBuf)
	if code != exitUserError {
		t.Errorf("exit code = %d, want %d (user); stderr=%q", code, exitUserError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "compile failed") {
		t.Errorf("Copilot path must still run spec compile; got stderr=%q", errBuf.String())
	}
}

func TestRunSchedule_DriverCopilot_ReachesCopilotBranchAfterValidation(t *testing.T) {
	// Happy path: valid spec, --driver copilot, --repo present. The Copilot
	// branch is reached. Slice 1 returns exit 0 with a placeholder message
	// indicating the branch was taken; Temporal MUST NOT be dialed.
	tasksMd := "- [ ] T001 [P] [US1] Implement the placeholder"
	repo := fixtureRepo(t, "091-good", tasksMd)
	fakeKernelForSchedule(t)
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		// Pointing Temporal at an impossible port would FAIL the local path
		// with exitRuntimeError. If we get exitSuccess here, the Copilot
		// branch did NOT dial Temporal — the routing skeleton works.
		"--temporal-host", "127.0.0.1:1",
		"--driver", "copilot",
		"--repo", "owner/name",
		"091",
	}, &out, &errBuf)
	if code != exitSuccess {
		t.Errorf("exit code = %d, want %d (success); stderr=%q", code, exitSuccess, errBuf.String())
	}
	if !strings.Contains(out.String(), "copilot dispatched") {
		t.Errorf("stdout should announce copilot dispatch; got: %q", out.String())
	}
	if strings.Contains(errBuf.String(), "Temporal unreachable") {
		t.Errorf("Copilot path must NOT dial Temporal; got: %q", errBuf.String())
	}
}

func TestRunSchedule_DefaultDriverStillDialsTemporal(t *testing.T) {
	// Regression: omitting --driver keeps the spec 097 path intact —
	// SchedulerWorkflow path dials Temporal and fails on unreachable.
	tasksMd := "- [ ] T001 [P] [US1] Implement the placeholder"
	repo := fixtureRepo(t, "091-good", tasksMd)
	fakeKernelForSchedule(t)
	var out, errBuf bytes.Buffer
	code := runSchedule(context.Background(), []string{
		"--repo-root", repo,
		"--temporal-host", "127.0.0.1:1",
		"091",
	}, &out, &errBuf)
	if code != exitRuntimeError {
		t.Errorf("exit code = %d, want %d (runtime); stderr=%q", code, exitRuntimeError, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "Temporal unreachable") {
		t.Errorf("default path must dial Temporal; got: %q", errBuf.String())
	}
}
