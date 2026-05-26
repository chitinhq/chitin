package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
)

func cmdAutoMerge(args []string) int {
	return runAutoMerge(args, os.Stdout, os.Stderr)
}

func runAutoMerge(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "status" {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator auto-merge status <PR>")
		return exitUserError
	}
	fs := flag.NewFlagSet("auto-merge status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args[1:]); err != nil {
		return exitUserError
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator auto-merge status <PR>")
		return exitUserError
	}
	pr, err := strconv.Atoi(fs.Arg(0))
	if err != nil || pr <= 0 {
		fmt.Fprintln(stderr, "error: PR must be a positive integer")
		return exitUserError
	}
	events, err := readAutoMergeEvents("", "", pr)
	if err != nil {
		fmt.Fprintf(stderr, "error: read chain: %v\n", err)
		return exitRuntimeError
	}
	if len(events) == 0 {
		fmt.Fprintf(stdout, "no auto-merge events for PR #%d\n", pr)
		return 3
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Ts < events[j].Ts })
	fmt.Fprintf(stdout, "%-30s %-28s %-18s %s\n", "TIMESTAMP", "EVENT", "OUTCOME", "DETAIL")
	lastTerminal := ""
	for _, ev := range events {
		outcome := autoMergeTerminal[ev.EventType]
		detail := stringField(ev.Payload, "reason")
		if detail == "" {
			detail = stringField(ev.Payload, "stderr_tail")
		}
		if detail == "" {
			detail = stringField(ev.Payload, "merge_sha")
		}
		fmt.Fprintf(stdout, "%-30s %-28s %-18s %s\n", ev.Ts, ev.EventType, outcome, detail)
		if outcome != "" {
			lastTerminal = outcome
		}
	}
	if lastTerminal == "succeeded" {
		return exitSuccess
	}
	return 2
}
