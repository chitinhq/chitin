package gov

import "sort"

type WorktreeDiagnosticSummary struct {
	Total    int                     `json:"total"`
	ByAgent  []WorktreeDiagnosticBin `json:"by_agent"`
	ByDriver []WorktreeDiagnosticBin `json:"by_driver"`
	ByAction []WorktreeDiagnosticBin `json:"by_action_type"`
	Recent   []WorktreeDiagnosticRow `json:"recent"`
}

type WorktreeDiagnosticBin struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type WorktreeDiagnosticRow struct {
	Ts             string `json:"ts"`
	Agent          string `json:"agent,omitempty"`
	Driver         string `json:"driver,omitempty"`
	ActionType     string `json:"action_type,omitempty"`
	ActionTarget   string `json:"action_target,omitempty"`
	RuleID         string `json:"rule_id,omitempty"`
	WorktreeStatus string `json:"worktree_status,omitempty"`
	WorktreeReason string `json:"worktree_reason,omitempty"`
}

func ReadWorktreeDiagnosticSummary(args ReadRecentArgs) (WorktreeDiagnosticSummary, error) {
	decisions, err := ReadRecent(args)
	if err != nil {
		return WorktreeDiagnosticSummary{}, err
	}
	summary := WorktreeDiagnosticSummary{
		ByAgent:  []WorktreeDiagnosticBin{},
		ByDriver: []WorktreeDiagnosticBin{},
		ByAction: []WorktreeDiagnosticBin{},
		Recent:   []WorktreeDiagnosticRow{},
	}
	agentCounts := map[string]int{}
	driverCounts := map[string]int{}
	actionCounts := map[string]int{}

	for _, d := range decisions {
		if d.WorktreeDiagnosticRuleID == "" {
			continue
		}
		summary.Total++
		agentCounts[countKey(d.Agent)]++
		driverCounts[countKey(d.Driver)]++
		actionCounts[countKey(string(d.Action.Type))]++
		summary.Recent = append(summary.Recent, WorktreeDiagnosticRow{
			Ts:             d.Ts,
			Agent:          d.Agent,
			Driver:         d.Driver,
			ActionType:     string(d.Action.Type),
			ActionTarget:   d.Action.Target,
			RuleID:         d.WorktreeDiagnosticRuleID,
			WorktreeStatus: d.WorktreeStatus,
			WorktreeReason: d.WorktreeReason,
		})
	}
	summary.ByAgent = sortedDiagnosticBins(agentCounts)
	summary.ByDriver = sortedDiagnosticBins(driverCounts)
	summary.ByAction = sortedDiagnosticBins(actionCounts)
	return summary, nil
}

func sortedDiagnosticBins(counts map[string]int) []WorktreeDiagnosticBin {
	out := make([]WorktreeDiagnosticBin, 0, len(counts))
	for key, count := range counts {
		out = append(out, WorktreeDiagnosticBin{Key: key, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func countKey(v string) string {
	if v == "" {
		return "(unknown)"
	}
	return v
}
