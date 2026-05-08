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
//   recent [--dir <dir>] [--window-hours N] [--limit N]
//     Returns up to <limit> most recent decisions (newest first) whose
//     ts falls within the past <window-hours>. Defaults: dir=$CHITIN_HOME
//     (or ~/.chitin), window-hours=24, limit=100.
//
// This subcommand exists so substrate MCP servers (hermes mcp,
// openclaw mcp set) can register chitin's decision-log query as an
// MCP tool by invoking `chitin-kernel decisions recent --json` directly,
// without chitin needing to host its own MCP server. See
// docs/decisions/2026-05-08-cull-mcp-server-tools-as-subcommands.md.
func cmdDecisions(args []string) {
	if len(args) < 1 {
		exitErr("decisions_no_subcommand", "usage: chitin-kernel decisions {recent} [flags]")
	}
	op := args[0]
	rest := args[1:]
	switch op {
	case "recent":
		cmdDecisionsRecent(rest)
	default:
		exitErr("decisions_unknown_subcommand", op)
	}
}

func cmdDecisionsRecent(args []string) {
	fs := flag.NewFlagSet("decisions recent", flag.ExitOnError)
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

	decs, err := gov.ReadRecent(gov.ReadRecentArgs{
		Dir:         resolved,
		WindowHours: *windowHours,
		Limit:       *limit,
	})
	if err != nil {
		exitErr("decisions_read", err.Error())
	}
	// Always emit a JSON array — never null — so MCP-bridge readers
	// can rely on .length / iteration without an empty-result branch.
	if decs == nil {
		decs = []gov.Decision{}
	}
	b, err := json.Marshal(decs)
	if err != nil {
		exitErr("decisions_marshal", err.Error())
	}
	fmt.Println(string(b))
}
