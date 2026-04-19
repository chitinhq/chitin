package normalize

import (
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/canon"
)

// Normalize maps a raw tool call to a canonical Action.
func Normalize(tool string, input map[string]any) *Action {
	a := &Action{
		Tool:  tool,
		Input: input,
	}

	// Extract common fields from input
	if p, ok := input["file_path"].(string); ok {
		a.Path = p
	} else if p, ok := input["path"].(string); ok {
		a.Path = p
	}
	if c, ok := input["command"].(string); ok {
		a.Command = c
	}
	if c, ok := input["content"].(string); ok {
		a.Content = c
	}

	a.Type = classify(tool, a)
	return a
}

func classify(tool string, a *Action) ActionType {
	switch strings.ToLower(tool) {
	// Read-only tools
	case "read", "glob", "grep", "ls", "notebookread":
		return Read

	// Write tools
	case "write", "edit", "notebookedit":
		return Write

	// Bash: classify by command content using canon
	case "bash":
		return classifyCommand(a.Command)

	// Agent/Task tools: treat as exec
	case "agent", "task", "taskcreate", "taskupdate", "tasklist", "taskget":
		return Exec

	// Web/network tools
	case "webfetch", "websearch":
		return Net

	// MCP tools (mcp__*)
	default:
		if strings.HasPrefix(strings.ToLower(tool), "mcp__") {
			return Net
		}
		return Exec
	}
}

// dangerousTools are canonical tool or tool.action combos that are destructive.
var dangerousTools = map[string]bool{
	"rm": true, "dd": true,
	"git.push":    true, // any push is flagged; governance can whitelist
	"git.reset":   true,
	"git.clean":   true,
	"git.checkout": true, // git checkout . is destructive
	"git.restore":  true,
}

// networkTools are canonical tools that access the network.
var networkTools = map[string]bool{
	"curl": true, "wget": true, "gh": true,
	"ssh": true, "scp": true, "rsync": true,
}

// classifyCommand uses canon to parse a shell command and classify by canonical tool.
func classifyCommand(cmd string) ActionType {
	parsed := canon.ParseOne(strings.TrimSpace(cmd))

	// Build tool.action key for specific matching.
	toolAction := parsed.Tool
	if parsed.Action != "" {
		toolAction = parsed.Tool + "." + parsed.Action
	}

	// Check dangerous patterns (tool or tool.action level).
	if dangerousTools[toolAction] || dangerousTools[parsed.Tool] {
		// Additional check: git push --force variants are extra dangerous,
		// but all git push/reset/clean are flagged by default.
		return Dangerous
	}

	// Git commands that aren't dangerous.
	if parsed.Tool == "git" {
		return Git
	}

	// Network tools.
	if networkTools[parsed.Tool] {
		return Net
	}

	// Check for dangerous patterns in flags (chmod 777, etc.)
	if parsed.Tool == "chmod" {
		if v, ok := parsed.Flags["mode"]; ok && (v == "777" || v == "u+s" || v == "g+s") {
			return Dangerous
		}
		// Check positional args for dangerous modes
		for _, arg := range parsed.Args {
			if arg == "777" || arg == "u+s" || arg == "g+s" {
				return Dangerous
			}
		}
	}

	return Exec
}
