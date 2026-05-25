// queue.go — `chitin-orchestrator queue` operator subcommand (spec 114 US1).
//
// T001 scope: flag parsing + defaults only. The chain-event scan
// (`internal/queue/scan.go`, T002), live PR fetch (`internal/queue/live.go`,
// T003), the FR-003 filter (T004), and the three format renderers
// (T005-T007) are wired in by their respective tasks. Reason validation
// against the FR-008 closed taxonomy is added by T008. This file
// establishes the subcommand surface those tasks plug into.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// queueOpts is the parsed shape of the `queue` subcommand's argv. Owning
// the struct here lets T002-T008 add downstream behaviour without churning
// the flag-parsing layer.
type queueOpts struct {
	Repo   string        // OWNER/NAME — defaults from $CHITIN_REPO
	Since  time.Duration // window over which chain events are considered — defaults 168h (7d)
	Format string        // table | json | md — defaults table
	Reason string        // FR-008 reason filter; empty = no filter
}

// queueDefaultSince is the spec 114 FR-001 default window: 7 days.
const queueDefaultSince = 168 * time.Hour

// queueDefaultFormat is the spec 114 FR-001 default output format.
const queueDefaultFormat = "table"

func cmdQueue(args []string) int {
	return runQueue(context.Background(), args, os.Stdout, os.Stderr)
}

func runQueue(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	_ = ctx
	_ = stdout

	opts, code := parseQueueArgs(args, stderr)
	if code != exitSuccess {
		return code
	}

	// T002-T008 plug in here: scan chain events, fetch live PRs, apply
	// FR-003 filter, optionally narrow by opts.Reason, render in opts.Format.
	// Until then, exit non-zero so automation/operators don't read the
	// placeholder as a successful empty queue.
	fmt.Fprintf(stderr, "queue: not yet implemented — opts: repo=%q since=%s format=%s reason=%q\n",
		opts.Repo, opts.Since, opts.Format, opts.Reason)
	return exitRuntimeError
}

// parseQueueArgs parses the queue subcommand argv. It is the entire T001
// surface: each flag, its default, and the $CHITIN_REPO fallback.
func parseQueueArgs(args []string, stderr io.Writer) (queueOpts, int) {
	fs := flag.NewFlagSet("queue", flag.ContinueOnError)
	fs.SetOutput(stderr)

	repo := fs.String("repo", os.Getenv("CHITIN_REPO"), "GitHub repo as OWNER/NAME (default $CHITIN_REPO)")
	since := fs.Duration("since", queueDefaultSince, "consider chain events newer than this duration (e.g. 24h, 168h)")
	format := fs.String("format", queueDefaultFormat, "output format: table | json | md")
	reason := fs.String("reason", "", "filter to a single FR-008 reason kind (e.g. sibling_rebase_failed); empty = all")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator queue [--repo OWNER/NAME] [--since DURATION] [--format table|json|md] [--reason KIND]")
	}

	if err := fs.Parse(args); err != nil {
		return queueOpts{}, exitUserError
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "error: queue takes no positional arguments")
		fs.Usage()
		return queueOpts{}, exitUserError
	}

	// FR-001 constrains --format to the three renderers T005-T007 will
	// implement. Reject other values here so an operator sees the bad
	// flag at the surface rather than a confusing render-time failure.
	switch *format {
	case "table", "json", "md":
	default:
		fmt.Fprintf(stderr, "error: --format must be one of table|json|md (got %q)\n", *format)
		fs.Usage()
		return queueOpts{}, exitUserError
	}

	return queueOpts{
		Repo:   *repo,
		Since:  *since,
		Format: *format,
		Reason: *reason,
	}, exitSuccess
}
