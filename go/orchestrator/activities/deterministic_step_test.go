package activities

import (
	"context"
	"runtime"
	"testing"
)

// Spec 076 FR-017 tests for the RunDeterministicStep activity: a mechanical
// command runs as a plain activity — no driver, no token cost. The activity
// settles the node on the command's exit code; a missing command spec settles
// the node failed rather than faulting or silently skipping.

// TestDeterministicStep_SucceedsOnZeroExit proves a command that exits zero
// yields a successful result with exit code 0.
func TestDeterministicStep_SucceedsOnZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on the POSIX `true` command")
	}
	step := NewDeterministicStep()
	got, err := step.Execute(context.Background(), DeterministicStepInput{
		NodeID:  "ok",
		Command: "true",
	})
	if err != nil {
		t.Fatalf("Execute returned an error for a clean command: %v", err)
	}
	if !got.Succeeded {
		t.Errorf("Succeeded = false, want true for a zero-exit command; result=%+v", got)
	}
	if got.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", got.ExitCode)
	}
	if got.NodeID != "ok" {
		t.Errorf("NodeID = %q, want ok", got.NodeID)
	}
}

// TestDeterministicStep_FailsOnNonZeroExit proves a command that exits
// non-zero yields an unsuccessful result — a real failed step, NOT an
// activity error (spec 076 FR-017 acceptance scenario 3).
func TestDeterministicStep_FailsOnNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on the POSIX `false` command")
	}
	step := NewDeterministicStep()
	got, err := step.Execute(context.Background(), DeterministicStepInput{
		NodeID:  "lint",
		Command: "false",
	})
	if err != nil {
		t.Fatalf("Execute returned an error for a non-zero exit; a failed step is a result, not a fault: %v", err)
	}
	if got.Succeeded {
		t.Errorf("Succeeded = true, want false for a non-zero-exit command; result=%+v", got)
	}
	if got.ExitCode == 0 {
		t.Errorf("ExitCode = 0, want non-zero for a failed command")
	}
}

// TestDeterministicStep_RunsArgsAndWorktree proves the activity runs the
// command with its declared args in the declared worktree directory — `pwd`
// run with cwd set to a temp dir echoes that dir.
func TestDeterministicStep_RunsArgsAndWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test relies on POSIX `pwd`")
	}
	dir := t.TempDir()
	step := NewDeterministicStep()
	got, err := step.Execute(context.Background(), DeterministicStepInput{
		NodeID:       "pwd-node",
		Command:      "pwd",
		WorktreePath: dir,
	})
	if err != nil {
		t.Fatalf("Execute returned an error: %v", err)
	}
	if !got.Succeeded {
		t.Fatalf("pwd should succeed; result=%+v", got)
	}
	// macOS resolves /var symlinks; accept either the temp dir or a suffix
	// match so the test is not brittle across platforms.
	if got.Output != dir && !hasSuffixPath(got.Output, dir) {
		t.Errorf("pwd output = %q, want the worktree dir %q", got.Output, dir)
	}
}

// TestDeterministicStep_EmptyCommandFails proves a deterministic node with no
// command spec settles failed — never an activity error, never silently
// skipped (spec 076 FR-017 edge case "a deterministic node carries no command
// spec"). A non-error result lets the scheduler settle exactly that node
// failed while the rest of the frontier proceeds.
func TestDeterministicStep_EmptyCommandFails(t *testing.T) {
	step := NewDeterministicStep()
	got, err := step.Execute(context.Background(), DeterministicStepInput{
		NodeID:  "no-command",
		Command: "   ", // whitespace-only is treated as empty.
	})
	if err != nil {
		t.Fatalf("an empty command must be a non-success RESULT, not an error: %v", err)
	}
	if got.Succeeded {
		t.Error("a deterministic node with no command spec must settle failed")
	}
	if got.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 (the command never ran)", got.ExitCode)
	}
	if got.Explanation == "" {
		t.Error("Explanation must name why the node could not run")
	}
}

// TestDeterministicStep_MissingBinaryFails proves a command whose binary
// cannot be started settles the step failed — a result, not an activity
// fault, so the scheduler blocks that node's dependents like any other
// failure.
func TestDeterministicStep_MissingBinaryFails(t *testing.T) {
	step := NewDeterministicStep()
	got, err := step.Execute(context.Background(), DeterministicStepInput{
		NodeID:  "ghost",
		Command: "chitin-no-such-binary-xyz",
	})
	if err != nil {
		t.Fatalf("a missing binary must be a non-success result, not an error: %v", err)
	}
	if got.Succeeded {
		t.Error("a step whose binary cannot be started must settle failed")
	}
}

// hasSuffixPath reports whether path s ends with suffix — a tiny helper so the
// pwd test tolerates symlink-resolved temp dirs across platforms.
func hasSuffixPath(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
