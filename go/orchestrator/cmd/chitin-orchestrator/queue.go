// queue.go — spec 114 `queue` operator subcommand.
//
// T001 scope: flag parsing and default resolution only. The chain-event
// scan (T002), live PR composition (T003), filter (T004), and the three
// output formats (T005-T007) land in their own tasks and plug into the
// queueArgs struct this file produces. T008 layers reason-taxonomy
// validation on top.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// queueArgs is the resolved set of inputs after flag parsing + default
// resolution. Later tasks (T002-T008) consume this struct.
type queueArgs struct {
	Repo   string
	Since  time.Duration
	Format string
	Reason string
}

// cmdQueue is the entrypoint dispatched from runMain.
func cmdQueue(args []string) int {
	return runQueue(context.Background(), args, os.Stdout, os.Stderr)
}

// runQueue parses flags and applies defaults. The remaining work (scan,
// compose, filter, format) is wired in by subsequent T0xx tasks; this
// stub exits cleanly so the dispatcher is callable end-to-end while the
// rest of spec 114 lands.
func runQueue(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	_ = ctx

	fs := flag.NewFlagSet("queue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.String("repo", os.Getenv("CHITIN_REPO"), "GitHub repo slug owner/name (default: $CHITIN_REPO)")
	since := fs.String("since", "168h", "lookback window for chain-event scan (Go duration; default 168h = 7d)")
	format := fs.String("format", "table", "output format: table|json|md (default table)")
	reason := fs.String("reason", "", "filter to a single escalation reason kind (see spec 114 FR-008)")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator queue [--repo owner/name] [--since DURATION] [--format table|json|md] [--reason KIND]")
	}

	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "error: unexpected positional argument %q\n", fs.Arg(0))
		fs.Usage()
		return exitUserError
	}

	sinceDur, err := time.ParseDuration(*since)
	if err != nil {
		fmt.Fprintf(stderr, "error: --since %q is not a valid Go duration: %v\n", *since, err)
		return exitUserError
	}

	if *repo == "" {
		fmt.Fprintln(stderr, "error: --repo not set and $CHITIN_REPO is empty")
		return exitUserError
	}

	qa := queueArgs{
		Repo:   *repo,
		Since:  sinceDur,
		Format: *format,
		Reason: *reason,
	}
	_ = qa

	fmt.Fprintln(stderr, "chitin-orchestrator queue: scan/compose/format pending — spec 114 T002-T008")
	return exitSuccess
}
