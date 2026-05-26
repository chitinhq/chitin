package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

// cmdQueue handles `chitin-orchestrator queue [flags]`.
func cmdQueue(args []string) {
	fs := flag.NewFlagSet("queue", flag.ExitOnError)
	repo := fs.String("repo", os.Getenv("CHITIN_REPO"), "repo to operate on (default: $CHITIN_REPO)")
	since := fs.String("since", "168h", "look back window (default: 168h)")
	format := fs.String("format", "table", "output format: table|json|csv (default: table)")
	reason := fs.String("reason", "", "reason for queue invocation (optional)")
	fs.Parse(args)

	// Validate since duration
	if _, err := time.ParseDuration(*since); err != nil {
		fmt.Fprintf(os.Stderr, "invalid --since duration: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("repo: %s\n", *repo)
	fmt.Printf("since: %s\n", *since)
	fmt.Printf("format: %s\n", *format)
	fmt.Printf("reason: %s\n", *reason)
	// TODO: Implement queue logic
}
