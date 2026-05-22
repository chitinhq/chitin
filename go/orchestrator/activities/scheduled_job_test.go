package activities

import (
	"context"
	"runtime"
	"testing"

	"github.com/chitinhq/chitin/go/orchestrator/schedules"
)

// Spec 081 US2 tests for the RunScheduledJob activity: it runs a migrated
// cron's existing script as a plain activity. The script's exit code settles
// the result — a non-zero exit is a failed RESULT, never an activity error, so
// the Schedule's next cycle still fires.

// TestScheduledJob_SucceedsOnZeroExit proves a script that exits zero yields a
// successful result with exit code 0.
func TestScheduledJob_SucceedsOnZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on the POSIX `true` command")
	}
	job := NewScheduledJob()
	got, err := job.Execute(context.Background(), schedules.JobSpec{
		Name:    "ok-job",
		Command: "/usr/bin/true",
	})
	if err != nil {
		t.Fatalf("Execute returned an error for a clean script: %v", err)
	}
	if !got.Succeeded {
		t.Errorf("Succeeded = false, want true for a zero-exit script; result=%+v", got)
	}
	if got.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", got.ExitCode)
	}
	if got.JobName != "ok-job" {
		t.Errorf("JobName = %q, want ok-job", got.JobName)
	}
}

// TestScheduledJob_FailsOnNonZeroExit proves a script that exits non-zero
// yields an unsuccessful result — a real failed run, NOT an activity error.
// The Schedule's next cycle still fires; a failed run is visible in history.
func TestScheduledJob_FailsOnNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on the POSIX `false` command")
	}
	job := NewScheduledJob()
	got, err := job.Execute(context.Background(), schedules.JobSpec{
		Name:    "failing-job",
		Command: "/usr/bin/false",
	})
	if err != nil {
		t.Fatalf("Execute returned an error for a non-zero exit; a failed run is a result, not a fault: %v", err)
	}
	if got.Succeeded {
		t.Errorf("Succeeded = true, want false for a non-zero-exit script; result=%+v", got)
	}
	if got.ExitCode == 0 {
		t.Errorf("ExitCode = 0, want non-zero for a failed script")
	}
}

// TestScheduledJob_EmptyCommandFails proves a JobSpec with no command settles
// failed — never an activity error, never silently skipped. A non-error result
// lets the Schedule's next cycle still fire.
func TestScheduledJob_EmptyCommandFails(t *testing.T) {
	job := NewScheduledJob()
	got, err := job.Execute(context.Background(), schedules.JobSpec{
		Name:    "no-command",
		Command: "   ", // whitespace-only is treated as empty.
	})
	if err != nil {
		t.Fatalf("an empty command must be a non-success RESULT, not an error: %v", err)
	}
	if got.Succeeded {
		t.Error("a JobSpec with no command must settle failed")
	}
	if got.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 (the script never ran)", got.ExitCode)
	}
	if got.Explanation == "" {
		t.Error("Explanation must name why the job could not run")
	}
}

// TestScheduledJob_MissingBinaryFails proves a script whose binary cannot be
// started settles failed — a result, not an activity fault.
func TestScheduledJob_MissingBinaryFails(t *testing.T) {
	job := NewScheduledJob()
	got, err := job.Execute(context.Background(), schedules.JobSpec{
		Name:    "ghost",
		Command: "chitin-no-such-binary-xyz",
	})
	if err != nil {
		t.Fatalf("a missing binary must be a non-success result, not an error: %v", err)
	}
	if got.Succeeded {
		t.Error("a job whose binary cannot be started must settle failed")
	}
}

// TestScheduledJob_RunsArgs proves the activity passes the JobSpec's Args to
// the script — `echo` with args echoes them on stdout.
func TestScheduledJob_RunsArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on POSIX `echo`")
	}
	job := NewScheduledJob()
	got, err := job.Execute(context.Background(), schedules.JobSpec{
		Name:    "echo-job",
		Command: "/bin/echo",
		Args:    []string{"chitin-081"},
	})
	if err != nil {
		t.Fatalf("Execute returned an error: %v", err)
	}
	if !got.Succeeded {
		t.Fatalf("echo should succeed; result=%+v", got)
	}
	if got.Output != "chitin-081" {
		t.Errorf("Output = %q, want %q — args were not passed to the script", got.Output, "chitin-081")
	}
}
