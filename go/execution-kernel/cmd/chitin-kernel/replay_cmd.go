package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/replay"
)

// cmdChainReplay dispatches `chitin-kernel chain replay <session_id>`.
// Re-evaluates a recorded session's gate decisions against the
// current policy at cwd; prints diffs.
//
// Flags:
//
//	--session=<id>   session_id to replay (or "latest" for most recent)
//	--json           emit JSON report instead of human-readable
//	--policy-cwd=<d> cwd for policy resolution (default: $PWD)
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

// cmdChainSummarize dispatches `chitin-kernel chain summarize`.
// Produces a compact markdown block suitable for prompt injection
// into a NEXT agent's context (memory-context primitive).
func cmdChainSummarize(args []string) {
	sessionID := ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--session="):
			sessionID = a[len("--session="):]
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, "Usage: chitin-kernel chain summarize --session=<id>")
			os.Exit(0)
		}
	}
	if sessionID == "" {
		exitErr("chain_summarize_no_session", "--session=<id> required")
	}
	out, err := replay.Summarize(sessionID)
	if err != nil {
		exitErr("chain_summarize_failed", err.Error())
	}
	fmt.Print(out)
}

// cmdChainRelated dispatches `chitin-kernel chain related`.
// Lists session IDs related to a given entry hint, most-recent
// + best-match first.
func cmdChainRelated(args []string) {
	entryID := ""
	kind := ""
	maxResults := 5
	var filePaths []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--entry-id="):
			entryID = a[len("--entry-id="):]
		case a == "--entry-id" && i+1 < len(args):
			i++
			entryID = args[i]
		case strings.HasPrefix(a, "--kind="):
			kind = a[len("--kind="):]
		case a == "--kind" && i+1 < len(args):
			i++
			kind = args[i]
		case strings.HasPrefix(a, "--file="):
			filePaths = append(filePaths, a[len("--file="):])
		case a == "--file" && i+1 < len(args):
			i++
			filePaths = append(filePaths, args[i])
		case strings.HasPrefix(a, "--max="):
			if n, err := strconv.Atoi(a[len("--max="):]); err == nil {
				maxResults = n
			}
		case a == "--max" && i+1 < len(args):
			i++
			if n, err := strconv.Atoi(args[i]); err == nil {
				maxResults = n
			}
		case strings.HasPrefix(a, "--limit="):
			if n, err := strconv.Atoi(a[len("--limit="):]); err == nil {
				maxResults = n
			}
		case a == "--limit" && i+1 < len(args):
			i++
			if n, err := strconv.Atoi(args[i]); err == nil {
				maxResults = n
			}
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, "Usage: chitin-kernel chain related [--entry-id=<id>] [--kind=<event-kind>] [--file=<path>...] [--limit=<n>]")
			os.Exit(0)
		}
	}
	var ids []string
	var err error
	if kind != "" {
		ids, err = replay.FindRelatedSessionsByKind(chitinDir(), kind, maxResults)
	} else {
		ids, err = replay.FindRelatedSessionsIn(chitinDir(), entryID, filePaths, maxResults)
	}
	if err != nil {
		exitErr("chain_related_failed", err.Error())
	}
	for _, id := range ids {
		fmt.Println(id)
	}
}

// cmdChain dispatches `chitin-kernel chain <subcommand>`. Today
// `replay`, `summarize`, and `related` are wired here.
func cmdChain(args []string) {
	if len(args) < 1 {
		exitErr("chain_no_subcommand", "usage: chitin-kernel chain <replay|summarize|related|snapshot|stats> [flags]")
	}
	switch args[0] {
	case "replay":
		cmdChainReplay(args[1:])
	case "summarize":
		cmdChainSummarize(args[1:])
	case "related":
		cmdChainRelated(args[1:])
	case "snapshot":
		cmdChainSnapshot(args[1:])
	case "stats":
		cmdChainStats(args[1:])
	case "recommend-tier":
		cmdChainRecommendTier(args[1:])
	default:
		exitErr("chain_unknown_subcommand", args[0])
	}
}

// cmdChainRecommendTier — chitin-kernel chain recommend-tier
// --action-type=<t> [--threshold=<f>] [--min-sample=<n>] [--json]
//
// Reads chain history; recommends the lowest tier (T0..T4) that
// has historically met the success threshold for an action type.
// Foundation for `everything-starts-at-T0` data-driven routing.
func cmdChainRecommendTier(args []string) {
	actionType := ""
	threshold := 0.85
	minSample := 10
	jsonOut := false
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--action-type="):
			actionType = a[len("--action-type="):]
		case strings.HasPrefix(a, "--threshold="):
			if f, err := strconv.ParseFloat(a[len("--threshold="):], 64); err == nil {
				threshold = f
			}
		case strings.HasPrefix(a, "--min-sample="):
			if n, err := strconv.Atoi(a[len("--min-sample="):]); err == nil {
				minSample = n
			}
		case a == "--json":
			jsonOut = true
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, `Usage: chitin-kernel chain recommend-tier --action-type=<t> [flags]

Reads chain history; recommends the lowest tier (T0..T4) that has
historically met the success threshold for an action type.

Flags:
  --action-type=<t>    Required. e.g., file.write, shell.exec, git.commit
  --threshold=<f>      Success rate threshold (default 0.85)
  --min-sample=<n>     Minimum decisions for confidence (default 10)
  --json               Emit structured JSON

Output:
  recommended_tier:     T0..T4
  reason:               One-line explanation
  insufficient_signal:  true if recommendation is below confidence
  per_agent:            Stats by agent
  sample_size:          Total decisions across agents

Use case:
  Dispatcher reads this before dispatching an entry; uses the
  recommendation as the starting tier instead of the static
  tier→driver map. Realizes the everything-starts-at-T0 vision.`)
			os.Exit(0)
		}
	}
	if actionType == "" {
		exitErr("recommend_tier_no_action", "--action-type=<t> required")
	}
	rec, err := replay.RecommendStartingTier(actionType, threshold, minSample)
	if err != nil {
		exitErr("recommend_tier_failed", err.Error())
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rec); err != nil {
			exitErr("recommend_tier_json", err.Error())
		}
		return
	}
	fmt.Printf("chitin recommend-tier — action_type=%s\n", rec.ActionType)
	fmt.Printf("  recommended:        %s\n", rec.RecommendedTier)
	fmt.Printf("  reason:             %s\n", rec.Reason)
	fmt.Printf("  sample_size:        %d\n", rec.SampleSize)
	fmt.Printf("  insufficient:       %t\n", rec.InsufficientSignal)
	if len(rec.PerAgent) > 0 {
		fmt.Println()
		fmt.Printf("  per-agent stats:\n")
		fmt.Printf("    %-25s %5s %10s %10s %10s %10s\n", "agent", "tier", "decisions", "allows", "denies", "success%")
		for agent, s := range rec.PerAgent {
			fmt.Printf("    %-25s %5s %10d %10d %10d %9.1f%%\n",
				agent, s.MappedTier, s.Decisions, s.Allows, s.Denies, s.SuccessRate*100)
		}
	}
}

// cmdChainStats — chitin-kernel chain stats [--by=<axis>] [--json]
// Aggregates decisions across all chain JSONLs; outputs per-bucket
// counts + success rates. Foundation for tier-router-by-data.
func cmdChainStats(args []string) {
	axis := "tool_name"
	jsonOut := false
	windowHours := 0
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--by="):
			axis = a[len("--by="):]
		case a == "--by" && i+1 < len(args):
			i++
			axis = args[i]
		case strings.HasPrefix(a, "--window-hours="):
			if n, err := strconv.Atoi(a[len("--window-hours="):]); err == nil {
				windowHours = n
			}
		case a == "--window-hours" && i+1 < len(args):
			i++
			if n, err := strconv.Atoi(args[i]); err == nil {
				windowHours = n
			}
		case a == "--json":
			jsonOut = true
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, `Usage: chitin-kernel chain stats [--by=<axis>] [--window-hours=<n>] [--json]

Aggregate decision events across all sessions; output per-bucket
counts + success rates.

Axes: tool_name | action_type | rule_id | decision | agent
Default: tool_name.`)
			os.Exit(0)
		}
	}
	stats, err := replay.ComputeStatsInWindow(axis, chitinDir(), windowHours, time.Time{})
	if err != nil {
		exitErr("chain_stats_failed", err.Error())
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(stats); err != nil {
			exitErr("chain_stats_json", err.Error())
		}
		return
	}
	fmt.Printf("chitin chain stats — by %s\n", axis)
	if stats.Window != "" {
		fmt.Printf("  window: %s\n", stats.Window)
	}
	fmt.Printf("  total decisions: %d\n\n", stats.Total)
	fmt.Printf("  %-30s %10s %10s %10s %10s\n", "bucket", "decisions", "allows", "denies", "success%")
	fmt.Printf("  %-30s %10s %10s %10s %10s\n", "------", "---------", "------", "------", "--------")
	for _, k := range stats.SortedBucketKeys() {
		b := stats.Buckets[k]
		key := k
		if len(key) > 30 {
			key = key[:27] + "..."
		}
		fmt.Printf("  %-30s %10d %10d %10d %9.1f%%\n",
			key, b.Decisions, b.Allows, b.Denies, b.SuccessRate*100,
		)
	}
}
