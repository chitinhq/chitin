package activities

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DeterministicStepInput is the typed input to the RunDeterministicStep
// activity (spec 076 FR-017): a NodeKindDeterministic node's mechanical step —
// the command, its arguments, and the worktree it runs in.
//
// A deterministic step is a mappable mechanical action — gofmt, go test, a
// lint pass, a version bump — that "workflows over agents" says belongs in a
// plain workflow step, not a frontier coding agent. The scheduler dispatches
// exactly this activity for a runnable deterministic node, and NEVER runs
// driver selection for it — zero token cost.
type DeterministicStepInput struct {
	// NodeID is the DAG node the step is for — carried through for correlation
	// in the activity's telemetry and the returned explanation.
	NodeID string `json:"node_id"`
	// Command is the program to run — the deterministic node's Command field
	// (e.g. "gofmt", "go"). An empty Command is a malformed deterministic
	// node: the step cannot run and settles the node failed (spec 076 FR-017
	// edge case "a deterministic node carries no command spec").
	Command string `json:"command"`
	// Args are the arguments passed to Command — e.g. ["test", "./..."].
	Args []string `json:"args"`
	// WorktreePath is the dedicated worktree the command runs in — its working
	// directory. Empty means the command runs in the worker's own directory;
	// a worktree-required node always supplies it (spec 076 FR-008).
	WorktreePath string `json:"worktree_path"`
}

// DeterministicStepResult is the typed output of the RunDeterministicStep
// activity — the mechanical step's outcome.
type DeterministicStepResult struct {
	// NodeID echoes the executed node, for correlation.
	NodeID string `json:"node_id"`
	// Succeeded is true iff the command exited zero. The scheduler maps it to
	// dag.StatusDone, and false to dag.StatusFailed — identically to an agent
	// node's success (spec 076 FR-017, acceptance scenario 3).
	Succeeded bool `json:"succeeded"`
	// ExitCode is the command's process exit code; -1 when the command never
	// ran (empty Command, or the binary could not be started).
	ExitCode int `json:"exit_code"`
	// Output is the trimmed combined stdout of the command — the work product
	// reference for telemetry.
	Output string `json:"output"`
	// Explanation is a human-readable account of the outcome.
	Explanation string `json:"explanation"`
}

// DeterministicStep is the RunDeterministicStep activity (spec 076 FR-017).
// Running a mechanical command is a SIDE EFFECT — subprocess and filesystem
// I/O — so it MUST run in an activity, never in workflow code. The activity
// carries no startup-bound dependency: a deterministic step is a self-
// contained command, so a zero-value DeterministicStep is usable.
//
// Determinism across a workflow REPLAY is preserved by Temporal exactly as it
// is for every other activity: the activity runs once and its result is
// recorded in history; a replay reads the recorded result rather than
// re-running the command.
type DeterministicStep struct{}

// NewDeterministicStep returns a RunDeterministicStep activity. It takes no
// dependencies — a deterministic step is a self-contained command.
func NewDeterministicStep() *DeterministicStep { return &DeterministicStep{} }

// ActivityName is the stable Temporal activity name RunDeterministicStep
// registers under and the scheduler workflow dispatches to.
func (a *DeterministicStep) ActivityName() string { return "RunDeterministicStep" }

// Execute runs one deterministic node's mechanical command in its worktree.
// It is the activity function registered with the Temporal worker.
//
// A non-zero exit code is NOT an activity error — it is a normal failed step:
// the result carries Succeeded=false and the scheduler settles the node
// failed, propagating the failure to dependents exactly as a failed agent
// node does (spec 076 FR-017, acceptance scenario 3). The error return is
// reserved for an input the activity cannot act on at all — an empty Command —
// which is surfaced as a non-success result, not an error, so the scheduler
// can settle exactly that node failed while the rest of the frontier proceeds
// (spec 076 FR-017 edge case).
func (a *DeterministicStep) Execute(ctx context.Context, in DeterministicStepInput) (DeterministicStepResult, error) {
	if strings.TrimSpace(in.Command) == "" {
		// A deterministic node with no command spec cannot run. Settle it
		// failed — never silently skip it, never route it to a driver
		// (spec 076 FR-017 edge case).
		return DeterministicStepResult{
			NodeID:    in.NodeID,
			Succeeded: false,
			ExitCode:  -1,
			Explanation: fmt.Sprintf(
				"deterministic node %s has no command spec — cannot run a mechanical step", in.NodeID),
		}, nil
	}

	cmd := exec.CommandContext(ctx, in.Command, in.Args...)
	if in.WorktreePath != "" {
		cmd.Dir = in.WorktreePath
	}
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	step := commandLine(in.Command, in.Args)

	if runErr != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if ok := asExitError(runErr, &exitErr); ok {
			exitCode = exitErr.ExitCode()
		}
		explanation := fmt.Sprintf(
			"deterministic node %s: step %q exited %d", in.NodeID, step, exitCode)
		if errOut != "" {
			explanation += ": " + errOut
		}
		return DeterministicStepResult{
			NodeID:      in.NodeID,
			Succeeded:   false,
			ExitCode:    exitCode,
			Output:      out,
			Explanation: explanation,
		}, nil
	}

	explanation := fmt.Sprintf("deterministic node %s: step %q completed", in.NodeID, step)
	if errOut != "" {
		explanation += "; stderr: " + errOut
	}
	return DeterministicStepResult{
		NodeID:      in.NodeID,
		Succeeded:   true,
		ExitCode:    0,
		Output:      out,
		Explanation: explanation,
	}, nil
}

// commandLine renders a command and its arguments as a single readable string
// for the result explanation.
func commandLine(command string, args []string) string {
	if len(args) == 0 {
		return command
	}
	return command + " " + strings.Join(args, " ")
}

// asExitError reports whether err is (or wraps) an *exec.ExitError, binding it
// to target when so. It is a tiny errors.As wrapper kept here to avoid an
// extra import line at the call site.
func asExitError(err error, target **exec.ExitError) bool {
	for err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
