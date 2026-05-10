package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// cmdDecisions dispatches `chitin-kernel decisions <op>`. Operator-facing
// surface for reading the daily gov-decisions-*.jsonl audit log.
//
// Subcommands:
//
//	recent [--dir <dir>] [--window-hours N] [--limit N]
//	  Returns up to <limit> most recent decisions (newest first) whose
//	  ts falls within the past <window-hours>. Defaults: dir=$CHITIN_HOME
//	  (or ~/.chitin), window-hours=24, limit=100.
//	worktree-diagnostics [--dir <dir>] [--window-hours N] [--limit N]
//	  Returns aggregate counts + recent sample rows for audit-only
//	  worktree diagnostics.
//
// This subcommand exists so substrate MCP servers (hermes mcp,
// openclaw mcp set) can register chitin's decision-log query as an
// MCP tool by invoking `chitin-kernel decisions recent --json` directly,
// without chitin needing to host its own MCP server. See
// docs/decisions/2026-05-08-cull-mcp-server-tools-as-subcommands.md.
func cmdDecisions(args []string) {
	if len(args) < 1 {
		exitErr("decisions_no_subcommand", "usage: chitin-kernel decisions {recent|worktree-diagnostics} [flags]")
	}
	op := args[0]
	rest := args[1:]
	switch op {
	case "recent":
		cmdDecisionsRecent(rest)
	case "worktree-diagnostics":
		cmdDecisionsWorktreeDiagnostics(rest)
	default:
		exitErr("decisions_unknown_subcommand", op)
	}
}

func cmdDecisionsRecent(args []string) {
	readArgs := parseDecisionReadFlags("decisions recent", args)
	decs, err := gov.ReadRecent(readArgs)
	if err != nil {
		exitErr("decisions_read", err.Error())
	}
	// Always emit a JSON array — never null — so MCP-bridge readers
	// can rely on .length / iteration without an empty-result branch.
	if decs == nil {
		decs = []gov.Decision{}
	}
	printJSON("decisions_marshal", decs)
}

func cmdDecisionsWorktreeDiagnostics(args []string) {
	readArgs := parseDecisionReadFlags("decisions worktree-diagnostics", args)
	summary, err := gov.ReadWorktreeDiagnosticSummary(readArgs)
	if err != nil {
		exitErr("decisions_read", err.Error())
	}
	printJSON("decisions_marshal", summary)
}

func parseDecisionReadFlags(name string, args []string) gov.ReadRecentArgs {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dir := fs.String("dir", "", "path to chitin state dir (default: $CHITIN_HOME or ~/.chitin)")
	windowHours := fs.Int("window-hours", 24, "look-back window in hours (must be > 0)")
	limit := fs.Int("limit", 100, "max decisions returned (must be > 0)")
	fs.Parse(args)

	if *windowHours <= 0 {
		exitErr("decisions_invalid_window_hours", "--window-hours must be > 0")
	}
	if *limit <= 0 {
		exitErr("decisions_invalid_limit", "--limit must be > 0")
	}

	resolved := *dir
	if resolved == "" {
		// chitinDir() honors CHITIN_HOME; defaults to ~/.chitin. Aligns
		// with where WriteLog persists decisions (gate_hook.go writes to
		// chitinDir()), so the read path can never miss a writer's dir.
		resolved = chitinDir()
	}
	return gov.ReadRecentArgs{
		Dir:         resolved,
		WindowHours: *windowHours,
		Limit:       *limit,
	}
}

func printJSON(errorKind string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		exitErr(errorKind, err.Error())
	}
	fmt.Println(string(b))
}
