// schedule.go — `chitin-orchestrator schedule <spec-ref>` subcommand
// (spec 097 US1).
//
// Phase 1 stub: returns ExitUserError with a clear "not yet implemented"
// message so the dispatcher in main.go can route to this handler before
// the full implementation lands in Phase 3. Replaced in T016-T018 with
// the real flow (resolve spec ref → compile DAG → validate → ExecuteWorkflow
// → emit scheduler_started chain event).

package main

import (
	"fmt"
	"os"
)

func cmdSchedule(args []string) int {
	fmt.Fprintln(os.Stderr, "chitin-orchestrator schedule: not yet implemented (spec 097 US1; see specs/097-operator-scheduler-entrypoint/tasks.md)")
	return exitUserError
}
