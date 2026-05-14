package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/replay"
)

// cmdChainReplay dispatches `chitin-kernel chain replay [flags]`.
func cmdChainReplay(args []string) {
	sessionID := ""
	format := "json"
	from := ""
	to := ""
	driver := ""
	tool := ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--session="):
			sessionID = a[len("--session="):]
		case strings.HasPrefix(a, "--format="):
			format = a[len("--format="):]
		case strings.HasPrefix(a, "--from="):
			from = a[len("--from="):]
		case strings.HasPrefix(a, "--to="):
			to = a[len("--to="):]
		case strings.HasPrefix(a, "--driver="):
			driver = a[len("--driver="):]
		case strings.HasPrefix(a, "--tool="):
			tool = a[len("--tool="):]
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, `Usage: chitin-kernel chain replay [flags]

Build a structured session timeline from chain events, joined decision
rows, and optional sidecar blobs.

Flags:
  --session=<id>     session_id / chain_id to replay (or "latest")
  --format=<f>       json | text (default: json)
  --from=<ts>        include events at/after RFC3339 timestamp
  --to=<ts>          include events at/before RFC3339 timestamp
  --driver=<name>    include only one driver
  --tool=<name>      include only one tool

Examples:
  chitin-kernel chain replay --session=latest
  chitin-kernel chain replay --session=8dc93816-... --format=text
  chitin-kernel chain replay --session=latest --driver=codex --tool=shell.exec`)
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

	timeline, err := replay.BuildTimeline(replay.ReplayOptions{
		SessionID: sessionID,
		From:      from,
		To:        to,
		Driver:    driver,
		Tool:      tool,
	})
	if err != nil {
		exitErr("chain_replay_failed", err.Error())
	}

	switch format {
	case "json":
		if err := replay.WriteTimelineJSON(os.Stdout, timeline); err != nil {
			exitErr("chain_replay_json", err.Error())
		}
	case "text":
		replay.WriteTimelineText(os.Stdout, timeline)
	default:
		exitErr("chain_replay_format", "--format must be json or text")
	}
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

func cmdChainSessions(args []string) {
	recent := 10
	format := "text"
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--recent="):
			if n, err := strconv.Atoi(a[len("--recent="):]); err == nil {
				recent = n
			}
		case strings.HasPrefix(a, "--format="):
			format = a[len("--format="):]
		case a == "--help" || a == "-h":
			fmt.Fprintln(os.Stderr, `Usage: chitin-kernel chain sessions [flags]

List recent session chains.

Flags:
  --recent=<n>       number of sessions to list (default: 10)
  --format=<f>       text | json (default: text)`)
			os.Exit(0)
		}
	}
	sessions, err := replay.ListRecentSessions(recent)
	if err != nil {
		exitErr("chain_sessions_failed", err.Error())
	}
	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(sessions); err != nil {
			exitErr("chain_sessions_json", err.Error())
		}
	case "text":
		for _, s := range sessions {
			fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%d\n",
				s.LastTs, s.SessionID, emptyDash(s.Driver), emptyDash(s.Agent), s.Events)
		}
	default:
		exitErr("chain_sessions_format", "--format must be text or json")
	}
}

func emptyDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// cmdChain dispatches `chitin-kernel chain <subcommand>`.
func cmdChain(args []string) {
	if len(args) < 1 {
		exitErr("chain_no_subcommand", "usage: chitin-kernel chain <replay|sessions|summarize|related|snapshot|stats> [flags]")
	}
	switch args[0] {
	case "replay":
		cmdChainReplay(args[1:])
	case "sessions":
		cmdChainSessions(args[1:])
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
	if stats.Floundering != nil {
		f := stats.Floundering
		fmt.Printf("  floundering calibration (%d sessions):\n", f.Sessions)
		fmt.Printf("    fixed:    precision %.3f  recall %.3f  false_positive_rate %.3f  false_negative_rate %.3f\n",
			f.FixedPrecision, f.FixedRecall, f.FixedFalsePositiveRate, f.FixedFalseNegativeRate)
		fmt.Printf("    adaptive: precision %.3f  recall %.3f  false_positive_rate %.3f  false_negative_rate %.3f\n",
			f.AdaptivePrecision, f.AdaptiveRecall, f.AdaptiveFalsePositiveRate, f.AdaptiveFalseNegativeRate)
		fmt.Printf("    false_positive_reduction %.1f%%  loop_misfire_increase %.3f\n\n",
			f.FalsePositiveReduction*100, f.LoopMisfireIncrease)
	}
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
