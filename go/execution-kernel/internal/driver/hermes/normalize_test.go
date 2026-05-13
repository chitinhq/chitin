package hermes

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestNormalize_terminalRoutesThroughGovNormalize(t *testing.T) {
	// terminal → gov.Normalize("terminal", ...) — same path as
	// claudecode "Bash" and codex "Bash". The gov layer handles
	// rm-rf re-tagging, so a destructive command lands as
	// ActFileRecursiveDelete, not bare ActShellExec.
	in := HookInput{
		Cwd:      "/tmp/wt",
		ToolName: "terminal",
		ToolInput: map[string]any{
			"command": "rm -rf /tmp/scratch",
		},
	}
	a, err := Normalize(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Type != gov.ActFileRecursiveDelete {
		t.Errorf("Type: got %q, want %q (gov.Normalize re-tag of rm -rf)", a.Type, gov.ActFileRecursiveDelete)
	}
	if a.Path != "/tmp/wt" {
		t.Errorf("Path: got %q, want %q", a.Path, "/tmp/wt")
	}
}

func TestNormalize_terminalAllowedShell(t *testing.T) {
	in := HookInput{
		Cwd:      "/repo",
		ToolName: "terminal",
		ToolInput: map[string]any{
			"command": "ls -la",
		},
	}
	a, err := Normalize(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Type != gov.ActShellExec {
		t.Errorf("Type: got %q, want %q", a.Type, gov.ActShellExec)
	}
}

func TestNormalize_readWriteFileMappings(t *testing.T) {
	cases := []struct {
		tool       string
		paramKey   string
		paramValue string
		wantType   gov.ActionType
		wantTarget string
	}{
		{"read_file", "path", "/etc/hostname", gov.ActFileRead, "/etc/hostname"},
		{"write_file", "path", "/repo/foo.go", gov.ActFileWrite, "/repo/foo.go"},
		{"patch", "path", "/repo/bar.go", gov.ActFileWrite, "/repo/bar.go"},
		{"search_files", "pattern", "TODO", gov.ActFileRead, "TODO"},
		{"skill_view", "name", "my-skill", gov.ActFileRead, "my-skill"},
		{"skills_list", "category", "devops", gov.ActFileRead, "devops"},
		{"skill_manage", "name", "new-skill", gov.ActFileWrite, "new-skill"},
		{"memory", "content", "remember this", gov.ActFileWrite, "memory"},
		{"todo", "todos", "[]", gov.ActFileWrite, "todo"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			a, err := Normalize(HookInput{
				Cwd:       "/cwd",
				ToolName:  tc.tool,
				ToolInput: map[string]any{tc.paramKey: tc.paramValue},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Type != tc.wantType {
				t.Errorf("Type: got %q, want %q", a.Type, tc.wantType)
			}
			if a.Target != tc.wantTarget {
				t.Errorf("Target: got %q, want %q", a.Target, tc.wantTarget)
			}
		})
	}
}

func TestNormalize_sessionSearchIsRead(t *testing.T) {
	t.Run("uses_query_as_target", func(t *testing.T) {
		a, err := Normalize(HookInput{
			Cwd:       "/cwd",
			ToolName:  "session_search",
			ToolInput: map[string]any{"query": "hermes hooks"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActFileRead {
			t.Errorf("Type: got %q, want %q", a.Type, gov.ActFileRead)
		}
		if a.Target != "hermes hooks" {
			t.Errorf("Target: got %q, want %q", a.Target, "hermes hooks")
		}
	})

	t.Run("falls_back_to_tool_name_without_query", func(t *testing.T) {
		a, err := Normalize(HookInput{
			Cwd:       "/cwd",
			ToolName:  "session_search",
			ToolInput: map[string]any{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActFileRead {
			t.Errorf("Type: got %q, want %q", a.Type, gov.ActFileRead)
		}
		if a.Target != "session_search" {
			t.Errorf("Target: got %q, want %q", a.Target, "session_search")
		}
	})
}

func TestNormalize_webAndBrowserToolsAreHTTPRequest(t *testing.T) {
	cases := []struct {
		tool       string
		input      map[string]any
		wantTarget string
	}{
		{"web_search", map[string]any{"query": "chitin governance"}, "chitin governance"},
		{"web_extract", map[string]any{"urls": []any{"https://example.com/a", "https://example.com/b"}}, "https://example.com/a"},
		{"browser_navigate", map[string]any{"url": "https://github.com"}, "https://github.com"},
		{"browser_click", map[string]any{"ref": "button-submit"}, "button-submit"},
		{"browser_type", map[string]any{"text": "hello"}, "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			a, err := Normalize(HookInput{
				Cwd:       "/cwd",
				ToolName:  tc.tool,
				ToolInput: tc.input,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Type != gov.ActHTTPRequest {
				t.Errorf("%s Type: got %q, want %q", tc.tool, a.Type, gov.ActHTTPRequest)
			}
			if a.Target != tc.wantTarget {
				t.Errorf("%s Target: got %q, want %q", tc.tool, a.Target, tc.wantTarget)
			}
		})
	}
}

func TestNormalize_delegateAndMixtureAreDelegateTask(t *testing.T) {
	d, err := Normalize(HookInput{
		ToolName:  "delegate_task",
		ToolInput: map[string]any{"goal": "summarize logs"},
	})
	if err != nil || d.Type != gov.ActDelegateTask || d.Target != "summarize logs" {
		t.Errorf("delegate_task: got %+v err=%v", d, err)
	}

	m, err := Normalize(HookInput{
		ToolName:  "mixture_of_agents",
		ToolInput: map[string]any{"user_prompt": "compare A and B"},
	})
	if err != nil || m.Type != gov.ActDelegateTask || m.Target != "compare A and B" {
		t.Errorf("mixture_of_agents: got %+v err=%v", m, err)
	}
}

func TestNormalize_executeCodeRoutesThroughGovNormalize(t *testing.T) {
	// execute_code's `code` field gets passed to gov.Normalize as
	// the command — so destructive code still trips the rm-rf rule.
	a, err := Normalize(HookInput{
		Cwd:       "/sandbox",
		ToolName:  "execute_code",
		ToolInput: map[string]any{"code": "rm -rf /tmp/foo"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Type != gov.ActFileRecursiveDelete {
		t.Errorf("Type: got %q, want %q (gov.Normalize re-tag)", a.Type, gov.ActFileRecursiveDelete)
	}
}

func TestNormalize_mcpToolName(t *testing.T) {
	a, err := Normalize(HookInput{
		ToolName:  "mcp__github__list_issues",
		ToolInput: map[string]any{"owner": "x"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Type != gov.ActMCPCall {
		t.Errorf("Type: got %q, want %q", a.Type, gov.ActMCPCall)
	}
	if a.Target != "github/list_issues" {
		t.Errorf("Target: got %q, want %q", a.Target, "github/list_issues")
	}
}

func TestNormalize_claudeCodeLeak_reNormalizesAndWarns(t *testing.T) {
	// Regression: 2026-05-06 chain showed raw `Write` strings arriving
	// at the hermes hook (envelope 01KQRF7D66GYXZ829G3QGRKWQB), which
	// fell to ActUnknown → default-deny-unknown → counter accumulation
	// → lockdown. Cross-driver leak detection catches the upstream
	// wiring bug: re-normalize via claudecode so the deny gets correct
	// rule attribution, and emit a warn so the dispatcher mis-route
	// is findable.
	buf := &bytes.Buffer{}
	SetWarnSink(buf)
	t.Cleanup(func() { SetWarnSink(nil) })

	cases := []struct {
		tool     string
		input    map[string]any
		wantType gov.ActionType
	}{
		{"Write", map[string]any{"file_path": "/etc/hostname", "content": "x"}, gov.ActFileWrite},
		{"Edit", map[string]any{"file_path": "/tmp/x", "old_string": "a", "new_string": "b"}, gov.ActFileWrite},
		{"Read", map[string]any{"file_path": "/etc/passwd"}, gov.ActFileRead},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			buf.Reset()
			a, err := Normalize(HookInput{ToolName: tc.tool, ToolInput: tc.input})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Type != tc.wantType {
				t.Errorf("Type=%q want %q (claude leak should re-normalize, not fall to ActUnknown)", a.Type, tc.wantType)
			}
			if !strings.Contains(buf.String(), `"warning":"cross_driver_tool_name"`) {
				t.Errorf("expected cross_driver_tool_name warn, got: %q", buf.String())
			}
			if !strings.Contains(buf.String(), `"driver":"hermes"`) {
				t.Errorf("warn missing driver=hermes: %q", buf.String())
			}
		})
	}
}

func TestNormalize_unmappedTools_fallToUnknown(t *testing.T) {
	// image_generate, cronjob, clarify, etc. have no clean gov-action
	// peer; they fall through to ActUnknown which the policy
	// default-deny rejects unless an operator opts a specific tool in.
	// Note: `process` was previously in this list; it now maps to
	// ActHermesProcess (see TestNormalize_hermesInternalTools below).
	for _, tool := range []string{"image_generate", "text_to_speech", "vision_analyze", "cronjob", "clarify"} {
		t.Run(tool, func(t *testing.T) {
			a, err := Normalize(HookInput{
				ToolName:  tool,
				ToolInput: map[string]any{},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.Type != gov.ActUnknown {
				t.Errorf("Type: got %q, want %q", a.Type, gov.ActUnknown)
			}
			if a.Target != tool {
				t.Errorf("Target: got %q, want %q (action_target = tool name on unknown)", a.Target, tool)
			}
		})
	}
}

// TestNormalize_hermesInternalTools pins the kanban_* + process mapping
// added to plug the lockdown loop observed on 2026-05-07: hermes worker
// plumbing calls were hitting ActUnknown → default-deny-unknown and
// crossing the lockdown threshold in <10 calls. Each kanban verb maps
// to ActKanbanCall with target = the verb (prefix stripped); the hermes
// `process` tool maps to ActHermesProcess with target = the action
// sub-verb passed in tool_input.
func TestNormalize_hermesInternalTools(t *testing.T) {
	t.Run("kanban_show_maps_to_ActKanbanCall_with_verb_target", func(t *testing.T) {
		a, err := Normalize(HookInput{
			ToolName:  "kanban_show",
			ToolInput: map[string]any{"task_id": "01ABC"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActKanbanCall {
			t.Errorf("Type: got %q, want %q", a.Type, gov.ActKanbanCall)
		}
		if a.Target != "show" {
			t.Errorf("Target: got %q, want %q (verb prefix stripped)", a.Target, "show")
		}
	})
	t.Run("kanban_complete_maps_to_ActKanbanCall_with_verb_target", func(t *testing.T) {
		a, err := Normalize(HookInput{
			ToolName:  "kanban_complete",
			ToolInput: map[string]any{"summary": "done"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActKanbanCall {
			t.Errorf("Type: got %q, want %q", a.Type, gov.ActKanbanCall)
		}
		if a.Target != "complete" {
			t.Errorf("Target: got %q, want %q", a.Target, "complete")
		}
	})
	t.Run("process_maps_to_ActHermesProcess", func(t *testing.T) {
		a, err := Normalize(HookInput{
			ToolName:  "process",
			ToolInput: map[string]any{"action": "list"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActHermesProcess {
			t.Errorf("Type: got %q, want %q", a.Type, gov.ActHermesProcess)
		}
	})
}

func TestNormalize_ProcessStaysHermesProcessDespiteGenericExecMapping(t *testing.T) {
	a, err := Normalize(HookInput{
		ToolName:  "process",
		ToolInput: map[string]any{"action": "list", "command": "ls -la"},
		Cwd:       "/cwd",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Type != gov.ActHermesProcess {
		t.Fatalf("Type=%q want %q", a.Type, gov.ActHermesProcess)
	}
	if a.Target != "list" {
		t.Fatalf("Target=%q want list", a.Target)
	}
}
