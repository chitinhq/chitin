package claudecode

import (
	"fmt"
	"io"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// mcpToolPrefix is Claude Code's wire format for MCP tool names:
// `mcp__<serverName>__<toolName>`. Detected here so MCP calls
// route to gov.ActMCPCall (matching the Copilot SDK driver) rather
// than falling to ActUnknown + default-deny — the latter locks out
// every MCP-using session under enforce mode, which silently
// regresses MCP support for any operator running with the strict
// rule set.
const mcpToolPrefix = "mcp__"

// parseMCPToolName splits "mcp__serverName__toolName" on the FIRST
// `__` separator after the prefix. Equivalent to SplitN(rest, "__", 2);
// the explicit Index makes the "first separator" intent obvious in
// code review. Tool names commonly contain `__` because some MCP
// servers compose names like `mcp__filesystem__read_file__binary`,
// and we want server="filesystem" / tool="read_file__binary" — not
// a 4-way split.
//
// Returns:
//
//	("server", "tool", true)  for "mcp__server__tool"
//	("server", "",     true)  for "mcp__server" (server-only call;
//	                           policy can still match on server)
//	("",       "",     false) for inputs without the mcp__ prefix
func parseMCPToolName(s string) (server, tool string, ok bool) {
	if !strings.HasPrefix(s, mcpToolPrefix) {
		return "", "", false
	}
	rest := s[len(mcpToolPrefix):]
	idx := strings.Index(rest, "__")
	if idx < 0 {
		return rest, "", true
	}
	return rest[:idx], rest[idx+2:], true
}

// warnSink is the destination for non-fatal warnings emitted during
// Normalize (e.g., wrong-type fields). nil → silent. Set by tests and
// by the cmd-layer to capture warnings on stderr.
//
// Package-global because Normalize's signature is committed by the
// hook protocol, but the additional channel is operator-facing
// telemetry the tests want to verify. A field on a future struct-form
// Normalizer would be cleaner; defer until we have multiple call sites.
var warnSink io.Writer

// SetWarnSink directs Normalize's non-fatal warnings (wrong-type
// fields, etc.) to w. Call from cmd-layer wiring. Pass nil to silence.
func SetWarnSink(w io.Writer) { warnSink = w }

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
//	Task, Agent, Skill                  → delegate.task (subagent dispatch
//	                                      + skill invocation are the same
//	                                      shape: cede a tool budget to a
//	                                      subordinate agent)
//	TaskCreate, TaskUpdate              → file.write target="task-state"
//	TaskGet, TaskList, TaskOutput,
//	  ToolSearch, AskUserQuestion       → file.read (browse-shape; no
//	                                      external side effect)
//	TaskStop                            → delegate.task (terminating an
//	                                      earlier delegation; same shape)
//	Monitor                             → shell.exec (spawns subprocess)
//	EnterPlanMode, ExitPlanMode         → file.read (mode toggle, no side
//	                                      effect on the host)
//	EnterWorktree, ExitWorktree         → git.worktree.add / .remove
//	PushNotification, RemoteTrigger     → http.request (external send)
//	CronCreate                          → file.write target="cron"
//	CronDelete                          → file.delete target="cron"
//	CronList, ScheduleWakeup            → file.read / file.write (schedule
//	                                      state change is write-shape)
//	Glob, Grep, LS                      → file.read (browse-shaped, default-allow)
//	TodoWrite                           → file.write target="todo"
//	                                      (matches existing gov.Normalize convention)
//	lowercase generic leak fallback     → re-normalized via gov.Normalize
//	                                      for proven cross-driver leaks
//	                                      like `read` and `exec`; warning
//	                                      emitted so wiring gets fixed
//	<unknown>                           → ActUnknown (fail-closed at policy)
//
// Issue #69: closed-enum coverage gap. Pre-fix, modern Claude Code
// tools (Agent, Skill, TaskCreate-family, etc.) all fell to ActUnknown
// → fail-closed → blocked under enforce mode, forcing operators to
// monitor mode globally. Now each name has a deliberate mapping; new
// tools added by Anthropic still hit ActUnknown (correct fail-closed
// behavior), but the common ones don't surprise the operator.
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

	case "Task", "Agent", "Skill":
		// Subagent delegation OR skill invocation. Both cede a tool
		// budget to a subordinate agent — same policy shape. Target
		// is the human-readable intent (description / subagent_type /
		// skill name) so audit-log readers see what the agent meant
		// to do, not the verbose prompt.
		target := stringField(in.ToolInput, "description")
		if target == "" {
			target = stringField(in.ToolInput, "subagent_type")
		}
		if target == "" {
			target = stringField(in.ToolInput, "skill")
		}
		return gov.Action{
			Type:   gov.ActDelegateTask,
			Target: target,
			Path:   in.Cwd,
		}, nil

	case "TaskStop":
		// Terminating an earlier delegation — same delegate-task
		// policy shape. Target is the task id being stopped.
		return gov.Action{
			Type:   gov.ActDelegateTask,
			Target: stringField(in.ToolInput, "task_id"),
			Path:   in.Cwd,
		}, nil

	case "TaskCreate", "TaskUpdate":
		// Mutate a task-list entry. file.write target="task-state"
		// matches the convention TodoWrite uses (single-target write
		// the operator can scope-allow with one rule).
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: "task-state",
			Path:   in.Cwd,
		}, nil

	case "TaskGet", "TaskList", "TaskOutput", "ToolSearch", "AskUserQuestion":
		// Browse-shape; no external side effect. Default-allow under
		// the existing read policy.
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: in.ToolName,
			Path:   in.Cwd,
		}, nil

	case "Monitor":
		// Spawns a subprocess + streams events. shell.exec shape so
		// the operator can scope-allow with the existing exec rules.
		return gov.Action{
			Type:   gov.ActShellExec,
			Target: stringField(in.ToolInput, "command"),
			Path:   in.Cwd,
		}, nil

	case "EnterPlanMode", "ExitPlanMode":
		// Mode toggle — no host-visible side effect. Browse-shape.
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: in.ToolName,
			Path:   in.Cwd,
		}, nil

	case "EnterWorktree":
		return gov.Action{
			Type:   gov.ActGitWorktreeAdd,
			Target: stringField(in.ToolInput, "branch"),
			Path:   in.Cwd,
		}, nil

	case "ExitWorktree":
		return gov.Action{
			Type:   gov.ActGitWorktreeRemove,
			Target: stringField(in.ToolInput, "path"),
			Path:   in.Cwd,
		}, nil

	case "PushNotification", "RemoteTrigger":
		// Outbound network — http.request shape. Operator can
		// allowlist by target host through the existing net rules.
		target := stringField(in.ToolInput, "url")
		if target == "" {
			target = stringField(in.ToolInput, "endpoint")
		}
		if target == "" {
			target = in.ToolName
		}
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: target,
			Path:   in.Cwd,
		}, nil

	case "CronCreate":
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: "cron",
			Path:   in.Cwd,
		}, nil

	case "CronDelete":
		return gov.Action{
			Type:   gov.ActFileDelete,
			Target: "cron",
			Path:   in.Cwd,
		}, nil

	case "CronList":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: "cron",
			Path:   in.Cwd,
		}, nil

	case "ScheduleWakeup":
		// Schedules a future agent-loop iteration — write-shape state
		// mutation. Target = the wakeup id (delaySeconds + reason
		// could go in payload but Target is the policy hook).
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: "schedule-wakeup",
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

	// MCP tool calls — `mcp__<server>__<tool>`. Route to ActMCPCall
	// with target="server/tool" matching the Copilot SDK driver's
	// shape. Without this, every MCP call falls to ActUnknown and
	// gets blocked by default-deny-unknown.
	if server, tool, ok := parseMCPToolName(in.ToolName); ok {
		target := server
		if tool != "" {
			target = server + "/" + tool
		}
		return gov.Action{
			Type:   gov.ActMCPCall,
			Target: target,
			Path:   in.Cwd,
			Params: in.ToolInput,
		}, nil
	}

	// Cross-driver leak detection: a few lowercase generic/openclaw-style
	// tool names have shown up on the Claude hook in real chain rows
	// (`read`, `exec`). Re-normalize via the shared gov layer so policy
	// attribution stays correct (file.read vs rm-rf vs git push), and warn
	// so the upstream dispatcher mis-route remains visible to the operator.
	if a, ok := normalizeGenericLeak(in); ok {
		return a, nil
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

func normalizeGenericLeak(in HookInput) (gov.Action, bool) {
	switch in.ToolName {
	case "read", "exec":
	default:
		return gov.Action{}, false
	}

	a, err := gov.Normalize(in.ToolName, in.ToolInput)
	if err != nil || a.Type == gov.ActUnknown {
		return gov.Action{}, false
	}
	a.Path = in.Cwd
	if warnSink != nil {
		fmt.Fprintf(warnSink,
			"{\"warning\":\"cross_driver_tool_name\",\"driver\":\"claude-code\",\"tool\":%q,\"hint\":\"upstream dispatcher routed a generic/openclaw-style tool name to the claude-code hook; re-normalized via gov.Normalize so policy attribution is correct, but the wiring should be fixed\"}\n",
			in.ToolName)
	}
	return a, true
}

// stringField extracts a string field from a tool_input map. Three
// paths:
//
//	missing key             → "", silent (caller decides if it's required)
//	present + string        → the value, silent
//	present + non-string    → "", warning to warnSink (operator telemetry
//	                          for malformed payloads — silent empty
//	                          would let policy default-deny mask the bug)
func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if warnSink != nil {
		fmt.Fprintf(warnSink, `{"warning":"tool_input_wrong_type","key":%q,"actual":%q}`+"\n", key, fmt.Sprintf("%T", v))
	}
	return ""
}
