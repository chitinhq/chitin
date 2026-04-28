package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/copilot"
)

// cmdDriveCopilot implements `chitin-kernel drive copilot [flags] [prompt]`.
//
// Exit codes:
//
//	0 — success or clean lockdown
//	1 — runtime error (session failed after startup)
//	2 — startup error or usage error
func cmdDriveCopilot(args []string) int {
	fs := flag.NewFlagSet("drive copilot", flag.ExitOnError)
	cwd := fs.String("cwd", ".", "policy scope working directory")
	interactive := fs.Bool("interactive", false, "launch REPL-style interactive session")
	preflight := fs.Bool("preflight", false, "run startup validations and exit without starting session")
	verbose := fs.Bool("verbose", false, "log every Decision JSON to stderr")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: chitin-kernel drive copilot [flags] [prompt]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *preflight {
		report, err := copilot.Preflight(copilot.PreflightOpts{Cwd: *cwd})
		fmt.Print(report)
		if err != nil {
			return 2
		}
		return 0
	}

	var prompt string
	if fs.NArg() > 0 {
		prompt = strings.Join(fs.Args(), " ")
	} else if !*interactive {
		fmt.Fprintln(os.Stderr, "error: prompt required unless --interactive")
		return 2
	}

	ctx := context.Background()
	err := copilot.Run(ctx, prompt, copilot.RunOpts{
		Cwd:         *cwd,
		Interactive: *interactive,
		Verbose:     *verbose,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
