// status.go — `chitin-orchestrator status [-run-id <id>]` subcommand
// (spec 097 US2).
//
// Phase 1 stub: returns ExitUserError. Replaced in T022-T023 with the real
// list-mode (ListWorkflows + Query) and inspect-mode (QueryWorkflow) flows.

package main

import (
	"fmt"
	"os"
)

func cmdStatus(args []string) int {
	fmt.Fprintln(os.Stderr, "chitin-orchestrator status: not yet implemented (spec 097 US2; see specs/097-operator-scheduler-entrypoint/tasks.md)")
	return exitUserError
}
