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
//	execute_code       → gov.Normalize("execute_code", code) so Python
//	                     subprocess/shutil patterns are extracted before
//	                     shell classification. Best-effort because
//	                     hermes's execute_code is a sandbox runner, not a
//	                     raw shell.
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
//	memory             → file.write, target = memory (durable memory write).
//	todo               → file.write, target = todo (session task state).
//	session_search     → file.read, target = query (past session/memory search).
//	process            → hermes.process, target = action.
//	kanban_*           → kanban.call, target = verb without kanban_ prefix.
//	mcp__<server>__<tool> → mcp.call (hermes uses the same MCP convention
//	                     as Claude Code; reuse the parsing).
//
// Tools not yet mapped (fall to ActUnknown, default-deny under enforce):
//
//	image_generate, text_to_speech, vision_analyze — modality-side-effect
//	cronjob — scheduling action; no clear gov-action peer
//	clarify — chat-only, no fs/network impact
//
// These produce ActUnknown which fail-closes under default-deny — safer
// than guessing wrong. Add proper types as the surface stabilizes.
package hermes

import (
	"fmt"
	"io"
	"strings"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
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
		// Hermes's sandboxed code runner. Route through gov.Normalize's
		// execute_code branch so Python subprocess/shutil patterns are
		// extracted before shell classification.
		code := stringField(in.ToolInput, "code")
		a, err := gov.Normalize("execute_code", map[string]any{"code": code})
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

	case "memory":
		// Hermes durable memory mutates operator-owned agent memory.
		// Keep the target stable rather than action/target-specific so
		// policy can reason about the memory surface as one governed sink.
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: "memory",
			Path:   in.Cwd,
			Params: in.ToolInput,
		}, nil

	case "todo":
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: "todo",
			Path:   in.Cwd,
			Params: in.ToolInput,
		}, nil

	case "session_search":
		target := stringField(in.ToolInput, "query")
		if target == "" {
			target = "session_search"
		}
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: target,
			Path:   in.Cwd,
			Params: in.ToolInput,
		}, nil

	// Kanban runtime calls — the hermes worker reading/writing its own
	// card lifecycle (`kanban_show`, `kanban_complete`, `kanban_block`,
	// `kanban_comment`, `kanban_heartbeat`, `kanban_create`, etc.).
	// Plumbing-shaped: not the agent doing things in the world, just
	// the worker managing its own status. Routed to ActKanbanCall so
	// they're auditable + governable but distinguished from real
	// host-effect actions. Without this case, every long-running
	// hermes worker hits ActUnknown → default-deny-unknown → lockdown
	// in <10 plumbing calls (root cause of 2026-05-07 smoke stalls).
	// Target = the kanban verb (`show`, `complete`, etc.) so policy
	// rules can allow/deny per-verb if desired.
	case "kanban_show", "kanban_list", "kanban_create", "kanban_assign",
		"kanban_reassign", "kanban_link", "kanban_unlink", "kanban_claim",
		"kanban_comment", "kanban_complete", "kanban_block", "kanban_unblock",
		"kanban_archive", "kanban_tail", "kanban_dispatch", "kanban_watch",
		"kanban_stats", "kanban_log", "kanban_runs", "kanban_heartbeat",
		"kanban_assignees", "kanban_context", "kanban_gc", "kanban_edit",
		"kanban_reclaim", "kanban_init", "kanban_boards":
		// Strip the "kanban_" prefix for a clean target verb.
		verb := in.ToolName[len("kanban_"):]
		return gov.Action{
			Type:   gov.ActKanbanCall,
			Target: verb,
			Path:   in.Cwd,
			Params: in.ToolInput,
		}, nil

	// Hermes process tool — runtime helper for background process
	// management inside the agent's session. Same plumbing class as
	// kanban_*; explicit ActHermesProcess so it doesn't accumulate
	// as default-deny-unknown.
	case "process":
		return gov.Action{
			Type:   gov.ActHermesProcess,
			Target: stringField(in.ToolInput, "action"),
			Path:   in.Cwd,
			Params: in.ToolInput,
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

	// Cross-driver leak detection: if a Claude-Code tool name reaches
	// the hermes hook, the upstream dispatcher (swarm orchestrator)
	// mis-routed the payload. Returning ActUnknown silently
	// accumulates default-deny-unknown denials toward the lockdown
	// threshold — root cause of 2026-05-06 hermes lockdown after
	// raw `Write` calls leaked in via envelope 01KQRF7D66GYXZ829G3QGRKWQB.
	// Re-normalize via claudecode.Normalize so the underlying action
	// gets correct rule attribution (no-rm, protected-system-path-write,
	// etc.) instead of accumulating as default-deny-unknown, and warn
	// so the dispatcher misconfiguration surfaces in operator stderr.
	if a, ok := normalizeClaudeLeak(in); ok {
		return a, nil
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

// normalizeClaudeLeak attempts to interpret in as a Claude-Code-shaped
// payload that was mis-routed to the hermes hook. Returns (action,true)
// when claudecode.Normalize maps the tool name to a known action; false
// otherwise (caller falls through to ActUnknown). Emits a structured
// warn line so the upstream wiring bug is findable — without it, the
// only signal would be a chain row with rule_id=default-deny-unknown
// and tool name "Write", which already conflated cross-driver leaks
// with genuinely-unknown hermes tools.
func normalizeClaudeLeak(in HookInput) (gov.Action, bool) {
	cc := claudecode.HookInput{
		SessionID:      in.SessionID,
		TranscriptPath: in.TranscriptPath,
		Cwd:            in.Cwd,
		HookEventName:  in.HookEventName,
		ToolName:       in.ToolName,
		ToolInput:      in.ToolInput,
	}
	a, err := claudecode.Normalize(cc)
	if err != nil || a.Type == gov.ActUnknown {
		return gov.Action{}, false
	}
	if warnSink != nil {
		fmt.Fprintf(warnSink,
			"{\"warning\":\"cross_driver_tool_name\",\"driver\":\"hermes\",\"tool\":%q,\"hint\":\"upstream dispatcher routed a Claude-Code tool name to the hermes hook; re-normalized via claudecode.Normalize so policy attribution is correct, but the wiring should be fixed\"}\n",
			in.ToolName)
	}
	return a, true
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
