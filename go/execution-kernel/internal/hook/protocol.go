// Package hook reads Claude Code PreToolUse payloads from stdin (>=2.x)
// or environment variables (pre-2.x). Phase 1 is monitor-only: the
// caller emits an event and exits 0; no decision is returned to Claude.
package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Input is the parsed hook payload.
type Input struct {
	Event     string         // PreToolUse, PostToolUse, Stop, Notification
	Tool      string         // tool name (e.g., "Bash", "Write", "Read")
	Input     map[string]any // tool input
	SessionID string         // session identifier
}

// stdinPayload is the JSON shape sent on stdin by Claude Code >= 2.x.
type stdinPayload struct {
	SessionID     string         `json:"session_id"`
	HookEventName string         `json:"hook_event_name"`
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
	CWD           string         `json:"cwd"`
	Model         string         `json:"model"`
}

// ReadClaudeInput reads from os.Stdin (if piped) or the process environment.
func ReadClaudeInput() (*Input, error) {
	if stdinHasData() {
		return parseClaudeInput(os.Stdin, os.Getenv)
	}
	return parseClaudeInput(emptyReader{}, os.Getenv)
}

// parseClaudeInput is the testable core: accepts an explicit reader and
// env lookup function. Reads stdin if non-empty; otherwise env.
func parseClaudeInput(r io.Reader, getenv func(string) string) (*Input, error) {
	data, _ := io.ReadAll(r)
	if len(data) > 0 {
		var s stdinPayload
		if err := json.Unmarshal(data, &s); err == nil && s.HookEventName != "" {
			return &Input{
				Event:     s.HookEventName,
				Tool:      s.ToolName,
				Input:     s.ToolInput,
				SessionID: s.SessionID,
			}, nil
		}
	}

	// Env fallback (Claude < 2.x protocol).
	evt := getenv("CLAUDE_HOOK_EVENT_NAME")
	if evt == "" {
		return nil, fmt.Errorf("no hook input: neither stdin JSON nor CLAUDE_HOOK_EVENT_NAME env")
	}

	var toolInput map[string]any
	if raw := getenv("CLAUDE_TOOL_INPUT"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &toolInput); err != nil {
			toolInput = map[string]any{"raw": raw}
		}
	}

	return &Input{
		Event:     evt,
		Tool:      getenv("CLAUDE_TOOL_NAME"),
		Input:     toolInput,
		SessionID: getenv("CLAUDE_SESSION_ID"),
	}, nil
}

// stdinHasData reports whether stdin is a pipe/file (data waiting)
// rather than a terminal.
func stdinHasData() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// emptyReader returns EOF immediately.
type emptyReader struct{}

func (emptyReader) Read(p []byte) (int, error) { return 0, io.EOF }
