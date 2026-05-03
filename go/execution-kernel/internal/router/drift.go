package router

import (
	"fmt"
	"strings"
)

// Drift heuristic — compares the agent's CURRENT proposed action
// against its STATED INTENT (from chain history) and signals when
// the action is out of scope.
//
// MVP shape:
//   - "Stated intent" = the most recent task_class + entry_id from
//     the chain's intent or task_assignment events (or absent)
//   - "Drift signal" fires when the action's expected_target_paths
//     don't overlap with the entry's `file:` declared paths
//
// Pure function: takes the hook input + the agent's chain events
// (already loaded), returns a HeuristicScore.
//
// Limitations (deferred to follow-up):
//   - No semantic comparison of intent vs action text (would need
//     the advisor LLM; tonight's MVP is structural only)
//   - No multi-step "did the agent forget what it was doing" check
//     (would need session memory across long-running runs)

// EntryIntent — extracted from chain history. Captures the
// declared scope of the agent's current task.
type EntryIntent struct {
	EntryID  string
	TaskClass string
	// FilePaths are the paths declared in the entry's `file:` field
	// at dispatch time, parsed into a slice. Empty when not
	// extractable.
	FilePaths []string
}

// extractIntentFromChain walks the chain looking for the most
// recent intent / task_assignment event and returns the declared
// scope. Returns zero-value EntryIntent if no signal is found.
//
// Today's chain doesn't have an explicit `intent` event type —
// this function looks at the agent_instance_id + the dispatcher's
// metadata in the run's first events. Future entries should emit
// a structured `task_assignment` event the kernel reads here.
func extractIntentFromChain(events []ChainEvent) EntryIntent {
	for _, ev := range events {
		// Heuristic v1: look at task_assignment-shaped events.
		// Today these don't exist as a typed event_type, but if a
		// `task_assignment` event_type ever lands, this is where we
		// pick it up. Otherwise we fall through to {} and drift
		// returns no-signal.
		if ev.EventType == "task_assignment" || ev.EventType == "intent" {
			intent := EntryIntent{}
			if id, ok := ev.Payload["entry_id"].(string); ok {
				intent.EntryID = id
			}
			if tc, ok := ev.Payload["task_class"].(string); ok {
				intent.TaskClass = tc
			}
			if files, ok := ev.Payload["file_paths"].([]interface{}); ok {
				for _, f := range files {
					if s, ok := f.(string); ok && s != "" {
						intent.FilePaths = append(intent.FilePaths, s)
					}
				}
			}
			return intent
		}
	}
	return EntryIntent{}
}

// targetPathFromInput extracts the path the action would touch
// (file_path / notebook_path / cwd-relative for Bash). Empty
// string when no path can be inferred.
func targetPathFromInput(input HookInput) string {
	if p := stringField(input.ToolInput, "file_path"); p != "" {
		return p
	}
	if p := stringField(input.ToolInput, "notebook_path"); p != "" {
		return p
	}
	// For Bash, scrape the command for the first path-shaped token.
	// Best-effort; many shell commands don't have a clean target.
	if input.ToolName == "Bash" {
		if cmd := stringField(input.ToolInput, "command"); cmd != "" {
			for _, tok := range strings.Fields(cmd) {
				// Skip flags and the command itself
				if strings.HasPrefix(tok, "-") {
					continue
				}
				if strings.Contains(tok, "/") || strings.HasSuffix(tok, ".ts") ||
					strings.HasSuffix(tok, ".go") || strings.HasSuffix(tok, ".py") ||
					strings.HasSuffix(tok, ".md") || strings.HasSuffix(tok, ".json") {
					return tok
				}
			}
		}
	}
	return ""
}

// pathOverlap returns true if the proposed path is "inside" any of
// the declared paths (substring match — declared paths are
// directory prefixes or specific files; proposed should be a
// descendant of one of them).
func pathOverlap(proposed string, declared []string) bool {
	if proposed == "" || len(declared) == 0 {
		return false
	}
	for _, d := range declared {
		if d == "" {
			continue
		}
		// Match if proposed contains the declared prefix
		if strings.Contains(proposed, d) {
			return true
		}
		// Or if declared contains the proposed (declared is a parent)
		if strings.Contains(d, proposed) {
			return true
		}
	}
	return false
}

// DetectDrift returns a HeuristicScore indicating whether the
// proposed action drifts from the agent's stated intent.
//
// Scores:
//   0.0 — no signal (no intent recorded, or proposed action is
//         within declared scope)
//   0.5 — soft drift (write to a path NOT in declared scope, but
//         no other red flags)
//   0.8 — hard drift (write to a path explicitly outside scope
//         AND action is high-blast-radius shape)
func DetectDrift(input HookInput, events []ChainEvent, threshold float64) HeuristicScore {
	intent := extractIntentFromChain(events)
	if intent.EntryID == "" || len(intent.FilePaths) == 0 {
		// No intent → no drift signal possible
		return HeuristicScore{Score: 0.0, Fired: false, Reason: "no-intent-recorded"}
	}
	target := targetPathFromInput(input)
	if target == "" {
		// No target path → can't compare; assume in-scope
		return HeuristicScore{Score: 0.0, Fired: false, Reason: "no-target-path"}
	}
	if pathOverlap(target, intent.FilePaths) {
		return HeuristicScore{
			Score:  0.0,
			Fired:  false,
			Reason: fmt.Sprintf("in-scope:%s", truncate(target, 60)),
		}
	}
	// Out of scope. Score by whether action is also high-blast.
	score := 0.5
	reason := fmt.Sprintf("out-of-scope-write:%s", truncate(target, 60))
	if input.ToolName == "Bash" || input.ToolName == "Edit" || input.ToolName == "Write" {
		blast := ScoreBlastRadius(input, threshold)
		if blast.Score > 0.5 {
			score = 0.8
			reason = fmt.Sprintf("out-of-scope-high-blast:%s", truncate(target, 60))
		}
	}
	return HeuristicScore{
		Score:  score,
		Fired:  score >= threshold,
		Reason: reason,
		Axis: map[string]float64{
			"intent_paths_count": float64(len(intent.FilePaths)),
		},
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
