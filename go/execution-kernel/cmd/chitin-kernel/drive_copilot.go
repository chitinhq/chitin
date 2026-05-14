package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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
	// ContinueOnError so parse failures hit the documented exit-code map
	// (return 2) instead of os.Exit'ing past it. Tests can then observe
	// the return value, and any future change to the exit-code map for
	// usage errors stays in one place.
	fs := flag.NewFlagSet("drive copilot", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cwd := fs.String("cwd", ".", "policy scope working directory")
	interactive := fs.Bool("interactive", false, "launch REPL-style interactive session")
	preflight := fs.Bool("preflight", false, "run startup validations and exit without starting session")
	verbose := fs.Bool("verbose", false, "log every Decision JSON to stderr")
	// Slice 6c: tier-driven model passes through from the activity dispatcher
	// (CHITIN_MODEL_COPILOT_T0..T4 + COPILOT_TIER_MODEL defaults). Empty
	// = SDK default (currently gpt-4.1, set in copilot.Run).
	model := fs.String("model", "", "model id to pass to Copilot SDK SessionConfig (empty = driver default)")
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

	prompt, err := resolveCopilotPrompt(fs.Args(), *interactive, os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	ctx := context.Background()
	err = copilot.Run(ctx, prompt, copilot.RunOpts{
		Cwd:         *cwd,
		Interactive: *interactive,
		Verbose:     *verbose,
		Model:       *model,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func resolveCopilotPrompt(args []string, interactive bool, stdin io.Reader) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	if interactive {
		return "", nil
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return "", fmt.Errorf("prompt required unless --interactive")
	}
	return string(data), nil
}
