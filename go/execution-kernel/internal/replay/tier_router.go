package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TierRecommendation is the output of RecommendStartingTier.
//
// Foundation for the `everything-starts-at-T0` end-state from
// PR #237's strategic entry — operators eventually delete static
// tier→driver mapping in favor of data-driven recommendations.
type TierRecommendation struct {
	ActionType         string                       `json:"action_type"`
	RecommendedTier    string                       `json:"recommended_tier"`
	Reason             string                       `json:"reason"`
	InsufficientSignal bool                         `json:"insufficient_signal"`
	PerAgent           map[string]AgentTierStats    `json:"per_agent"`
	SampleSize         int                          `json:"sample_size"`
}

// AgentTierStats — per-agent decision stats for one action_type.
type AgentTierStats struct {
	Decisions   int     `json:"decisions"`
	Allows      int     `json:"allows"`
	Denies      int     `json:"denies"`
	SuccessRate float64 `json:"success_rate"`
	MappedTier  string  `json:"mapped_tier,omitempty"`
}

// AgentToTier maps an agent_instance_id to a tier label.
// Today's static snapshot of the chitin.yaml tier→driver table.
// Future: read from chitin.yaml directly.
func AgentToTier(agent string) string {
	switch agent {
	case "local-qwen", "local-glm-flash", "local-glm", "local-deepseek":
		return "T0"
	case "copilot":
		return "T1"
	case "claude-code":
		// Tier depends on model; chain doesn't record. MVP: T3
		// (claude-code-headless sonnet, the most-frequent in
		// chitin's setup).
		return "T3"
	}
	return ""
}

// TierOrder defines "lowest tier first" iteration.
var TierOrder = []string{"T0", "T1", "T2", "T3", "T4"}

// RecommendStartingTier reads chain stats by agent for a given
// action_type and recommends the lowest tier with success_rate
// >= threshold.
//
// MVP rules:
//   - sample_size < minSampleSize → recommend T0 (default
//     starting tier; need data before being opinionated)
//   - lowest tier with success_rate >= successThreshold wins
//   - if no tier meets threshold, recommend highest tier with
//     data + flag insufficient_signal
func RecommendStartingTier(actionType string, successThreshold float64, minSampleSize int) (*TierRecommendation, error) {
	if successThreshold <= 0 {
		successThreshold = 0.85
	}
	if minSampleSize <= 0 {
		minSampleSize = 10
	}
	stats, err := perAgentStatsForActionType(actionType)
	if err != nil {
		return nil, err
	}

	rec := &TierRecommendation{
		ActionType: actionType,
		PerAgent:   stats,
	}
	for _, s := range stats {
		rec.SampleSize += s.Decisions
	}

	if rec.SampleSize < minSampleSize {
		rec.RecommendedTier = "T0"
		rec.InsufficientSignal = true
		rec.Reason = fmt.Sprintf("insufficient sample (%d < %d) — defaulting to T0",
			rec.SampleSize, minSampleSize)
		return rec, nil
	}

	tierBest := map[string]float64{}
	for agent, s := range stats {
		tier := AgentToTier(agent)
		s.MappedTier = tier
		stats[agent] = s
		if tier == "" || s.Decisions < 3 {
			continue
		}
		if cur, ok := tierBest[tier]; !ok || s.SuccessRate > cur {
			tierBest[tier] = s.SuccessRate
		}
	}
	rec.PerAgent = stats

	// Lowest tier meeting threshold wins
	for _, t := range TierOrder {
		if rate, ok := tierBest[t]; ok && rate >= successThreshold {
			rec.RecommendedTier = t
			rec.Reason = fmt.Sprintf("lowest tier with success_rate >= %.2f (got %.2f at %s)",
				successThreshold, rate, t)
			return rec, nil
		}
	}

	// No tier meets threshold — fall back to highest tier with data
	for i := len(TierOrder) - 1; i >= 0; i-- {
		if _, ok := tierBest[TierOrder[i]]; ok {
			rec.RecommendedTier = TierOrder[i]
			rec.InsufficientSignal = true
			rec.Reason = fmt.Sprintf("no tier meets %.2f threshold; recommending highest with data (%s)",
				successThreshold, TierOrder[i])
			return rec, nil
		}
	}

	rec.RecommendedTier = "T0"
	rec.InsufficientSignal = true
	rec.Reason = "no agent→tier mappings; defaulting to T0"
	return rec, nil
}

// perAgentStatsForActionType — walks chain JSONLs once,
// aggregating per-agent decision stats filtered to one action_type.
func perAgentStatsForActionType(actionType string) (map[string]AgentTierStats, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	pattern := filepath.Join(home, ".chitin", "events-*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	out := map[string]AgentTierStats{}
	for _, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var ev map[string]interface{}
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			if etype, _ := ev["event_type"].(string); etype != "decision" {
				continue
			}
			payload, _ := ev["payload"].(map[string]interface{})
			if payload == nil {
				continue
			}
			if at, _ := payload["action_type"].(string); at != actionType {
				continue
			}
			agent, _ := ev["agent_instance_id"].(string)
			if agent == "" {
				continue
			}
			s := out[agent]
			s.Decisions++
			if dec, _ := payload["decision"].(string); dec == "allow" {
				s.Allows++
			} else if dec == "deny" {
				s.Denies++
			}
			out[agent] = s
		}
	}
	for k, s := range out {
		if s.Decisions > 0 {
			s.SuccessRate = float64(s.Allows) / float64(s.Decisions)
		}
		out[k] = s
	}
	return out, nil
}
