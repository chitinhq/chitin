// Package hermes normalizes Hermes Agent PreToolUse hook payloads to
// canonical gov.Action values. Sibling of internal/driver/claudecode,
// internal/driver/codex, and internal/driver/gemini.
//
// Wire shape: byte-compatible with Claude Code's PreToolUse stdin
// (verified 2026-05-05 against ~/.hermes/hermes-agent/agent/shell_hooks.py
// docstring). Hermes accepts both Claude-Code-style and Hermes-canonical
// response shapes back from the hook (`{decision:"block",reason}` OR
// `{action:"block",message}`). chitin emits the Claude-Code shape, which
// hermes parses correctly.
//
// Tool-name set (from ~/.hermes/hermes-agent/agent/display.py
// primary_args map; closed enum unless noted):
//
//	terminal           → shell.exec (re-classified via gov.Normalize for
//	                     rm/git push/etc — same pipeline as the codex
//	                     and claudecode "Bash" path).
//	read_file          → file.read, target = path.
//	write_file         → file.write, target = path.
//	patch              → file.write, target = path (patch-style write).
//	search_files       → file.read, target = pattern (grep-like).
//	execute_code       → shell.exec via gov.Normalize on the code string
//	                     under "command" — best-effort because hermes's
//	                     execute_code is a sandbox runner, not a raw
//	                     shell, but the deny-rules (rm-rf, etc.) still
//	                     apply to anything it would attempt.
//	web_search         → http.request, target = query.
//	web_extract        → http.request, target = first url (urls is array).
//	browser_navigate   → http.request, target = url.
//	browser_click,
//	browser_type,
//	browser_scroll     → http.request, target = best-effort field per tool.
//	delegate_task      → delegate.task, target = goal.
//	mixture_of_agents  → delegate.task, target = user_prompt.
//	skill_view         → file.read, target = name (skill .md path).
//	skills_list        → file.read, target = category.
//	skill_manage       → file.write, target = name (skill creation/edit).
//	mcp__<server>__<tool> → mcp.call (hermes uses the same MCP convention
//	                     as Claude Code; reuse the parsing).
//
// Tools not yet mapped (fall to ActUnknown, default-deny under enforce):
//
//	image_generate, text_to_speech, vision_analyze — modality-side-effect
//	cronjob — scheduling action; no clear gov-action peer
//	clarify — chat-only, no fs/network impact
//	process — generic process action; ambiguous
//
// These produce ActUnknown which fail-closes under default-deny — safer
// than guessing wrong. Add proper types as the surface stabilizes.
package hermes

import (
	"fmt"
	"io"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// HookInput mirrors hermes's pre_tool_call stdin shape per the
// shell_hooks.py docstring. Field naming matches Claude Code's wire
// format so the upstream router-hook shim and evalHookStdin pipeline
// already speak it; only per-tool normalization differs.
type HookInput struct {
	SessionID      string         `json:"session_id"`
	TranscriptPath string         `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
}

var warnSink io.Writer

// SetWarnSink directs Normalize's non-fatal warnings to w. Pass nil
// to silence. Same shape as the codex/gemini drivers.
func SetWarnSink(w io.Writer) { warnSink = w }

// mcpToolPrefix mirrors the convention shared with claudecode + codex.
const mcpToolPrefix = "mcp__"

// Normalize maps a hermes pre_tool_call payload to a gov.Action. All
// tool names from hermes-agent/agent/display.py:primary_args have a
// case here; everything else falls to ActUnknown.
func Normalize(in HookInput) (gov.Action, error) {
	switch in.ToolName {
	case "terminal":
		// Same path as the claudecode + codex Bash branches —
		// gov.Normalize("terminal", ...) re-classifies rm-rf, git push,
		// gh pr create, etc. into specific action types.
		cmd := stringField(in.ToolInput, "command")
		a, err := gov.Normalize("terminal", map[string]any{"command": cmd})
		if err != nil {
			return gov.Action{}, fmt.Errorf("normalize terminal: %w", err)
		}
		a.Path = in.Cwd
		return a, nil

	case "execute_code":
		// hermes's sandboxed code runner. Treat the code string as a
		// shell command for gov.Normalize purposes — the deny rules
		// (rm-rf, etc.) still apply to anything it would attempt.
		code := stringField(in.ToolInput, "code")
		a, err := gov.Normalize("terminal", map[string]any{"command": code})
		if err != nil {
			return gov.Action{}, fmt.Errorf("normalize execute_code: %w", err)
		}
		a.Path = in.Cwd
		return a, nil

	case "read_file":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "path"),
			Path:   in.Cwd,
		}, nil

	case "write_file", "patch":
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: stringField(in.ToolInput, "path"),
			Path:   in.Cwd,
		}, nil

	case "search_files":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "pattern"),
			Path:   in.Cwd,
		}, nil

	case "web_search":
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: stringField(in.ToolInput, "query"),
			Path:   in.Cwd,
		}, nil

	case "web_extract":
		// urls is an array — record the first one for legibility; the
		// gate doesn't care about the rest at this layer.
		urls := firstStringInArray(in.ToolInput, "urls")
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: urls,
			Path:   in.Cwd,
		}, nil

	case "browser_navigate":
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: stringField(in.ToolInput, "url"),
			Path:   in.Cwd,
		}, nil

	case "browser_click", "browser_type", "browser_scroll", "browser_snapshot":
		// Browser interaction. Per-tool target field varies; fall back
		// to the most-specific available primary arg.
		var target string
		for _, k := range []string{"ref", "text", "direction", "selector"} {
			if v := stringField(in.ToolInput, k); v != "" {
				target = v
				break
			}
		}
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: target,
			Path:   in.Cwd,
		}, nil

	case "delegate_task":
		return gov.Action{
			Type:   gov.ActDelegateTask,
			Target: stringField(in.ToolInput, "goal"),
			Path:   in.Cwd,
		}, nil

	case "mixture_of_agents":
		return gov.Action{
			Type:   gov.ActDelegateTask,
			Target: stringField(in.ToolInput, "user_prompt"),
			Path:   in.Cwd,
		}, nil

	case "skill_view", "skills_list":
		// Read-only skill discovery; chitin policy treats these as file
		// reads on the skills tree.
		field := "name"
		if in.ToolName == "skills_list" {
			field = "category"
		}
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, field),
			Path:   in.Cwd,
		}, nil

	case "skill_manage":
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: stringField(in.ToolInput, "name"),
			Path:   in.Cwd,
		}, nil
	}

	// MCP tool calls — `mcp__<server>__<tool>`. Same shape as the
	// claudecode + codex drivers; route to ActMCPCall with target =
	// "server/tool".
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

	// Forward-compat: unmapped hermes tools (image_generate, cronjob,
	// clarify, etc.) fall through as ActUnknown. Policy default-deny
	// rejects them unless an operator opts a specific tool in.
	return gov.Action{
		Type:   gov.ActUnknown,
		Target: in.ToolName,
		Path:   in.Cwd,
		Params: in.ToolInput,
	}, nil
}

func stringField(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func firstStringInArray(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok {
		return ""
	}
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	s, ok := arr[0].(string)
	if !ok {
		return ""
	}
	return s
}

func parseMCPToolName(name string) (server, tool string, ok bool) {
	if !strings.HasPrefix(name, mcpToolPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(name, mcpToolPrefix)
	if rest == "" {
		return "", "", false
	}
	idx := strings.Index(rest, "__")
	if idx < 0 {
		// `mcp__server` with no tool suffix — accept, target = server.
		return rest, "", true
	}
	return rest[:idx], rest[idx+2:], true
}
