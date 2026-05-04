// Package codex normalizes Codex CLI PreToolUse hook payloads to
// canonical gov.Action values. Sibling of internal/driver/
// claudecode and internal/driver/gemini.
//
// Wire shape (verified 2026-05-04 against codex 0.128.0): byte-
// identical to Claude Code's PreToolUse stdin format. Codex even
// uses the same field names — `hook_event_name`, `tool_name`,
// `tool_input`, `cwd`, `session_id`, `tool_use_id`, `turn_id`. So
// the router-hook shim and evalHookStdin pipeline already speak
// it; only per-tool normalization differs because codex's tool
// name set is its own (Bash, apply_patch, MCP names).
//
// Tool-name set (closed enum):
//
//	Bash               → shell.exec (re-classified via gov.Normalize
//	                     for rm/git push/etc — same flow as the
//	                     claudecode driver since both use "Bash" as
//	                     the wire name).
//	apply_patch        → file.write target = first file path in the
//	                     patch (best-effort) or empty if not parseable.
//	read_file          → file.read.
//	mcp__<server>__<tool> → mcp.call (codex passes MCP tool calls
//	                     under the same naming convention as Claude
//	                     Code; reuse the same parsing).
//	<unknown>          → ActUnknown (fail-closed under default-deny).
package codex

import (
	"fmt"
	"io"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// HookInput mirrors codex's PreToolUse stdin. Codex includes a
// few extra fields (model, permission_mode, tool_use_id, turn_id)
// that the claudecode shape doesn't have, but only ToolName +
// ToolInput + Cwd are load-bearing for normalization. Kept as
// a separate struct so each driver can evolve independently.
type HookInput struct {
	SessionID      string         `json:"session_id"`
	TranscriptPath string         `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	Model          string         `json:"model"`
	PermissionMode string         `json:"permission_mode"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
	ToolUseID      string         `json:"tool_use_id"`
	TurnID         string         `json:"turn_id"`
}

var warnSink io.Writer

// SetWarnSink directs Normalize's non-fatal warnings to w. Pass
// nil to silence.
func SetWarnSink(w io.Writer) { warnSink = w }

// mcpToolPrefix mirrors the claudecode convention. Codex's MCP
// tool names follow the same `mcp__<server>__<tool>` pattern.
const mcpToolPrefix = "mcp__"

// Normalize maps a codex PreToolUse payload to a gov.Action.
func Normalize(in HookInput) (gov.Action, error) {
	switch in.ToolName {
	case "Bash":
		// Same path as claudecode — gov.Normalize handles the
		// rm/git push/gh pr create re-classification.
		cmd := stringField(in.ToolInput, "command")
		a, err := gov.Normalize("terminal", map[string]any{"command": cmd})
		if err != nil {
			return gov.Action{}, fmt.Errorf("normalize Bash: %w", err)
		}
		a.Path = in.Cwd
		return a, nil

	case "apply_patch":
		// codex's apply_patch tool_input includes a unified-diff-
		// shaped string under "input" (codex 0.128.0). Best-effort
		// extract the first file path; if we can't parse it, the
		// action still goes through as file.write with empty target
		// so policy can match on action_type alone.
		target := firstFilePathFromPatch(stringField(in.ToolInput, "input"))
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: target,
			Path:   in.Cwd,
		}, nil

	case "read_file":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "file_path"),
			Path:   in.Cwd,
		}, nil
	}

	// MCP tools — `mcp__<server>__<tool>`. Same parsing as the
	// claudecode driver; codex passes the same wire format.
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

	// Forward-compat: unknown tools fail-closed.
	return gov.Action{
		Type:   gov.ActUnknown,
		Target: in.ToolName,
		Path:   in.Cwd,
		Params: in.ToolInput,
	}, nil
}

// parseMCPToolName splits "mcp__server__tool" on the FIRST `__`
// after the prefix. Same shape as claudecode/normalize.go.
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

// firstFilePathFromPatch returns the first file path found in a
// codex apply_patch input string. Codex apply_patch uses a
// unified-diff-ish format starting with `*** Begin Patch` and
// per-file headers like `*** Update File: path/to/file` or
// `*** Add File: path/to/file`. We match those headers; if no
// header is found, return empty.
func firstFilePathFromPatch(s string) string {
	if s == "" {
		return ""
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{
			"*** Update File: ",
			"*** Add File: ",
			"*** Delete File: ",
		} {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimSpace(line[len(prefix):])
			}
		}
	}
	return ""
}

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
