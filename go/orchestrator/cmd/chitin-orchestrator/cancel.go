// cancel.go — `chitin-orchestrator cancel -run-id <id>` subcommand
// (spec 097 US2; FRs 001, 008, 009, 010, 011).
//
// Flow:
//
//  1. Parse argv (flag.NewFlagSet scoped to this subcommand).
//  2. Dial Temporal (dialTemporal). Fail runtime on unreachable.
//  3. DescribeWorkflowExecution to probe state.
//     - Not found → exit 1 with "no scheduler run with run_id"
//     - Already terminal (Completed/Failed/Canceled/Terminated/TimedOut)
//       → exit 1, "already in terminal state X", NO chain event emitted
//       (idempotency per contracts/cancel-subcommand.md).
//  4. CancelWorkflow signal — Temporal accepts; workflow winds down on
//     its next scheduler tick. We return as soon as the signal is
//     accepted, not after wind-down.
//  5. emitSchedulerCanceled chain event (fail-soft per D8).
//  6. Print "canceled run_id=<id> reason=<...>" to stdout, exit 0.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

func cmdCancel(args []string) int {
	return runCancel(context.Background(), args, os.Stdout, os.Stderr)
}

func runCancel(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cancel", flag.ContinueOnError)
	fs.SetOutput(stderr)
	runID := fs.String("run-id", "", "Temporal WorkflowID of the run to cancel (== SchedulerInput.RunID)")
	reason := fs.String("reason", "", "operator-supplied cancellation reason; carried into the scheduler_canceled chain event")
	temporalHost := fs.String("temporal-host", "", "Temporal frontend host:port")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator cancel -run-id <id> [-reason <text>] [--temporal-host host:port]")
	}

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "error: cancel takes no positional arguments")
		fs.Usage()
		return exitUserError
	}
	if *runID == "" {
		fmt.Fprintln(stderr, "error: -run-id is required")
		fs.Usage()
		return exitUserError
	}

	c, host, err := dialTemporal(ctx, *temporalHost)
	if err != nil {
		fmt.Fprintf(stderr, "error: Temporal unreachable at %s — is the temporal-dev service running?\n", host)
		return exitRuntimeError
	}
	defer c.Close()

	return cancelWithClient(ctx, c, *runID, *reason, stdout, stderr)
}

// cancelWithClient is the dialed-client form of runCancel — extracted so
// future tests can pass a mock without re-dialing.
func cancelWithClient(ctx context.Context, c client.Client, runID, reason string, stdout, stderr io.Writer) int {
	// Probe state. Treat not-found as user error, terminal-state as
	// idempotent reject (also user error).
	desc, err := c.DescribeWorkflowExecution(ctx, runID, "")
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not found") || strings.Contains(msg, "NotFound") {
			fmt.Fprintf(stderr, "error: no scheduler run with run_id %q\n", runID)
			return exitUserError
		}
		fmt.Fprintf(stderr, "error: describe workflow failed: %v\n", err)
		return exitRuntimeError
	}
	status := desc.GetWorkflowExecutionInfo().GetStatus()
	if isTerminalStatus(status) {
		fmt.Fprintf(stderr, "error: run_id %q already in terminal state %q\n", runID, terminalStatusName(status))
		return exitUserError
	}

	if err := c.CancelWorkflow(ctx, runID, ""); err != nil {
		fmt.Fprintf(stderr, "error: cancel failed: %v\n", err)
		return exitRuntimeError
	}

	emitSchedulerCanceled(ctx, SchedulerCanceledPayload{
		RunID:  runID,
		Reason: reason,
	}, stderr)

	reasonOut := reason
	if reasonOut == "" {
		reasonOut = "(none)"
	}
	fmt.Fprintf(stdout, "canceled run_id=%s reason=%q\n", runID, reasonOut)
	return exitSuccess
}

// isTerminalStatus reports whether the workflow execution is in any
// terminal state. Running and ContinuedAsNew are NOT terminal — those
// are the cancellable states.
func isTerminalStatus(s enumspb.WorkflowExecutionStatus) bool {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED,
		enumspb.WORKFLOW_EXECUTION_STATUS_FAILED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return true
	}
	return false
}

// terminalStatusName returns a human-readable name for a terminal status,
// matching the strings the contract's stderr message uses ("Completed",
// "Failed", "Canceled", "Terminated", "TimedOut").
func terminalStatusName(s enumspb.WorkflowExecutionStatus) string {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "Completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "Failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "Canceled"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "Terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "TimedOut"
	}
	return s.String()
}
