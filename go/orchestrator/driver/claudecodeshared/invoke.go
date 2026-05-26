package claudecodeshared

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/driver"
	"github.com/chitinhq/chitin/go/orchestrator/internal/blob"
)

// PrintArgs returns the Claude Code headless invocation argv shared by the
// hosted claudecode driver and local ollama-launched Claude Code driver.
func PrintArgs(prompt string) []string {
	return []string{"--dangerously-skip-permissions", "-p", prompt}
}

// InvocationContext applies the WorkUnit deadline to a subprocess context.
func InvocationContext(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	if deadline.IsZero() {
		return context.WithCancel(parent)
	}
	return context.WithDeadline(parent, deadline)
}

// PromptFor renders the common implementation prompt shape for Claude Code
// runtime drivers.
func PromptFor(wu driver.WorkUnit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Chitin work unit: %s\n", wu.ID)
	if wu.SpecID != "" {
		fmt.Fprintf(&b, "Spec: %s\n", wu.SpecID)
	}
	if wu.TaskID != "" {
		fmt.Fprintf(&b, "Task: %s\n", wu.TaskID)
	}
	if wu.WorktreePath != "" {
		fmt.Fprintf(&b, "Worktree: %s\n", wu.WorktreePath)
	}
	b.WriteString("\nInstructions:\n")
	b.WriteString(wu.Context)
	return b.String()
}

// ResultFromCommand maps a completed CLI subprocess into the spec-075 Result
// shape used by implementation-mode drivers.
func ResultFromCommand(ctx context.Context, store blob.Store, wu driver.WorkUnit, driverID, stdout, stderr string, runErr error) (driver.Result, error) {
	outputRef, err := blob.Externalize(ctx, store, []byte(stdout))
	if err != nil {
		return driver.Result{}, err
	}
	res := driver.Result{WorkUnitID: wu.ID, DriverID: driverID, OutputRef: outputRef}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.Status = driver.StatusTimeout
		res.Explanation = fmt.Sprintf("driver %q timed out running work unit %q", driverID, wu.ID)
		if stderr != "" {
			res.Explanation += ": " + stderr
		}
		return externalizeExplanation(ctx, store, res)
	}
	if runErr != nil {
		res.Status = driver.StatusFailed
		res.Explanation = fmt.Sprintf("driver %q failed running work unit %q: %v", driverID, wu.ID, runErr)
		if stderr != "" {
			res.Explanation += ": " + stderr
		}
		return externalizeExplanation(ctx, store, res)
	}
	res.Status = driver.StatusSucceeded
	res.Explanation = fmt.Sprintf("driver %q completed work unit %q", driverID, wu.ID)
	if stderr != "" {
		res.Explanation += "; stderr: " + stderr
	}
	return externalizeExplanation(ctx, store, res)
}

func externalizeExplanation(ctx context.Context, store blob.Store, res driver.Result) (driver.Result, error) {
	explanation, err := blob.Externalize(ctx, store, []byte(res.Explanation))
	if err != nil {
		return driver.Result{}, err
	}
	res.Explanation = explanation
	return res, nil
}
