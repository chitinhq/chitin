package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/speckit"
)

// cmdSpeckitLint dispatches `chitin-kernel speckit-lint <spec-dir> [--json]`.
//
// Exit codes:
//
//	0 — spec passes all v1 checks
//	1 — one or more lint findings
//	2 — argument or IO error (missing dir, unreadable spec.md, etc.)
func cmdSpeckitLint(args []string) int {
	return runSpeckitLint(args, os.Stdout, os.Stderr)
}

// runSpeckitLint is the testable entry point — caller-provided io.Writers
// so tests can capture output without process exits.
func runSpeckitLint(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("speckit-lint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "emit findings as JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: chitin-kernel speckit-lint <spec-dir> [--json]")
		return 2
	}
	specDir := fs.Arg(0)

	findings, err := speckit.Lint(specDir)
	if err != nil {
		fmt.Fprintf(stderr, "speckit-lint: %v\n", err)
		return 2
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		out := struct {
			SpecDir  string             `json:"spec_dir"`
			Findings []speckit.Finding  `json:"findings"`
			Counts   map[string]int     `json:"counts"`
		}{
			SpecDir:  specDir,
			Findings: findings,
			Counts:   countByCheckID(findings),
		}
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(stderr, "speckit-lint: json encode: %v\n", err)
			return 2
		}
	} else {
		if len(findings) == 0 {
			fmt.Fprintf(stdout, "speckit-lint: %s — clean (0 findings)\n", specDir)
		} else {
			fmt.Fprintf(stdout, "speckit-lint: %s — %d findings\n", specDir, len(findings))
			for _, f := range findings {
				if f.Line > 0 {
					fmt.Fprintf(stdout, "  [%s] %s:%d %s\n", f.Severity, f.File, f.Line, f.Detail)
				} else {
					fmt.Fprintf(stdout, "  [%s] %s %s\n", f.Severity, f.File, f.Detail)
				}
				fmt.Fprintf(stdout, "    check: %s\n", f.CheckID)
			}
		}
	}

	if len(findings) > 0 {
		return 1
	}
	return 0
}

func countByCheckID(findings []speckit.Finding) map[string]int {
	out := map[string]int{}
	for _, f := range findings {
		out[f.CheckID]++
	}
	return out
}
