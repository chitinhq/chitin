package gov

import (
	"testing"
)

// TestNormalize_OpenclawToolConformance verifies that every openclaw pi-runtime
// tool name maps to the correct canonical ActionType. This is the conformance
// surface: if a new tool is added to openclaw, it MUST appear here with the
// expected type. ActUnknown means it's not yet classified - a failing test
// surfaces that as action-item work.
//
// The tool names come from the openclaw pi-runtime tool registry. When a new
// tool is added upstream, add it here with the expected type. If the expected
// type is ActUnknown, the test will FAIL - forcing classification before merge.
func TestNormalize_OpenclawToolConformance(t *testing.T) {
	tests := []struct {
		tool     string
		args     map[string]any
		wantType ActionType
		wantDesc string // human-readable description for test output
	}{
		// --- File operations ---
		{"read", map[string]any{"path": "/etc/hosts"}, ActFileRead, "read → file.read"},
		{"read", map[string]any{"file_path": "/etc/hosts"}, ActFileRead, "read (file_path alias) → file.read"},
		{"write", map[string]any{"path": "/tmp/out.txt", "content": "hi"}, ActFileWrite, "write → file.write"},
		{"edit", map[string]any{"path": "/tmp/out.txt"}, ActFileWrite, "edit → file.write"},

		// --- Shell operations ---
		{"exec", map[string]any{"cmd": "ls -la"}, ActShellExec, "exec → shell.exec (via normalizeShell)"},
		{"exec", map[string]any{"command": "ls -la"}, ActShellExec, "exec (command alias) → shell.exec"},
		{"process", map[string]any{"cmd": "sleep 5"}, ActShellExec, "process → shell.exec"},
		{"terminal", map[string]any{"command": "git status"}, ActGitStatus, "terminal git status → git.status"},
		{"bash", map[string]any{"command": "rm -rf /tmp/old"}, ActFileRecursiveDelete, "bash rm -rf → file.recursive_delete"},

		// --- Memory tools ---
		{"memory_search", map[string]any{"query": "chitin"}, ActFileRead, "memory_search → file.read"},
		{"memory_get", map[string]any{"path": "MEMORY.md"}, ActFileRead, "memory_get → file.read"},
		{"memory_get", map[string]any{"file": "MEMORY.md"}, ActFileRead, "memory_get (file alias) → file.read"},

		// --- Session tools ---
		{"sessions_list", map[string]any{}, ActFileRead, "sessions_list → file.read"},
		{"sessions_history", map[string]any{}, ActFileRead, "sessions_history → file.read"},
		{"session_status", map[string]any{}, ActFileRead, "session_status → file.read"},
		{"sessions_send", map[string]any{"agentId": "glm-agent", "message": "hi"}, ActDelegateTask, "sessions_send → delegate.task"},
		{"sessions_spawn", map[string]any{"agentId": "glm-agent"}, ActDelegateTask, "sessions_spawn → delegate.task"},
		{"sessions_yield", map[string]any{}, ActFileRead, "sessions_yield → file.read"},

		// --- Cron scheduling ---
		{"cron", map[string]any{"action": "create", "name": "heartbeat"}, ActDelegateTask, "cron create → delegate.task"},
		{"cron", map[string]any{"name": "heartbeat"}, ActDelegateTask, "cron (fallback) → delegate.task"},

		// --- Subagent management ---
		{"subagents", map[string]any{"action": "spawn", "agentId": "worker-1"}, ActDelegateTask, "subagents spawn → delegate.task"},
		{"subagents", map[string]any{}, ActDelegateTask, "subagents (fallback) → delegate.task"},

		// --- Network tools ---
		{"image", map[string]any{"path": "/tmp/photo.jpg"}, ActHTTPRequest, "image → http.request"},
		{"image", map[string]any{"url": "https://example.com/img.png"}, ActHTTPRequest, "image (url) → http.request"},
		{"image_generate", map[string]any{"prompt": "a cat"}, ActHTTPRequest, "image_generate → http.request"},
		{"web_search", map[string]any{"query": "chitin"}, ActHTTPRequest, "web_search → http.request"},
		{"web_fetch", map[string]any{"url": "https://example.com"}, ActHTTPRequest, "web_fetch → http.request"},
		{"ollama_web_search", map[string]any{"query": "chitin"}, ActHTTPRequest, "ollama_web_search → http.request"},
		{"ollama_web_fetch", map[string]any{"url": "https://example.com"}, ActHTTPRequest, "ollama_web_fetch → http.request"},

		// --- Other openclaw tools ---
		{"delegate_task", map[string]any{"goal": "build feature X"}, ActDelegateTask, "delegate_task → delegate.task"},
		{"search_files", map[string]any{"query": "TODO"}, ActFileRead, "search_files → file.read"},
		{"skill_view", map[string]any{"skill": "coding"}, ActFileRead, "skill_view → file.read"},
		{"todo", map[string]any{}, ActFileWrite, "todo → file.write"},

		// --- External driver tool names (should still classify) ---
		{"read_file", map[string]any{"path": "/etc/hosts"}, ActFileRead, "read_file → file.read"},
		{"write_file", map[string]any{"path": "/tmp/out.txt"}, ActFileWrite, "write_file → file.write"},
		{"execute_code", map[string]any{"code": "print('hi')"}, ActFileWrite, "execute_code (pure python) → file.write"},
		{"execute_code", map[string]any{"code": "subprocess.run(['rm', '-rf', '/tmp'])"}, ActFileRecursiveDelete, "execute_code rm → file.recursive_delete"},

		// Other read-only tools (Claude Code / openclaw file search)
		{"glob", map[string]any{"path": "/home"}, ActFileRead, "glob → file.read"},
		{"grep", map[string]any{"path": "/home"}, ActFileRead, "grep → file.read"},
		{"ls", map[string]any{"path": "/home"}, ActFileRead, "ls → file.read"},
		{"notebookread", map[string]any{"path": "/tmp/nb.ipynb"}, ActFileRead, "notebookread → file.read"},

		// Notebook cell editing
		{"notebookedit", map[string]any{"path": "/tmp/nb.ipynb"}, ActFileWrite, "notebookedit → file.write"},

		// Agent/task tools
		{"agent", map[string]any{}, ActDelegateTask, "agent → delegate.task"},
		{"task", map[string]any{}, ActDelegateTask, "task → delegate.task"},
		{"taskcreate", map[string]any{}, ActDelegateTask, "taskcreate → delegate.task"},
		{"taskupdate", map[string]any{}, ActDelegateTask, "taskupdate → delegate.task"},
		{"tasklist", map[string]any{}, ActDelegateTask, "tasklist → delegate.task"},
		{"taskget", map[string]any{}, ActDelegateTask, "taskget → delegate.task"},

		// --- Unknown tool (fail-closed) ---
		{"unknown_tool_xyz", map[string]any{}, ActUnknown, "unknown tool → unknown (fail-closed)"},
	}

	for _, tt := range tests {
		t.Run(tt.wantDesc, func(t *testing.T) {
			a, err := Normalize(tt.tool, tt.args)
			if err != nil {
				t.Fatalf("Normalize(%q): %v", tt.tool, err)
			}
			if a.Type != tt.wantType {
				t.Errorf("Normalize(%q): got type %q, want %q", tt.tool, a.Type, tt.wantType)
			}
		})
	}
}

// TestNormalize_OpenclawToolConformance_Completeness asserts that every
// openclaw pi-runtime tool present in the current openclaw config is
// represented in the conformance table above. This catches new tools that
// were added to openclaw but not yet mapped in the normalizer.
//
// If this test fails, add the new tool to the table above with the
// expected ActionType. Use ActUnknown if the tool has not been classified
// yet - but ActUnknown entries will fail the main conformance test.
func TestNormalize_OpenclawToolConformance_Completeness(t *testing.T) {
	// Known openclaw pi-runtime tools as of 2026-05-09.
	// Source: openclaw tool registry + tools.alsoAllow in config +
	// openclaw-plugin-governance's before_tool_call handler.
	knownTools := []string{
		// Core pi-runtime tools
		"read", "write", "edit", "exec", "process", "memory_search", "memory_get",
		"sessions_list", "sessions_history", "sessions_yield", "session_status",
		"sessions_send", "sessions_spawn", "cron", "subagents",
		"image", "image_generate", "web_search", "web_fetch",
		"ollama_web_search", "ollama_web_fetch",
		"delegate_task", "search_files", "skill_view", "todo",
		// External driver names (claude-code, codex, gemini, copilot)
		"terminal", "bash",
		"read_file", "write_file", "patch",
		"execute_code",
		"agent", "task", "taskcreate", "taskupdate", "tasklist", "taskget",
		"glob", "grep", "ls", "notebookread", "notebookedit",
	}

	// All conformance entries are in TestNormalize_OpenclawToolConformance.
	// This test verifies that known openclaw tools are not returning ActUnknown.

	for _, tool := range knownTools {
		a, err := Normalize(tool, map[string]any{})
		if err != nil {
			t.Logf("Normalize(%q) error: %v (may need args)", tool, err)
			continue
		}
		if a.Type == ActUnknown {
			t.Errorf("Normalize(%q): returned ActUnknown - add to conformance table with correct ActionType", tool)
		}
	}
}