package drivers

import (
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/claudecode"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/gemini"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/driver/hermes"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestCrossDriverNoUnknownTools verifies that every documented tool name
// from each driver (claudecode, gemini, hermes) normalizes to a non-ActUnknown
// ActionType. This catches the same class of bug found in gov.Normalize
// where missing tool cases fall through to ActUnknown — a policy gap that
// causes default-deny in enforce mode.
//
// When a new tool is added to a driver's normalize.go, add it to the
// corresponding list below. If it intentionally returns ActUnknown, add
// a comment explaining why and skip it in the test.
func TestCrossDriverNoUnknownTools(t *testing.T) {
	t.Run("claudecode", func(t *testing.T) {
		// Every tool name from claudecode.Normalize's switch statement.
		// Intentionally omitted: unknown tools that fall through to
		// claudecode's ActUnknown default.
		tools := []struct {
			name  string
			input map[string]any
		}{
			{"Bash", map[string]any{"command": "ls"}},
			{"Read", map[string]any{"file_path": "/tmp/x"}},
			{"Write", map[string]any{"file_path": "/tmp/x", "content": "hi"}},
			{"Edit", map[string]any{"file_path": "/tmp/x", "old_string": "a", "new_string": "b"}},
			{"Glob", map[string]any{"pattern": "**/*.go"}},
			{"Grep", map[string]any{"pattern": "TODO"}},
			{"LS", map[string]any{"path": "/tmp"}},
			{"NotebookRead", map[string]any{"notebook_path": "/tmp/nb.ipynb"}},
			{"NotebookEdit", map[string]any{"notebook_path": "/tmp/nb.ipynb", "cell_number": 0}},
			{"WebFetch", map[string]any{"url": "https://example.com"}},
			{"WebSearch", map[string]any{"query": "chitin"}},
			{"TodoRead", map[string]any{}},
			{"TodoWrite", map[string]any{"todos": "[]"}},
			{"Task", map[string]any{}},
			{"Agent", map[string]any{}},
		}
		for _, tc := range tools {
			t.Run(tc.name, func(t *testing.T) {
				a, err := claudecode.Normalize(claudecode.HookInput{
					ToolName:  tc.name,
					ToolInput: tc.input,
				})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if a.Type == gov.ActUnknown {
					t.Errorf("tool %q: normalized to ActUnknown (missing case in driver?)", tc.name)
				}
			})
		}
	})

	t.Run("gemini", func(t *testing.T) {
		tools := []struct {
			name  string
			input map[string]any
		}{
			{"run_shell_command", map[string]any{"command": "ls"}},
			{"read_file", map[string]any{"file_path": "/tmp/x"}},
			{"read_many_files", map[string]any{"paths": []any{"/tmp/a", "/tmp/b"}}},
			{"list_directory", map[string]any{"path": "/tmp"}},
			{"glob", map[string]any{"pattern": "**/*.go"}},
			{"search_file_content", map[string]any{"pattern": "TODO"}},
			{"list_background_processes", map[string]any{}},
			{"read_background_output", map[string]any{}},
			{"edit", map[string]any{"file_path": "/tmp/x"}},
			{"replace", map[string]any{"file_path": "/tmp/y"}},
			{"write_file", map[string]any{"file_path": "/tmp/z"}},
			{"web_fetch", map[string]any{"url": "https://example.com"}},
			{"google_web_search", map[string]any{"query": "chitin"}},
			{"save_memory", map[string]any{"content": "..."}},
			{"update_topic", map[string]any{"summary": "..."}},
		}
		for _, tc := range tools {
			t.Run(tc.name, func(t *testing.T) {
				a, err := gemini.Normalize(gemini.HookInput{
					ToolName:  tc.name,
					ToolInput: tc.input,
				})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if a.Type == gov.ActUnknown {
					t.Errorf("tool %q: normalized to ActUnknown (missing case in driver?)", tc.name)
				}
			})
		}
	})

	t.Run("hermes", func(t *testing.T) {
		tools := []struct {
			name  string
			input map[string]any
		}{
			{"terminal", map[string]any{"command": "ls"}},
			{"read_file", map[string]any{"path": "/tmp/x"}},
			{"write_file", map[string]any{"path": "/tmp/x"}},
			{"patch", map[string]any{"path": "/tmp/y"}},
			{"search_files", map[string]any{"pattern": "TODO"}},
			{"execute_code", map[string]any{"code": "print('hi')"}},
			{"web_search", map[string]any{"query": "chitin"}},
			{"web_extract", map[string]any{"urls": []any{"https://example.com"}}},
			{"browser_navigate", map[string]any{"url": "https://example.com"}},
			{"browser_click", map[string]any{"ref": "btn"}},
			{"browser_type", map[string]any{"text": "hello"}},
			{"browser_scroll", map[string]any{"direction": "down"}},
			{"browser_snapshot", map[string]any{}},
			{"delegate_task", map[string]any{"goal": "do thing"}},
			{"mixture_of_agents", map[string]any{"user_prompt": "compare"}},
			{"skill_view", map[string]any{"name": "my-skill"}},
			{"skills_list", map[string]any{"category": "devops"}},
			{"skill_manage", map[string]any{"name": "new-skill"}},
			{"memory", map[string]any{"content": "remember this"}},
			{"todo", map[string]any{"todos": "[]"}},
			{"session_search", map[string]any{"query": "hermes"}},
			{"process", map[string]any{"action": "list"}},
			{"kanban_show", map[string]any{"task_id": "01ABC"}},
			{"kanban_complete", map[string]any{"summary": "done"}},
			{"kanban_list", map[string]any{}},
			{"kanban_create", map[string]any{}},
			{"kanban_assign", map[string]any{}},
			{"kanban_reassign", map[string]any{}},
			{"kanban_link", map[string]any{}},
			{"kanban_unlink", map[string]any{}},
			{"kanban_claim", map[string]any{}},
			{"kanban_comment", map[string]any{}},
			{"kanban_block", map[string]any{}},
			{"kanban_unblock", map[string]any{}},
			{"kanban_archive", map[string]any{}},
			{"kanban_tail", map[string]any{}},
			{"kanban_dispatch", map[string]any{}},
			{"kanban_watch", map[string]any{}},
			{"kanban_stats", map[string]any{}},
			{"kanban_log", map[string]any{}},
			{"kanban_runs", map[string]any{}},
			{"kanban_heartbeat", map[string]any{}},
			{"kanban_edit", map[string]any{}},
			{"kanban_reclaim", map[string]any{}},
			{"kanban_init", map[string]any{}},
			{"kanban_boards", map[string]any{}},
			{"kanban_assignees", map[string]any{}},
			{"kanban_context", map[string]any{}},
			{"kanban_gc", map[string]any{}},
		}
		for _, tc := range tools {
			t.Run(tc.name, func(t *testing.T) {
				a, err := hermes.Normalize(hermes.HookInput{
					ToolName:  tc.name,
					ToolInput: tc.input,
				})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if a.Type == gov.ActUnknown {
					t.Errorf("tool %q: normalized to ActUnknown (missing case in driver?)", tc.name)
				}
			})
		}
	})
}

// TestCrossDriverMCPToolParsesCorrectly verifies that MCP tool names
// (mcp__server__tool format) are parsed correctly across drivers.
func TestCrossDriverMCPToolParsesCorrectly(t *testing.T) {
	t.Run("hermes_mcp", func(t *testing.T) {
		a, err := hermes.Normalize(hermes.HookInput{
			ToolName:  "mcp__github__list_issues",
			ToolInput: map[string]any{"owner": "chitinhq"},
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
	})

	t.Run("claudecode_mcp", func(t *testing.T) {
		a, err := claudecode.Normalize(claudecode.HookInput{
			ToolName:  "mcp__filesystem__read_file",
			ToolInput: map[string]any{"path": "/tmp/x"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActMCPCall {
			t.Errorf("Type: got %q, want %q", a.Type, gov.ActMCPCall)
		}
	})
}

// TestCrossDriverDestructiveReclassification verifies that destructive
// commands are reclassified consistently across all drivers, regardless
// of which driver's tool name is used.
func TestCrossDriverDestructiveReclassification(t *testing.T) {
	rmrf := "rm -rf /tmp/scratch"

	t.Run("claudecode_bash_rmrf", func(t *testing.T) {
		a, err := claudecode.Normalize(claudecode.HookInput{
			ToolName:  "Bash",
			ToolInput: map[string]any{"command": rmrf},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActFileRecursiveDelete {
			t.Errorf("claudecode Bash rm -rf: got %q, want %q", a.Type, gov.ActFileRecursiveDelete)
		}
	})

	t.Run("gemini_runshellcommand_rmrf", func(t *testing.T) {
		a, err := gemini.Normalize(gemini.HookInput{
			ToolName:  "run_shell_command",
			ToolInput: map[string]any{"command": rmrf},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActFileRecursiveDelete {
			t.Errorf("gemini run_shell_command rm -rf: got %q, want %q", a.Type, gov.ActFileRecursiveDelete)
		}
	})

	t.Run("hermes_terminal_rmrf", func(t *testing.T) {
		a, err := hermes.Normalize(hermes.HookInput{
			ToolName:  "terminal",
			ToolInput: map[string]any{"command": rmrf},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActFileRecursiveDelete {
			t.Errorf("hermes terminal rm -rf: got %q, want %q", a.Type, gov.ActFileRecursiveDelete)
		}
	})

	t.Run("hermes_executecode_rmrf", func(t *testing.T) {
		a, err := hermes.Normalize(hermes.HookInput{
			ToolName:  "execute_code",
			ToolInput: map[string]any{"code": rmrf},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a.Type != gov.ActFileRecursiveDelete {
			t.Errorf("hermes execute_code rm -rf: got %q, want %q", a.Type, gov.ActFileRecursiveDelete)
		}
	})
}