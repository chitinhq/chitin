// cancel.go — `chitin-orchestrator cancel -run-id <id>` subcommand
// (spec 097 US2).
//
// Phase 1 stub: returns ExitUserError. Replaced in T024-T026 with the real
// flow (DescribeWorkflowExecution → idempotency check → CancelWorkflow →
// emit scheduler_canceled chain event).

package main

import (
	"fmt"
	"os"
)

func cmdCancel(args []string) int {
	fmt.Fprintln(os.Stderr, "chitin-orchestrator cancel: not yet implemented (spec 097 US2; see specs/097-operator-scheduler-entrypoint/tasks.md)")
	return exitUserError
}
