package replay

import (
	"fmt"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// Summarize produces a markdown block suitable for prompt
// injection — a compact memory-context view of a session's chain
// for the NEXT agent that picks up related work.
//
// Operator framing 2026-05-03 evening: "the next agent can easily
// replay what's happened.. and we can have that replay in our
// memory layer too."
//
// Output shape:
//   ## Prior session <id> summary
//   - <N> tool calls (M denied), files touched: a.ts, b.go, ...
//   - Last decision: <ts> <tool> on <target> [allow|deny rule]
//   - Open questions / advisor nudges (from shared memory)
//   - Outcome (if known): commit_sha, error, ...
//
// Designed to be SHORT — agents have limited prompt budget.
// Cap at ~400 chars per session summary.
func Summarize(sessionID string) (string, error) {
	events := router.ReadChainEvents(sessionID)
	if len(events) == 0 {
		return "", fmt.Errorf("no chain events for session %s", sessionID)
	}

	var (
		decisionCount int
		denyCount     int
		filesTouched  = map[string]bool{}
		lastDecision  *router.ChainEvent
	)

	for i := range events {
		ev := events[i]
		if ev.EventType == "decision" {
			decisionCount++
			if dec, _ := ev.Payload["decision"].(string); dec == "deny" {
				denyCount++
			}
			lastDecision = &events[i]
			if t, _ := ev.Payload["tool_name"].(string); t != "" {
				if target, _ := ev.Payload["action_target"].(string); target != "" {
					if isLikelyFilePath(target) {
						// Cap at 5 distinct paths to keep summary short
						if len(filesTouched) < 5 {
							filesTouched[shortPath(target)] = true
						}
					}
				}
			}
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Prior session %s summary\n", shortID(sessionID))
	fmt.Fprintf(&sb, "- %d tool calls", decisionCount)
	if denyCount > 0 {
		fmt.Fprintf(&sb, " (%d denied)", denyCount)
	}
	if len(filesTouched) > 0 {
		fmt.Fprintf(&sb, ", files: %s", strings.Join(sortedKeys(filesTouched), ", "))
	}
	sb.WriteString("\n")
	if lastDecision != nil {
		tool, _ := lastDecision.Payload["tool_name"].(string)
		target, _ := lastDecision.Payload["action_target"].(string)
		dec, _ := lastDecision.Payload["decision"].(string)
		ruleID, _ := lastDecision.Payload["rule_id"].(string)
		ts := lastDecision.Ts
		extra := ""
		if dec == "deny" {
			extra = fmt.Sprintf(" [%s]", ruleID)
		}
		fmt.Fprintf(&sb, "- Last decision: %s %s on %q (%s)%s\n",
			ts, tool, shortPath(target), dec, extra,
		)
	}
	return sb.String(), nil
}

func isLikelyFilePath(s string) bool {
	if s == "" {
		return false
	}
	// Heuristic: contains /, ends with common file extension, or
	// is dotted relative path
	if strings.Contains(s, "/") {
		return true
	}
	for _, ext := range []string{".ts", ".tsx", ".js", ".go", ".py", ".md", ".json", ".yaml", ".yml", ".sh"} {
		if strings.HasSuffix(s, ext) {
			return true
		}
	}
	return false
}

func shortPath(s string) string {
	if len(s) <= 50 {
		return s
	}
	return "..." + s[len(s)-47:]
}

func shortID(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:8] + "…"
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Stable sort for deterministic output (test-friendly)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
