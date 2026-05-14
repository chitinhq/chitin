package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	kexplain "github.com/chitinhq/chitin/go/execution-kernel/internal/explain"
)

func cmdExplain(args []string) {
	eventID := ""
	parseArgs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		eventID = args[0]
		parseArgs = args[1:]
	}
	fs := flag.NewFlagSet("explain", flag.ExitOnError)
	cwd := fs.String("cwd", ".", "cwd used to resolve chitin.yaml and bounds")
	policyFile := fs.String("policy-file", "", "explicit chitin.yaml path")
	dir := fs.String("dir", "", "path to chitin state dir (default: $CHITIN_HOME or ~/.chitin)")
	nearMissLimit := fs.Int("near-miss-limit", 3, "max near-miss rules to show")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: chitin-kernel explain <event-id> [--cwd <path>] [--policy-file <path>] [--dir <path>]\n\n")
		fmt.Fprintln(fs.Output(), "Emits human-readable text for operator inspection; no stable JSON output contract is currently defined.")
		fmt.Fprintln(fs.Output(), "\nFlags:")
		fs.PrintDefaults()
	}
	fs.Parse(parseArgs)

	if eventID == "" && fs.NArg() == 1 {
		eventID = fs.Arg(0)
	}
	if eventID == "" || fs.NArg() > 1 {
		exitErr("explain_missing_event_id", "usage: chitin-kernel explain <event-id> [--cwd <path>] [--policy-file <path>] [--dir <path>]")
	}

	absCwd, err := filepath.Abs(*cwd)
	if err != nil {
		exitErr("explain_bad_cwd", err.Error())
	}
	stateDir := *dir
	if stateDir == "" {
		stateDir = chitinDir()
	}
	report, err := kexplain.Build(kexplain.Args{
		StateDir:    stateDir,
		Cwd:         absCwd,
		PolicyFile:  *policyFile,
		EventID:     eventID,
		NearMissMax: *nearMissLimit,
	})
	if err != nil {
		exitErr("explain_failed", err.Error())
	}

	fmt.Fprintln(os.Stdout, renderExplain(report))
}

func renderExplain(report *kexplain.Report) string {
	var b strings.Builder
	verdict := "ALLOWED"
	if !report.Decision.Allowed {
		verdict = "BLOCKED"
	}
	fmt.Fprintf(&b, "Decision %s\n", report.EventID)
	fmt.Fprintf(&b, "%s by %s (%s)\n", verdict, report.Decision.RuleID, report.Decision.Mode)
	fmt.Fprintf(&b, "Action: %s %s\n", report.Event.ActionType, report.Event.ActionTarget)
	fmt.Fprintf(&b, "When: %s\n", report.Event.Ts)
	if report.PolicyPath != "" {
		fmt.Fprintf(&b, "Policy: %s\n", report.PolicyPath)
	}
	if report.Decision.Reason != "" {
		fmt.Fprintf(&b, "Why: %s\n", report.Decision.Reason)
	}
	if report.Decision.Suggestion != "" {
		fmt.Fprintf(&b, "Suggestion: %s\n", report.Decision.Suggestion)
	}
	if report.Decision.CorrectedCommand != "" {
		fmt.Fprintf(&b, "Safer command: %s\n", report.Decision.CorrectedCommand)
	}

	fmt.Fprintf(&b, "\nRule\n")
	if report.Rule == nil {
		fmt.Fprintf(&b, "- no current policy rule with id %q was found\n", report.Decision.RuleID)
	} else {
		fmt.Fprintf(&b, "- %s (%s, mode=%s)\n", report.Rule.ID, report.Rule.Effect, report.Rule.Mode)
		fmt.Fprintf(&b, "- %s\n", report.Rule.Summary)
		for _, check := range report.Rule.Match.Checks {
			status := "miss"
			if check.Matched {
				status = "hit"
			}
			fmt.Fprintf(&b, "- %s: %s; %s\n", check.Name, status, check.Detail)
		}
	}

	fmt.Fprintf(&b, "\nBounds\n")
	fmt.Fprintf(&b, "- status: %s\n", report.Bounds.Status)
	if report.Bounds.MaxFilesChanged > 0 || report.Bounds.MaxLinesChanged > 0 {
		fmt.Fprintf(&b, "- ceilings: files<=%d lines<=%d\n", report.Bounds.MaxFilesChanged, report.Bounds.MaxLinesChanged)
	}
	if report.Bounds.RuleID != "" {
		fmt.Fprintf(&b, "- rule: %s\n", report.Bounds.RuleID)
	}
	if report.Bounds.Reason != "" {
		fmt.Fprintf(&b, "- detail: %s\n", report.Bounds.Reason)
	}

	fmt.Fprintf(&b, "\nSignals\n")
	if report.Signals == nil {
		fmt.Fprintf(&b, "- none recorded for this decision\n")
	} else {
		fmt.Fprintf(&b, "- row: %s at %s\n", report.Signals.RuleID, report.Signals.Ts)
		fmt.Fprintf(&b, "- predicted_blast=%.2f floundering=%.2f drift=%.2f\n",
			report.Signals.PredictedBlast, report.Signals.FlounderingScore, report.Signals.DriftScore)
		if report.Signals.RoutingDecision != "" {
			fmt.Fprintf(&b, "- route: %s\n", report.Signals.RoutingDecision)
		}
	}

	fmt.Fprintf(&b, "\nNear Misses\n")
	if len(report.NearMisses) == 0 {
		fmt.Fprintf(&b, "- none\n")
	} else {
		for _, miss := range report.NearMisses {
			fmt.Fprintf(&b, "- %s (%s, score=%.2f)\n", miss.RuleID, miss.Effect, miss.Score)
			for _, failure := range miss.Failures {
				fmt.Fprintf(&b, "  %s: %s\n", failure.Name, failure.Detail)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
