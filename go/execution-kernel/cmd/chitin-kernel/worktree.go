package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/worktree"
)

func cmdWorktree(args []string) {
	if len(args) < 1 {
		exitErr("worktree_no_subcommand", "usage: chitin-kernel worktree {status} [flags]")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "status":
		cmdWorktreeStatus(rest)
	default:
		exitErr("worktree_unknown_subcommand", sub)
	}
}

func cmdWorktreeStatus(args []string) {
	fs := flag.NewFlagSet("worktree status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "emit one JSON object per worktree")
	stale := fs.Bool("stale", false, "filter to stale worktrees")
	pruneEligible := fs.Bool("prune-eligible", false, "emit newline-delimited stale worktree paths")
	repoDir := fs.String("repo", ".", "repository directory")
	fs.Parse(args)

	if *jsonOut && *pruneEligible {
		exitErr("worktree_invalid_flags", "--json and --prune-eligible are mutually exclusive")
	}

	rows, err := worktree.Status(context.Background(), worktree.Options{
		RepoDir: *repoDir,
		Now:     time.Now().UTC(),
		Stale:   *stale,
	})
	if err != nil {
		exitErr("worktree_status", err.Error())
	}

	switch {
	case *pruneEligible:
		fmt.Print(worktree.FormatPruneEligible(rows))
	case *jsonOut:
		out, err := worktree.FormatJSONLines(rows)
		if err != nil {
			exitErr("worktree_json", err.Error())
		}
		fmt.Print(out)
	default:
		fmt.Print(worktree.FormatText(rows))
	}

}
