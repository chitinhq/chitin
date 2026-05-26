package main

import (
	"fmt"
	"io"
	"os"

	"github.com/chitinhq/chitin/go/orchestrator/schedules"
)

func cmdSchedules(args []string) int {
	return runSchedules(args, os.Stdout, os.Stderr)
}

func runSchedules(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "list" {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator schedules list")
		return exitUserError
	}
	fmt.Fprintf(stdout, "%-32s %-16s %s\n", "NAME", "CADENCE", "DESCRIPTION")
	for _, job := range schedules.Registry() {
		cadence := job.Cron
		if job.Interval > 0 {
			cadence = "every " + job.Interval.String()
		}
		fmt.Fprintf(stdout, "%-32s %-16s %s\n", job.Name, cadence, job.Description)
	}
	return exitSuccess
}
