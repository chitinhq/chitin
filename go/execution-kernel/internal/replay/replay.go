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

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/router"
)

// isPolicyAbsentError reports whether the gov.LoadWithInheritance
// error means "no chitin.yaml in the walk-up chain" (benign — fall
// open to heuristic-only replay). Any other error means the policy
// existed but failed to parse / failed strict-mode validation, and
// must surface to the operator.
//
// gov.LoadWithInheritance returns a wrapped error of the form
// "no_policy_found: ..." for the absent case (see internal/gov/
// inherit.go). String-match isn't ideal, but the gov package
// doesn't yet export a sentinel error for this; switching to a
// sentinel is a separate refactor.
func isPolicyAbsentError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no_policy_found")
}

// reverseToolName maps an action_type back to a representative raw
// tool name, since the chain payload's tool_name is the normalized
// action_type. ScoreBlastRadius branches on raw names ("Bash",
// "Edit", "Read") so without this reverse-map heuristic replay
// would treat every action as unknown-tool.
//
// Lossy by design — `file.read` could have been `Read`, `Glob`, or
// `Grep`; we pick the representative most likely to match policy
// rules. For replay this is good enough; the alternative (storing
// the full HookInput on every chain event) would 5x the audit log
// size for a marginal accuracy gain.
func reverseToolName(actionType, fallback string) string {
	switch actionType {
	case "shell.exec":
		return "Bash"
	case "file.write":
		return "Edit"
	case "file.read":
		return "Read"
	case "http.request":
		return "WebFetch"
	case "delegate.task":
		return "Task"
	case "git.worktree.add":
		return "EnterWorktree"
	case "git.worktree.remove":
		return "ExitWorktree"
	}
	return fallback
}

// Result is the per-event diff produced by replay.
type Result struct {
	SessionID    string         `json:"session_id"`
	TotalEvents  int            `json:"total_events"`
	Decisions    int            `json:"decision_events"`
	Diffs        []DecisionDiff `json:"diffs"`
	Summary      Summary        `json:"summary"`
	PolicyPath   string         `json:"policy_path,omitempty"`
	// GovRuleCount records how many gov.Policy rules were active at
	// replay time. 0 means no chitin.yaml resolved at policyCwd, so
	// kernel-rule replay was skipped — operator can spot a misnamed
	// --policy-cwd flag at a glance.
	GovRuleCount int `json:"gov_rule_count"`
}

// DecisionDiff captures one event whose original decision differs
// from what the current policy would produce.
type DecisionDiff struct {
	Ts             string `json:"ts"`
	ToolName       string `json:"tool_name"`
	ActionType     string `json:"action_type,omitempty"`
	ActionTarget   string `json:"action_target"`
	OriginalRule   string `json:"original_rule"`
	OriginalAllow  bool   `json:"original_allow"`
	ReplayedAllow  bool   `json:"replayed_allow"`
	ReplayedRule   string `json:"replayed_rule,omitempty"`
	ReplayedReason string `json:"replayed_reason,omitempty"`
	// Layer indicates where the difference originated: "kernel" (a
	// gov.Policy rule changed), "heuristic" (a router heuristic
	// changed), or "" if both produced the same delta.
	Layer string `json:"layer,omitempty"`
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
// Replays BOTH layers in the order they fire at runtime:
//  1. Kernel deny rules (gov.Policy.Evaluate) — reconstructs a
//     gov.Action from chain-recorded action_type+action_target and
//     evaluates against the current chitin.yaml.
//  2. Router heuristics (blast-radius today; floundering is per-
//     window so it's stateful and skipped in single-event replay).
//
// Kernel deny short-circuits: if gov.Policy denies, heuristics
// aren't run for that event — same as the live hook.
//
// Falls open on either layer: if chitin.yaml isn't found at
// policyCwd we skip kernel-rule replay; if router policy is empty
// we skip heuristic replay. The diff still surfaces whatever layer
// IS configured.
func Run(ctx context.Context, sessionID, policyCwd string) (*Result, error) {
	events := router.ReadChainEvents(sessionID)
	if len(events) == 0 {
		return nil, fmt.Errorf("replay: no chain events for session %s", sessionID)
	}

	routerPolicy := router.LoadPolicy(policyCwd)
	govPolicy, _, govErr := gov.LoadWithInheritance(policyCwd)
	// Distinguish "no chitin.yaml found" (fall open, heuristic-only
	// replay) from "chitin.yaml is malformed" (surface to operator).
	// Silent fall-open on ANY error would hide a broken policy as
	// gov_rule_count=0, which is exactly the trap operators run
	// this command to catch.
	govLoaded := govErr == nil
	if govErr != nil && !isPolicyAbsentError(govErr) {
		// Malformed yaml / strict-mode violation / parse error.
		// Surface to operator instead of silently degrading.
		return nil, fmt.Errorf("replay: failed to load policy at %s: %w", policyCwd, govErr)
	}

	result := &Result{
		SessionID:   sessionID,
		TotalEvents: len(events),
		PolicyPath:  policyCwd,
	}
	if govLoaded {
		result.GovRuleCount = len(govPolicy.Rules)
	}

	for _, ev := range events {
		if ev.EventType != "decision" {
			continue
		}
		result.Decisions++

		toolName, _ := ev.Payload["tool_name"].(string)
		actionType, _ := ev.Payload["action_type"].(string)
		actionTarget, _ := ev.Payload["action_target"].(string)
		originalRule, _ := ev.Payload["rule_id"].(string)
		originalAllow := false
		if d, ok := ev.Payload["decision"].(string); ok && d == "allow" {
			originalAllow = true
		}

		replayedAllow := true
		replayedReason := ""
		replayedRule := ""
		layer := ""

		// Layer 1: kernel deny rules. Best-effort reconstruction —
		// chain preserves action_type + target, which is exactly
		// what gov.Policy rules match against.
		if govLoaded && actionType != "" {
			act := gov.Action{
				Type:   gov.ActionType(actionType),
				Target: actionTarget,
				Path:   policyCwd,
			}
			d := govPolicy.Evaluate(act)
			if !d.Allowed {
				replayedAllow = false
				replayedRule = d.RuleID
				replayedReason = "kernel:" + d.Reason
				layer = "kernel"
			}
		}

		// Layer 2: heuristics. Only runs if kernel allowed (matches
		// live hook semantics — kernel deny short-circuits).
		//
		// Important caveat: the chain emitter writes
		// `tool_name = string(action_type)` (see
		// cmd/chitin-kernel/gate_emit.go) — i.e., chain.tool_name
		// is the NORMALIZED action_type ("shell.exec"), NOT the
		// raw Claude Code tool ("Bash"). ScoreBlastRadius matches
		// on raw tool names. We reverse-map the closed action_type
		// set to a representative raw tool to keep heuristic replay
		// approximately faithful to the live router's verdict.
		// This is lossy by design — the chain doesn't preserve the
		// full HookInput, so heuristic replay is best-effort.
		if replayedAllow {
			rawTool := reverseToolName(actionType, toolName)
			hookInput := router.HookInput{
				ToolName: rawTool,
				ToolInput: map[string]interface{}{
					"file_path": actionTarget,
					"command":   actionTarget,
				},
				Cwd:       policyCwd,
				SessionID: sessionID,
			}
			if cfg, ok := routerPolicy.Heuristics["blast_radius"]; ok && cfg.Enabled {
				score := router.ScoreBlastRadius(hookInput, cfg.Threshold)
				if score.Fired {
					replayedAllow = false
					replayedRule = "blast_radius"
					replayedReason = "blast-radius:" + score.Reason
					layer = "heuristic"
				}
			}
		}

		// Now-allowed flips: original was deny, replay is allow.
		// The live deny necessarily came from a kernel rule (today's
		// router heuristics flag actions but don't issue stand-alone
		// denies), so attribute the flip to the kernel layer for
		// operator interpretability.
		if originalAllow != replayedAllow && replayedAllow && layer == "" {
			layer = "kernel"
		}

		if originalAllow != replayedAllow {
			result.Diffs = append(result.Diffs, DecisionDiff{
				Ts:             ev.Ts,
				ToolName:       toolName,
				ActionType:     actionType,
				ActionTarget:   actionTarget,
				OriginalRule:   originalRule,
				OriginalAllow:  originalAllow,
				ReplayedAllow:  replayedAllow,
				ReplayedRule:   replayedRule,
				ReplayedReason: replayedReason,
				Layer:          layer,
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
	fmt.Fprintf(w, "  gov rules:       %d\n", r.GovRuleCount)
	fmt.Fprintf(w, "  unchanged:       %d\n", r.Summary.UnchangedDecisions)
	fmt.Fprintf(w, "  now-denied:      %d\n", r.Summary.NowDenied)
	fmt.Fprintf(w, "  now-allowed:     %d\n", r.Summary.NowAllowed)
	if r.GovRuleCount == 0 {
		fmt.Fprintf(w, "  note:            no chitin.yaml at %q — kernel-rule replay skipped (heuristic-only)\n", r.PolicyPath)
	}
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
		layerTag := d.Layer
		if layerTag == "" {
			layerTag = "-"
		}
		fmt.Fprintf(w, "    [%s] %s on %q  (orig: %s/%s)  %s  [%s/%s] %s\n",
			d.Ts, d.ToolName, target,
			boolToStr(d.OriginalAllow), d.OriginalRule,
			dir, layerTag, d.ReplayedRule, d.ReplayedReason,
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
