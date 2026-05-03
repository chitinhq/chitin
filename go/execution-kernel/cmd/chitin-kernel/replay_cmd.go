package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/replay"
)

// cmdChainReplay dispatches `chitin-kernel chain replay <session_id>`.
// Re-evaluates a recorded session's gate decisions against the
// current policy at cwd; prints diffs.
//
// Flags:
//   --session=<id>   session_id to replay (or "latest" for most recent)
//   --json           emit JSON report instead of human-readable
//   --policy-cwd=<d> cwd for policy resolution (default: $PWD)
func cmdChainReplay(args []string) {
	sessionID := ""
	jsonOut := false
	policyCwd, _ := os.Getwd()
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--session="):
			sessionID = a[len("--session="):]
		case a == "--json":
			jsonOut = true
		case strings.HasPrefix(a, "--policy-cwd="):
			policyCwd = a[len("--policy-cwd="):]
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, `Usage: chitin-kernel chain replay [flags]

Re-evaluate a recorded session's gate decisions against the current
router policy at the specified cwd.

Flags:
  --session=<id>     session_id to replay (or "latest" for the
                     most recently-modified ~/.chitin/events-*.jsonl)
  --json             emit JSON report instead of human-readable
  --policy-cwd=<d>   cwd for policy resolution (default: $PWD)

Examples:
  chitin-kernel chain replay --session=latest
  chitin-kernel chain replay --session=8dc93816-... --json | jq
  chitin-kernel chain replay --session=latest --policy-cwd=/path/to/proposed/policy/dir`)
			os.Exit(0)
		}
	}
	if sessionID == "" {
		exitErr("chain_replay_no_session", "--session=<id> required (or --session=latest)")
	}
	if sessionID == "latest" {
		latest, err := replay.FindMostRecentSession()
		if err != nil {
			exitErr("chain_replay_no_latest", err.Error())
		}
		sessionID = latest
	}

	result, err := replay.Run(context.Background(), sessionID, policyCwd)
	if err != nil {
		exitErr("chain_replay_failed", err.Error())
	}

	if jsonOut {
		if err := replay.WriteJSONReport(os.Stdout, result); err != nil {
			exitErr("chain_replay_json", err.Error())
		}
		return
	}
	replay.WriteHumanReport(os.Stdout, result)
}

// cmdChain dispatches `chitin-kernel chain <subcommand>`. Today
// only `replay` is wired here; chain-info/chain-verify already
// exist as top-level subcommands and will be folded in over time.
func cmdChain(args []string) {
	if len(args) < 1 {
		exitErr("chain_no_subcommand", "usage: chitin-kernel chain <replay> [flags]")
	}
	switch args[0] {
	case "replay":
		cmdChainReplay(args[1:])
	default:
		exitErr("chain_unknown_subcommand", args[0])
	}
}
