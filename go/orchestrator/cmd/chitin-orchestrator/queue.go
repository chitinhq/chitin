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
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/internal/queue"
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

var queueNow = func() time.Time { return time.Now().UTC() }

func cmdQueue(args []string) int {
	return runQueue(context.Background(), args, os.Stdout, os.Stderr)
}

func runQueue(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, code := parseQueueArgs(args, stderr)
	if code != exitSuccess {
		return code
	}

	// FR-002: --repo is mandatory (default $CHITIN_REPO). Without it
	// FetchLive cannot resolve which repo to list — fail at the CLI
	// boundary with a self-naming error rather than a confusing
	// downstream message.
	if opts.Repo == "" {
		fmt.Fprintln(stderr, "error: --repo is required (or set $CHITIN_REPO)")
		return exitUserError
	}

	// FR-008 closed taxonomy check — reject unknown --reason at the
	// surface so the operator sees the accepted set in the error.
	if err := queue.ValidateReasonKind(opts.Reason); err != nil {
		fmt.Fprintf(stderr, "error: %s\n", err.Error())
		return exitUserError
	}

	now := queueNow().UTC()
	since := now.Add(-opts.Since)

	// 1. Chain scan — pure reader of $CHITIN_DIR/events-*.jsonl.
	chainDir := queue.ResolveChainDir()
	chainEvents, err := queue.Scan(chainDir, since)
	if err != nil {
		fmt.Fprintf(stderr, "error: scan chain events at %s: %v\n", chainDir, err)
		return exitRuntimeError
	}

	// 2. Live PR fetch — gh pr list + per-PR commit history. Both seams
	// shell out to gh in the production path; the queue_test injects
	// fakes via PATH-shadowing the gh binary.
	live, err := queue.FetchLive(ctx, opts.Repo, queue.DefaultPRLister(), queue.DefaultCommitFetcher())
	if err != nil {
		fmt.Fprintf(stderr, "error: fetch live PRs from %s: %v\n", opts.Repo, err)
		return exitRuntimeError
	}

	// 3. Compose into Entry slice via the FR-003 filter, then optionally
	// narrow by --reason. The filter's chain-first ordering survives the
	// reason filter (FilterByReason preserves order).
	entries := queue.Build(chainEvents, live, now)
	entries = queue.FilterByReason(entries, opts.Reason)

	// 4. Render to opts.Format. parseQueueArgs already validated the
	// enum, so the default branch is defensive only.
	var out string
	switch opts.Format {
	case "table":
		out = queue.FormatTable(entries, now)
	case "md":
		out = queue.FormatMarkdown(entries, now)
	case "json":
		js, jerr := queue.FormatJSON(entries)
		if jerr != nil {
			fmt.Fprintf(stderr, "error: render json: %v\n", jerr)
			return exitRuntimeError
		}
		out = js
	default:
		fmt.Fprintf(stderr, "error: unknown format %q (parseQueueArgs should have caught this)\n", opts.Format)
		return exitRuntimeError
	}

	if _, err := fmt.Fprint(stdout, out); err != nil {
		fmt.Fprintf(stderr, "error: write output: %v\n", err)
		return exitRuntimeError
	}
	if !strings.HasSuffix(out, "\n") {
		fmt.Fprintln(stdout)
	}
	return exitSuccess
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
