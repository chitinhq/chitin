package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/chitinhq/chitin/go/orchestrator/internal/reportfreshness"
)

func cmdReports(args []string) int {
	return runReports(context.Background(), args, os.Stdout, os.Stderr)
}

func runReports(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator reports check|list [--config path]")
		return exitUserError
	}
	switch args[0] {
	case "check":
		return runReportsCheck(ctx, args[1:], stdout, stderr)
	case "list":
		return runReportsList(args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, "usage: chitin-orchestrator reports check|list [--config path]")
		return exitUserError
	}
}

func runReportsCheck(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reports check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "report freshness config path")
	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	cfg, err := reportfreshness.LoadConfigOrDefault(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUserError
	}
	res, err := reportfreshness.Check(ctx, cfg.Paths, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}
	renderReportTable(stdout, res.Rows)
	if len(res.Missing) > 0 {
		return 3
	}
	if len(res.Stale) > 0 {
		return 2
	}
	return exitSuccess
}

func runReportsList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reports list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "report freshness config path")
	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	cfg, err := reportfreshness.LoadConfigOrDefault(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUserError
	}
	fmt.Fprintf(stdout, "%-72s %8s\n", "PATH", "SLA_HOURS")
	for _, path := range cfg.Paths {
		fmt.Fprintf(stdout, "%-72s %8d\n", path.Path, path.SLAHours)
	}
	return exitSuccess
}

func renderReportTable(w io.Writer, rows []reportfreshness.ReportStatus) {
	fmt.Fprintf(w, "%-72s %8s %8s %-8s\n", "PATH", "AGE_HRS", "SLA_HRS", "STATUS")
	for _, row := range rows {
		age := "-"
		if row.Status != reportfreshness.StatusMissing {
			age = fmt.Sprintf("%.1f", row.AgeHours)
		}
		fmt.Fprintf(w, "%-72s %8s %8d %-8s\n", row.Path, age, row.SLAHours, row.Status)
	}
}
