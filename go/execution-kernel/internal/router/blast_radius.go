package router

import (
	"fmt"
	"regexp"
	"strings"
)

// blastRadiusAxes computes the four-axis breakdown for a hook input.
//
// Axes (each 0.0-1.0):
//   - reversibility: 1.0=fully reversible, 0.0=irreversible
//   - scope: 0.0=single file/no-op, 1.0=mass change
//   - visibility: 0.0=local, 1.0=public-internet effect
//   - counterparties: 0.0=self only, 1.0=external service/user impacted
//
// Combined score: irreversibility weighted 0.4, scope 0.25,
// visibility 0.2, counterparties 0.15.
func blastRadiusAxes(input HookInput) (axes map[string]float64, reason string) {
	command := stringField(input.ToolInput, "command")
	filePath := stringField(input.ToolInput, "file_path")
	if filePath == "" {
		filePath = stringField(input.ToolInput, "notebook_path")
	}

	// Read-shaped tools — maximally safe
	readShaped := map[string]bool{
		"Read": true, "Glob": true, "Grep": true, "LS": true,
		"TaskGet": true, "TaskList": true, "TaskOutput": true,
		"ToolSearch": true, "AskUserQuestion": true,
		"EnterPlanMode": true, "ExitPlanMode": true,
	}
	if readShaped[input.ToolName] {
		return map[string]float64{
			"reversibility": 1.0, "scope": 0.0,
			"visibility": 0.0, "counterparties": 0.0,
		}, "read-only-tool"
	}

	// Outbound network
	outboundNet := map[string]bool{
		"WebFetch": true, "WebSearch": true,
		"PushNotification": true, "RemoteTrigger": true,
	}
	if outboundNet[input.ToolName] {
		return map[string]float64{
			"reversibility": 0.7, "scope": 0.0,
			"visibility": 0.5, "counterparties": 0.7,
		}, "outbound-network"
	}

	// Local file write
	localWrites := map[string]bool{
		"Edit": true, "Write": true, "NotebookEdit": true, "TodoWrite": true,
	}
	if localWrites[input.ToolName] {
		scope := 0.2
		reason := "local-file-write"
		if regexp.MustCompile(`/(chitin\.yaml|CLAUDE\.md|\.claude/)`).MatchString(filePath) {
			scope = 0.8
			reason = "governance-config-write"
		} else if regexp.MustCompile(`/(node_modules|\.git|dist|build)/`).MatchString(filePath) {
			scope = 0.9
			reason = "generated-or-vcs-write"
		}
		return map[string]float64{
			"reversibility": 0.6, "scope": scope,
			"visibility": 0.0, "counterparties": 0.0,
		}, reason
	}

	// Bash — shape by command pattern
	if input.ToolName == "Bash" {
		// Allowlist: chitin-kernel gate reset/evaluate are always safe
		if regexp.MustCompile(`^\s*(?:\./)?chitin-kernel\s+gate\s+(reset|evaluate)\b`).MatchString(command) {
			return map[string]float64{
				"reversibility": 1.0, "scope": 0.0,
				"visibility": 0.0, "counterparties": 0.0,
			}, "allowlisted-chitin-kernel-gate"
		}
		if regexp.MustCompile(`\brm\s+(-[rfRF]+\s+|--recursive\s+)`).MatchString(command) {
			return map[string]float64{
				"reversibility": 0.0, "scope": 1.0,
				"visibility": 0.0, "counterparties": 0.0,
			}, "recursive-delete"
		}
		if regexp.MustCompile(`\bgit\s+push\s+(--force|-f)\b`).MatchString(command) {
			return map[string]float64{
				"reversibility": 0.0, "scope": 0.5,
				"visibility": 0.9, "counterparties": 0.7,
			}, "force-push"
		}
		if regexp.MustCompile(`\bgit\s+reset\s+--hard\b`).MatchString(command) {
			return map[string]float64{
				"reversibility": 0.0, "scope": 0.4,
				"visibility": 0.0, "counterparties": 0.0,
			}, "hard-reset"
		}
		if regexp.MustCompile(`\bgit\s+push\b`).MatchString(command) {
			return map[string]float64{
				"reversibility": 0.2, "scope": 0.4,
				"visibility": 0.7, "counterparties": 0.6,
			}, "git-push"
		}
		if regexp.MustCompile(`\bgh\s+pr\s+(create|merge)\b|\bgh\s+issue\s+close\b`).MatchString(command) {
			return map[string]float64{
				"reversibility": 0.4, "scope": 0.3,
				"visibility": 0.8, "counterparties": 0.8,
			}, "github-state-change"
		}
		if regexp.MustCompile(`\b(npm|pnpm)\s+publish\b`).MatchString(command) {
			return map[string]float64{
				"reversibility": 0.0, "scope": 0.5,
				"visibility": 1.0, "counterparties": 1.0,
			}, "package-publish"
		}
		if regexp.MustCompile(`\b(curl|wget)\b`).MatchString(command) {
			return map[string]float64{
				"reversibility": 0.7, "scope": 0.0,
				"visibility": 0.3, "counterparties": 0.5,
			}, "network-out"
		}
		return map[string]float64{
			"reversibility": 0.5, "scope": 0.3,
			"visibility": 0.0, "counterparties": 0.0,
		}, "generic-shell-exec"
	}

	// Orchestration / scheduling — typically local, reversible
	orchestration := map[string]bool{
		"Task": true, "Agent": true, "Skill": true,
		"TaskCreate": true, "TaskUpdate": true, "TaskStop": true,
		"CronCreate": true, "CronDelete": true, "CronList": true,
		"ScheduleWakeup": true, "Monitor": true,
		"EnterWorktree": true, "ExitWorktree": true,
	}
	if orchestration[input.ToolName] {
		return map[string]float64{
			"reversibility": 0.5, "scope": 0.3,
			"visibility": 0.0, "counterparties": 0.0,
		}, "orchestration-shape"
	}

	// Unknown tool — moderate caution
	return map[string]float64{
		"reversibility": 0.4, "scope": 0.4,
		"visibility": 0.0, "counterparties": 0.0,
	}, fmt.Sprintf("unknown-tool:%s", input.ToolName)
}

// ScoreBlastRadius computes the combined blast-radius score for a hook
// input. Returns a HeuristicScore with `Fired` set true iff the score
// meets or exceeds the threshold.
func ScoreBlastRadius(input HookInput, threshold float64) HeuristicScore {
	axes, reason := blastRadiusAxes(input)
	score := (1-axes["reversibility"])*0.4 +
		axes["scope"]*0.25 +
		axes["visibility"]*0.2 +
		axes["counterparties"]*0.15
	// Round to 3 decimals (matches TS)
	score = floatRound(score, 3)
	return HeuristicScore{
		Score:  score,
		Fired:  score >= threshold,
		Reason: reason,
		Axis:   axes,
	}
}

func floatRound(x float64, places int) float64 {
	mul := 1.0
	for i := 0; i < places; i++ {
		mul *= 10
	}
	return float64(int64(x*mul+0.5)) / mul
}

func stringField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
