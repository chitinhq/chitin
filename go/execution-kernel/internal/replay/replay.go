// Package replay re-evaluates a recorded session's gate decisions
// against the CURRENT policy. Output: per-event decision diff
// (today_decision vs replay_decision).
//
// Use cases:
//   - "Did our policy change break this session?" — replay against
//     the new policy, see which formerly-allowed actions are now
//     denied (or vice versa).
//   - Counterfactual analysis — "if we had had this stricter rule
//     last week, what would have happened?"
//   - Policy regression testing — replay a corpus of sessions
//     against a proposed policy change before deploying it.
package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// Result is the per-event diff produced by replay.
type Result struct {
	SessionID    string         `json:"session_id"`
	TotalEvents  int            `json:"total_events"`
	Decisions    int            `json:"decision_events"`
	Diffs        []DecisionDiff `json:"diffs"`
	Summary      Summary        `json:"summary"`
	PolicyPath   string         `json:"policy_path,omitempty"`
}

// DecisionDiff captures one event whose original decision differs
// from what the current policy would produce.
type DecisionDiff struct {
	Ts             string `json:"ts"`
	ToolName       string `json:"tool_name"`
	ActionTarget   string `json:"action_target"`
	OriginalRule   string `json:"original_rule"`
	OriginalAllow  bool   `json:"original_allow"`
	ReplayedAllow  bool   `json:"replayed_allow"`
	ReplayedReason string `json:"replayed_reason,omitempty"`
}

// Summary aggregates the diff counts.
type Summary struct {
	UnchangedDecisions int `json:"unchanged"`
	NowDenied          int `json:"now_denied"`
	NowAllowed         int `json:"now_allowed"`
}

// Run replays the chain events for a session against the current
// policy at policyCwd. Returns the diff result.
//
// Note: today's MVP replays HEURISTIC decisions only. Replaying
// FULL kernel deny rules requires loading gov.Policy + replicating
// the chain-original action's normalization — left as a
// follow-up entry. Today's diff catches blast-radius / floundering
// changes which is the meaningful operator-facing signal.
func Run(ctx context.Context, sessionID, policyCwd string) (*Result, error) {
	events := router.ReadChainEvents(sessionID)
	if len(events) == 0 {
		return nil, fmt.Errorf("replay: no chain events for session %s", sessionID)
	}

	policy := router.LoadPolicy(policyCwd)

	result := &Result{
		SessionID:   sessionID,
		TotalEvents: len(events),
	}

	for _, ev := range events {
		if ev.EventType != "decision" {
			continue
		}
		result.Decisions++

		toolName, _ := ev.Payload["tool_name"].(string)
		actionTarget, _ := ev.Payload["action_target"].(string)
		originalRule, _ := ev.Payload["rule_id"].(string)
		originalAllow := false
		if d, ok := ev.Payload["decision"].(string); ok && d == "allow" {
			originalAllow = true
		}

		// Replay heuristic decisions: reconstruct a HookInput from
		// the recorded fields and re-score with current policy.
		hookInput := router.HookInput{
			ToolName: toolName,
			// Best-effort reconstruction — chain doesn't preserve full
			// tool_input, only the normalized action_type + target.
			ToolInput: map[string]interface{}{
				"file_path": actionTarget,
				"command":   actionTarget,
			},
			Cwd:       policyCwd,
			SessionID: sessionID,
		}

		replayedAllow := true
		replayedReason := ""
		if cfg, ok := policy.Heuristics["blast_radius"]; ok && cfg.Enabled {
			score := router.ScoreBlastRadius(hookInput, cfg.Threshold)
			if score.Fired {
				replayedAllow = false
				replayedReason = "blast-radius:" + score.Reason
			}
		}

		if originalAllow != replayedAllow {
			result.Diffs = append(result.Diffs, DecisionDiff{
				Ts:             ev.Ts,
				ToolName:       toolName,
				ActionTarget:   actionTarget,
				OriginalRule:   originalRule,
				OriginalAllow:  originalAllow,
				ReplayedAllow:  replayedAllow,
				ReplayedReason: replayedReason,
			})
			if !replayedAllow {
				result.Summary.NowDenied++
			} else {
				result.Summary.NowAllowed++
			}
		} else {
			result.Summary.UnchangedDecisions++
		}
	}

	return result, nil
}

// WriteHumanReport writes a human-readable report of a Result to w.
func WriteHumanReport(w io.Writer, r *Result) {
	fmt.Fprintf(w, "chitin chain replay — session %s\n", r.SessionID)
	fmt.Fprintf(w, "  total events:    %d\n", r.TotalEvents)
	fmt.Fprintf(w, "  decisions:       %d\n", r.Decisions)
	fmt.Fprintf(w, "  unchanged:       %d\n", r.Summary.UnchangedDecisions)
	fmt.Fprintf(w, "  now-denied:      %d\n", r.Summary.NowDenied)
	fmt.Fprintf(w, "  now-allowed:     %d\n", r.Summary.NowAllowed)
	if len(r.Diffs) == 0 {
		fmt.Fprintln(w, "\n  No diffs — current policy produces the same decisions as the recorded run.")
		return
	}
	fmt.Fprintln(w, "\n  diffs:")
	for _, d := range r.Diffs {
		dir := "→"
		if d.OriginalAllow && !d.ReplayedAllow {
			dir = "→ NOW DENIED"
		} else if !d.OriginalAllow && d.ReplayedAllow {
			dir = "→ NOW ALLOWED"
		}
		target := d.ActionTarget
		if len(target) > 60 {
			target = target[:60] + "…"
		}
		fmt.Fprintf(w, "    [%s] %s on %q  (orig: %s/%s)  %s  (reason: %s)\n",
			d.Ts, d.ToolName, target,
			boolToStr(d.OriginalAllow), d.OriginalRule,
			dir, d.ReplayedReason,
		)
	}
}

func boolToStr(b bool) string {
	if b {
		return "allow"
	}
	return "deny"
}

// WriteJSONReport writes the Result as pretty JSON to w.
func WriteJSONReport(w io.Writer, r *Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// FindMostRecentSession scans ~/.chitin/events-*.jsonl and returns
// the session_id of the most recently-modified file. Helper for
// CLI ergonomics ("replay the last session").
func FindMostRecentSession() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	pattern := filepath.Join(home, ".chitin", "events-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no chain event files at %s", pattern)
	}
	var newest string
	var newestMod int64
	for _, p := range matches {
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		mt := st.ModTime().Unix()
		if mt > newestMod {
			newestMod = mt
			newest = p
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no readable chain event files")
	}
	// Extract session_id from filename
	base := filepath.Base(newest)
	base = strings.TrimPrefix(base, "events-")
	base = strings.TrimSuffix(base, ".jsonl")
	return base, nil
}
