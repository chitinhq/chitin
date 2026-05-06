// Package gemini normalizes Gemini CLI BeforeTool hook payloads
// to canonical gov.Action values. Sibling of internal/driver/
// claudecode; same wire shape as Claude Code's PreToolUse hook
// (the hook stdin protocol is byte-identical), but with
// gemini-specific tool names that must be re-mapped.
//
// Wire stability: gemini-cli emits BeforeTool with the same
// {session_id, cwd, hook_event_name, tool_name, tool_input}
// shape that Claude Code's PreToolUse uses. The router-hook
// shim already speaks that protocol — only the per-tool
// normalization differs.
//
// Source for the closed enum: gemini-cli's bundled tool registry
// at @google/gemini-cli/bundle/chunk-*.js. Last verified against
// 0.40.1 (2026-05-04). Unknown tools fall through to ActUnknown,
// which the policy default-deny-unknown rule will catch in
// enforce mode — same fail-closed posture as the claudecode driver.
package gemini

import (
	"fmt"
	"io"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// HookInput mirrors the wire-format gemini-cli sends on the
// BeforeTool hook's stdin. Identical to claudecode.HookInput
// modulo the field set; we keep the struct local to this driver
// so the two normalizers can evolve independently as vendors
// diverge.
type HookInput struct {
	SessionID      string         `json:"session_id"`
	TranscriptPath string         `json:"transcript_path"`
	Cwd            string         `json:"cwd"`
	HookEventName  string         `json:"hook_event_name"`
	Timestamp      string         `json:"timestamp"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
}

// warnSink mirrors the claudecode pattern — wrong-type fields
// emit a structured warning via this io.Writer. nil → silent.
var warnSink io.Writer

// SetWarnSink directs Normalize's non-fatal warnings to w. Pass
// nil to silence.
func SetWarnSink(w io.Writer) { warnSink = w }

// Normalize maps a Gemini CLI BeforeTool payload to a gov.Action.
//
// Tool-name mapping (as of gemini-cli 0.40.1):
//
//	run_shell_command                    → shell.exec (re-classified
//	                                       via gov.Normalize for rm,
//	                                       git push, etc.)
//	read_file, read_many_files,
//	  list_directory, glob,
//	  search_file_content,
//	  list_background_processes,
//	  read_background_output             → file.read
//	edit, replace, write_file            → file.write
//	web_fetch, google_web_search         → http.request
//	save_memory                          → file.write target="memory"
//	update_topic                         → file.write target="topic"
//	                                       (gemini's internal plan/
//	                                       summary; treat as write-
//	                                       shaped so policy rules
//	                                       stay simple)
//	<unknown>                            → ActUnknown (fail-closed)
func Normalize(in HookInput) (gov.Action, error) {
	switch in.ToolName {
	case "run_shell_command":
		cmd := stringField(in.ToolInput, "command")
		a, err := gov.Normalize("terminal", map[string]any{"command": cmd})
		if err != nil {
			return gov.Action{}, fmt.Errorf("normalize run_shell_command: %w", err)
		}
		a.Path = in.Cwd
		return a, nil

	case "read_file":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "file_path"),
			Path:   in.Cwd,
		}, nil

	case "read_many_files":
		// gemini accepts a list of paths/globs under "paths"; pick
		// the first as a representative target — the action_type
		// is what policy matches against, not the full input.
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: firstStringInList(in.ToolInput, "paths"),
			Path:   in.Cwd,
		}, nil

	case "list_directory":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "path"),
			Path:   in.Cwd,
		}, nil

	case "glob":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "pattern"),
			Path:   in.Cwd,
		}, nil

	case "search_file_content":
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: stringField(in.ToolInput, "pattern"),
			Path:   in.Cwd,
		}, nil

	case "list_background_processes", "read_background_output":
		// Browse-shaped — no host side effect.
		return gov.Action{
			Type:   gov.ActFileRead,
			Target: in.ToolName,
			Path:   in.Cwd,
		}, nil

	case "edit", "replace":
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: stringField(in.ToolInput, "file_path"),
			Path:   in.Cwd,
		}, nil

	case "write_file":
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: stringField(in.ToolInput, "file_path"),
			Path:   in.Cwd,
		}, nil

	case "web_fetch":
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: stringField(in.ToolInput, "url"),
			Path:   in.Cwd,
		}, nil

	case "google_web_search":
		return gov.Action{
			Type:   gov.ActHTTPRequest,
			Target: "google_web_search:" + stringField(in.ToolInput, "query"),
			Path:   in.Cwd,
		}, nil

	case "save_memory":
		// Internal memory artifact — write-shaped but target=memory
		// so a single "allow if target=memory" rule covers it.
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: "memory",
			Path:   in.Cwd,
		}, nil

	case "update_topic":
		// Gemini's per-turn plan/summary update. No host side effect
		// but treat as write-shaped at target="topic" to match the
		// claudecode TodoWrite convention (Target=todo) so policy
		// rules need only one entry per artifact kind.
		return gov.Action{
			Type:   gov.ActFileWrite,
			Target: "topic",
			Path:   in.Cwd,
		}, nil
	}

	// Cross-driver leak detection: see hermes/normalize.go for the
	// rationale (2026-05-06 lockdown incident). When a Claude-Code
	// tool name leaks into the gemini hook, re-normalize via
	// claudecode so policy attribution is correct (no-rm,
	// protected-system-path-write, etc.) and warn so the upstream
	// wiring bug is findable in operator stderr.
	if a, ok := normalizeClaudeLeak(in); ok {
		return a, nil
	}

	// Forward-compat: unknown tools fail-closed. Operator gets a
	// clear "default-deny-unknown" verdict and can add a rule if
	// the tool turns out to be benign.
	return gov.Action{
		Type:   gov.ActUnknown,
		Target: in.ToolName,
		Path:   in.Cwd,
		Params: in.ToolInput,
	}, nil
}

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
			"{\"warning\":\"cross_driver_tool_name\",\"driver\":\"gemini\",\"tool\":%q,\"hint\":\"upstream dispatcher routed a Claude-Code tool name to the gemini hook; re-normalized via claudecode.Normalize so policy attribution is correct, but the wiring should be fixed\"}\n",
			in.ToolName)
	}
	return a, true
}

// stringField extracts a string field. Same shape as
// claudecode.stringField — wrong-type values warn (operator
// telemetry), missing keys silent (caller decides if required).
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

// firstStringInList returns the first string entry in
// tool_input[key]. Used for read_many_files where the target
// shape is a list of paths.
func firstStringInList(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	if s, ok := list[0].(string); ok {
		return s
	}
	return ""
}
