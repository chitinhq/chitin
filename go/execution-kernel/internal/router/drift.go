package router

import (
	"strings"

	kdrift "github.com/chitinhq/chitin/go/execution-kernel/internal/drift"
)

type DriftResult struct {
	Score HeuristicScore
	Eval  kdrift.Evaluation
}

func EvaluateDrift(input HookInput, events []ChainEvent, cfg HeuristicConfig) DriftResult {
	observation := kdrift.Observation{
		ToolName:   input.ToolName,
		TargetPath: targetPathFromInput(input),
		Command:    stringField(input.ToolInput, "command"),
	}
	eval := kdrift.Evaluate(observation, mapChainEvents(events), kdrift.Config{
		WarnThreshold: cfg.WarnThreshold,
		HaltThreshold: firstPositive(cfg.HaltThreshold, cfg.Threshold),
		MaxTurns:      cfg.MaxTurns,
	})
	score := HeuristicScore{
		Score:  eval.Score,
		Fired:  eval.Action != kdrift.ActionNone,
		Reason: eval.Reason,
		Axis: map[string]float64{
			"intent_paths_count": float64(len(eval.Intent.FilePaths)),
			"turn_count":         float64(eval.State.TurnCount),
			"out_of_scope":       float64(eval.State.PathsOutOfScope),
		},
	}
	return DriftResult{Score: score, Eval: eval}
}

func DetectDrift(input HookInput, events []ChainEvent, threshold float64) HeuristicScore {
	return EvaluateDrift(input, events, HeuristicConfig{Threshold: threshold}).Score
}

func targetPathFromInput(input HookInput) string {
	if p := stringField(input.ToolInput, "file_path"); p != "" {
		return p
	}
	if p := stringField(input.ToolInput, "notebook_path"); p != "" {
		return p
	}
	if input.ToolName == "Bash" {
		if cmd := stringField(input.ToolInput, "command"); cmd != "" {
			for _, tok := range strings.Fields(cmd) {
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

func mapChainEvents(events []ChainEvent) []kdrift.Event {
	out := make([]kdrift.Event, 0, len(events))
	for _, ev := range events {
		out = append(out, kdrift.Event{
			Ts:        ev.Ts,
			EventType: ev.EventType,
			Payload:   ev.Payload,
		})
	}
	return out
}

func firstPositive(vs ...float64) float64 {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

func pathOverlap(proposed string, declared []string) bool {
	if proposed == "" || len(declared) == 0 {
		return false
	}
	proposed = strings.Trim(strings.ReplaceAll(proposed, "\\", "/"), "/")
	for _, raw := range declared {
		decl := strings.Trim(strings.ReplaceAll(raw, "\\", "/"), "/")
		if decl == "" {
			continue
		}
		if strings.Contains(proposed, decl) || strings.Contains(decl, proposed) {
			return true
		}
	}
	return false
}
