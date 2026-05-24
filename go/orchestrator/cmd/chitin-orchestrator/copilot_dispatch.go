// copilot_dispatch.go — `chitin-orchestrator schedule --driver copilot`
// dispatch path (spec 099 US1).
//
// Slice 1 is the routing skeleton: the function is reachable from
// runSchedule after spec resolution + DAG validation, prints a
// placeholder success line to stdout, returns exitSuccess. No GitHub
// side effects yet. Slice 2 wires `gh issue create` per
// contracts/cli-driver-flag.md; slice 3+ adds the chain emit and
// stale-check hooks.

package main

import (
	"context"
	"fmt"
	"io"
)

// copilotDispatchInput is the closed input shape for the Copilot branch.
// Kept small on purpose — the function is invoked only from runSchedule
// after spec validation, so all repo-resolution work has already happened.
type copilotDispatchInput struct {
	SpecRef string // resolved spec ref, e.g. "099-github-native-dispatch"
	Repo    string // GitHub repo slug, e.g. "owner/name", from --repo flag
}

// runCopilotDispatch handles the --driver copilot branch of the schedule
// subcommand. Slice 1: routing skeleton only; returns success with a
// placeholder line. Slice 2 will add the real `gh issue create` call,
// `copilot_dispatched` chain emit, and proper exit code mapping per
// contracts/cli-driver-flag.md.
func runCopilotDispatch(ctx context.Context, in copilotDispatchInput, stdout, stderr io.Writer) int {
	fmt.Fprintf(stdout, "copilot dispatched: spec_ref=%s repo=%s (slice 1 stub — gh issue create lands in slice 2)\n",
		in.SpecRef, in.Repo)
	return exitSuccess
}
