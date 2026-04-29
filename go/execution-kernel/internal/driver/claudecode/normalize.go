package claudecode

import (
	"fmt"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// Normalize maps a Claude Code PreToolUse payload to a canonical gov.Action.
//
// Tool-name mapping per the spec:
//
//	Bash                                → terminal (gov.Normalize re-tags
//	                                      shell commands; rm/git/gh patterns
//	                                      get re-classified to specific types)
//	Edit, Write, NotebookEdit           → file.write
//	Read                                → file.read
//	WebFetch, WebSearch                 → http.request
//	Task                                → delegate.task
//	Glob, Grep, LS                      → file.read (browse-shaped, default-allow)
//	TodoWrite                           → file.write target="todo"
//	                                      (matches existing gov.Normalize convention)
//	<unknown>                           → ActUnknown (fail-closed at policy)
//
// The Glob/Grep/LS/TodoWrite resolution follows the plan's leaning
// recommendation: read-shaped browse tools default-allow rather than
// fail-closed. TodoWrite uses ActFileWrite target="todo" to match the
// pre-existing v2 normalize convention so policy rules need only one
// "allow if target=todo" entry.
func Normalize(in HookInput) (gov.Action, error) {
	switch in.ToolName {
	case "Bash":
		// gov.Normalize handles terminal → re-classification (rm, git push,
		// gh pr create, etc.). Pass the raw command string through under
		// the "command" key, matching the existing gov.normalizeShell shape.
		cmd := stringField(in.ToolInput, "command")
		a, err := gov.Normalize("terminal", map[string]any{"command": cmd})
		if err != nil {
			return gov.Action{}, fmt.Errorf("normalize Bash: %w", err)
		}
		a.Path = in.Cwd
		return a, nil

	case "Read":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "file_path"),
			Path:   in.Cwd,
		}, nil

	case "Edit", "Write", "NotebookEdit":
		path := stringField(in.ToolInput, "file_path")
		if path == "" {
			path = stringField(in.ToolInput, "notebook_path")
		}
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: path,
			Path:   in.Cwd,
		}, nil

	case "WebFetch":
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: stringField(in.ToolInput, "url"),
			Path:   in.Cwd,
		}, nil

	case "WebSearch":
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: stringField(in.ToolInput, "query"),
			Path:   in.Cwd,
		}, nil

	case "Task":
		// Task = subagent delegation. Use the description (human-readable
		// goal) as the Target since policy rules and audit-log readers
		// want the intent, not the verbose subagent_type or prompt.
		target := stringField(in.ToolInput, "description")
		if target == "" {
			target = stringField(in.ToolInput, "subagent_type")
		}
		return gov.Action{
			Type:   gov.ActDelegateTask,
			Target: target,
			Path:   in.Cwd,
		}, nil

	case "Glob":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "pattern"),
			Path:   in.Cwd,
		}, nil

	case "Grep":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "pattern"),
			Path:   in.Cwd,
		}, nil

	case "LS":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "path"),
			Path:   in.Cwd,
		}, nil

	case "TodoWrite":
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: "todo",
			Path:   in.Cwd,
		}, nil
	}

	// Forward-compat: future Claude Code tools we haven't seen go through
	// as ActUnknown. Policy default-deny will reject them unless the
	// operator adds a rule — fail-closed behavior.
	return gov.Action{
		Type:   gov.ActUnknown,
		Target: in.ToolName,
		Path:   in.Cwd,
		Params: in.ToolInput,
	}, nil
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
