// Package claudecode normalizes Claude Code's PreToolUse hook payloads
// into gov.Actions and formats gov.Decisions back into the hook
// response shape Claude Code expects.
//
// The hook is registered globally (~/.claude/settings.json) or per-project
// (.claude/settings.json) by `chitin-kernel install claude-code-hook`,
// pointing at `chitin-kernel gate evaluate --hook-stdin --agent=claude-code`.
//
// Hook protocol (Claude Code → chitin):
//   - stdin: JSON with tool_name, tool_input, cwd, session_id, transcript_path
//   - stdout (allow): empty; exit 0
//   - stdout (deny):  {"decision":"block","reason":"..."}; exit 2
//
// See spec docs/superpowers/specs/2026-04-29-cost-governance-kernel-design.md
// §"Path A — Claude Code session" for the full data flow.
package claudecode

// HookInput is the shape of Claude Code's PreToolUse JSON payload.
// Fields beyond these are ignored — the hook protocol is forward-
// compatible by design (Claude Code adds fields without removing).
type HookInput struct {
	SessionID      string         `json:"session_id"`
	TranscriptPath string         `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
}
