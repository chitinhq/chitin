package drift

import (
	"path/filepath"
	"strings"
)

type Event struct {
	Ts        string
	EventType string
	Payload   map[string]interface{}
}

type Observation struct {
	ToolName   string
	TargetPath string
	Command    string
}

type Intent struct {
	EntryID    string
	TaskClass  string
	FilePaths  []string
	DeclaredAt string
}

type State struct {
	TurnCount       int     `json:"turn_count"`
	PathsTotal      int     `json:"paths_total"`
	PathsOutOfScope int     `json:"paths_out_of_scope"`
	Score           float64 `json:"score"`
}

type Action string

const (
	ActionNone   Action = ""
	ActionDemote Action = "demote"
	ActionKill   Action = "kill"
)

type Config struct {
	WarnThreshold float64
	HaltThreshold float64
	MaxTurns      int
}

type Evaluation struct {
	Intent    Intent
	State     State
	Score     float64
	Reason    string
	Action    Action
	Target    string
	InScope   bool
	Detected  bool
	HasIntent bool
}

func DefaultConfig() Config {
	return Config{
		WarnThreshold: 0.3,
		HaltThreshold: 0.6,
		MaxTurns:      8,
	}
}

func Evaluate(observation Observation, events []Event, cfg Config) Evaluation {
	cfg = cfg.withDefaults()
	intent, intentIndex := ExtractIntent(events)
	out := Evaluation{
		Intent:    intent,
		Target:    normalizePath(observation.TargetPath),
		InScope:   true,
		HasIntent: intent.EntryID != "",
	}
	if !out.HasIntent || len(intent.FilePaths) == 0 {
		out.Reason = "no-intent-recorded"
		return out
	}
	state := buildState(events, intentIndex, intent.FilePaths)
	if out.Target == "" {
		state.Score = scoreState(state, cfg)
		out.State = state
		out.Score = state.Score
		out.Reason = "no-target-path"
		return out
	}
	state.TurnCount++
	state.PathsTotal++
	out.InScope = InScope(out.Target, intent.FilePaths)
	if !out.InScope {
		state.PathsOutOfScope++
		out.Detected = true
	}
	state.Score = scoreState(state, cfg)
	out.State = state
	out.Score = state.Score
	if !out.Detected {
		out.Reason = "in-scope:" + truncate(out.Target, 80)
		return out
	}
	switch {
	case out.Score >= cfg.HaltThreshold && shouldKill(observation, state):
		out.Action = ActionKill
		out.Reason = "out-of-scope-kill:" + truncate(out.Target, 80)
	case out.Score >= cfg.WarnThreshold:
		out.Action = ActionDemote
		out.Reason = "out-of-scope-demote:" + truncate(out.Target, 80)
	default:
		out.Reason = "out-of-scope-observe:" + truncate(out.Target, 80)
	}
	return out
}

func ExtractIntent(events []Event) (Intent, int) {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.EventType != "task_assignment" && ev.EventType != "intent" {
			continue
		}
		intent := Intent{DeclaredAt: ev.Ts}
		if id, ok := ev.Payload["entry_id"].(string); ok {
			intent.EntryID = id
		}
		if tc, ok := ev.Payload["task_class"].(string); ok {
			intent.TaskClass = tc
		}
		switch files := ev.Payload["file_paths"].(type) {
		case []interface{}:
			for _, f := range files {
				if s, ok := f.(string); ok && s != "" {
					intent.FilePaths = append(intent.FilePaths, normalizePath(s))
				}
			}
		case []string:
			for _, f := range files {
				if f != "" {
					intent.FilePaths = append(intent.FilePaths, normalizePath(f))
				}
			}
		}
		return intent, i
	}
	return Intent{}, -1
}

func InScope(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	np := normalizePath(path)
	for _, p := range patterns {
		if matchPattern(np, normalizePath(p)) {
			return true
		}
	}
	return false
}

func buildState(events []Event, start int, scope []string) State {
	state := State{}
	for i := start + 1; i < len(events); i++ {
		ev := events[i]
		if ev.EventType != "decision" {
			continue
		}
		decision, _ := ev.Payload["decision"].(string)
		if decision != "allow" && decision != "guide" {
			continue
		}
		target, _ := ev.Payload["action_target"].(string)
		if target == "" {
			continue
		}
		state.TurnCount++
		if !isWriteLikeAction(ev.Payload) {
			continue
		}
		state.PathsTotal++
		if !InScope(target, scope) {
			state.PathsOutOfScope++
		}
	}
	return state
}

func isWriteLikeAction(payload map[string]interface{}) bool {
	actionType, _ := payload["action_type"].(string)
	switch actionType {
	case "file.write", "git.commit", "git.push", "shell.exec":
		return true
	}
	return false
}

func scoreState(s State, cfg Config) float64 {
	var scopeScore float64
	if s.PathsTotal > 0 {
		scopeScore = float64(s.PathsOutOfScope) / float64(s.PathsTotal)
	}
	var turnScore float64
	if cfg.MaxTurns > 0 {
		turnScore = float64(s.TurnCount) / float64(cfg.MaxTurns)
		if turnScore > 1.0 {
			turnScore = 1.0
		}
	}
	return 0.6*scopeScore + 0.4*turnScore
}

func shouldKill(observation Observation, state State) bool {
	if observation.ToolName == "Bash" {
		return true
	}
	return state.PathsOutOfScope >= 2
}

func normalizePath(p string) string {
	p = filepath.ToSlash(strings.TrimSpace(p))
	if len(p) >= 2 && p[1] == ':' {
		p = p[2:]
	}
	p = strings.TrimPrefix(p, "/")
	return p
}

func matchPattern(path, pattern string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
		return strings.Contains(path, "/"+prefix+"/")
	}
	if strings.HasSuffix(pattern, "**") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "**"))
	}
	if matched, err := filepath.Match(pattern, path); err == nil && matched {
		return true
	}
	// Intent file_paths are typically repo-relative (e.g. apps/cli/src/main.ts)
	// while a tool target can be absolute (/repo/apps/cli/src/main.ts). Treat
	// the pattern as matching when it is a path-suffix of the target so scope
	// checks don't silently miss every absolute path.
	return path == pattern || strings.HasSuffix(path, "/"+pattern)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.WarnThreshold > 0 {
		d.WarnThreshold = c.WarnThreshold
	}
	if c.HaltThreshold > 0 {
		d.HaltThreshold = c.HaltThreshold
	}
	if c.MaxTurns > 0 {
		d.MaxTurns = c.MaxTurns
	}
	if d.HaltThreshold < d.WarnThreshold {
		d.HaltThreshold = d.WarnThreshold
	}
	return d
}
