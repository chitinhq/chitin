package claudecode

import (
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestNormalize_AllDocumentedToolsProduceNonEmptyType is the spec
// invariant: every documented Claude Code tool name maps to a known
// non-empty Action.Type. New Claude Code tools we haven't modeled fall
// through to ActUnknown — also non-empty, but flagged so the policy
// default-deny path catches them.
func TestNormalize_AllDocumentedToolsProduceNonEmptyType(t *testing.T) {
	tools := []string{
		// Original tool set
		"Bash", "Edit", "Write", "NotebookEdit",
		"Read", "WebFetch", "WebSearch", "Task",
		"Glob", "Grep", "LS", "TodoWrite",
		// Modern Claude Code tools (issue #69 — pre-fix these all
		// fell to ActUnknown → fail-closed → blocked under enforce
		// mode).
		"Agent", "Skill", "TaskStop",
		"TaskCreate", "TaskUpdate",
		"TaskGet", "TaskList", "TaskOutput", "ToolSearch", "AskUserQuestion",
		"Monitor",
		"EnterPlanMode", "ExitPlanMode",
		"EnterWorktree", "ExitWorktree",
		"PushNotification", "RemoteTrigger",
		"CronCreate", "CronDelete", "CronList",
		"ScheduleWakeup",
	}
	for _, name := range tools {
		in := HookInput{
			ToolName:  name,
			ToolInput: map[string]any{"command": "x", "file_path": "/x", "url": "https://x", "query": "x", "pattern": "*", "path": "/x", "description": "x", "notebook_path": "/x.ipynb", "subagent_type": "x", "skill": "x", "task_id": "t1", "branch": "main", "endpoint": "/x"},
			Cwd:       "/cwd",
		}
		a, err := Normalize(in)
		if err != nil {
			t.Errorf("%s: err=%v", name, err)
			continue
		}
		if a.Type == "" {
			t.Errorf("%s: empty Action.Type", name)
		}
		if a.Type == gov.ActUnknown {
			t.Errorf("%s: must not produce ActUnknown (documented tool)", name)
		}
	}
}

// TestNormalize_ModernClaudeCodeToolMappings pins the per-tool action
// type for the modern Claude Code surface. Closes issue #69 — the
// pre-fix denial under `mode: enforce` was the headline operator pain.
func TestNormalize_ModernClaudeCodeToolMappings(t *testing.T) {
	cases := []struct {
		name      string
		toolName  string
		toolInput map[string]any
		wantType  gov.ActionType
	}{
		{"Agent maps to delegate.task", "Agent", map[string]any{"description": "search the codebase", "subagent_type": "general-purpose"}, gov.ActDelegateTask},
		{"Skill maps to delegate.task", "Skill", map[string]any{"skill": "graphify"}, gov.ActDelegateTask},
		{"TaskStop maps to delegate.task", "TaskStop", map[string]any{"task_id": "t1"}, gov.ActDelegateTask},
		{"TaskCreate maps to file.write target=task-state", "TaskCreate", map[string]any{}, gov.ActFileWrite},
		{"TaskUpdate maps to file.write target=task-state", "TaskUpdate", map[string]any{}, gov.ActFileWrite},
		{"TaskGet maps to file.read", "TaskGet", map[string]any{}, gov.ActFileRead},
		{"TaskList maps to file.read", "TaskList", map[string]any{}, gov.ActFileRead},
		{"TaskOutput maps to file.read", "TaskOutput", map[string]any{}, gov.ActFileRead},
		{"ToolSearch maps to file.read", "ToolSearch", map[string]any{"query": "x"}, gov.ActFileRead},
		{"AskUserQuestion maps to file.read", "AskUserQuestion", map[string]any{}, gov.ActFileRead},
		{"Monitor maps to shell.exec", "Monitor", map[string]any{"command": "tail -f log"}, gov.ActShellExec},
		{"EnterPlanMode maps to file.read", "EnterPlanMode", map[string]any{}, gov.ActFileRead},
		{"ExitPlanMode maps to file.read", "ExitPlanMode", map[string]any{}, gov.ActFileRead},
		{"EnterWorktree maps to git.worktree.add", "EnterWorktree", map[string]any{"branch": "feat/x"}, gov.ActGitWorktreeAdd},
		{"ExitWorktree maps to git.worktree.remove", "ExitWorktree", map[string]any{"path": "/wt"}, gov.ActGitWorktreeRemove},
		{"PushNotification maps to http.request", "PushNotification", map[string]any{"endpoint": "https://x"}, gov.ActHTTPRequest},
		{"RemoteTrigger maps to http.request", "RemoteTrigger", map[string]any{"url": "https://x"}, gov.ActHTTPRequest},
		{"CronCreate maps to file.write target=cron", "CronCreate", map[string]any{}, gov.ActFileWrite},
		{"CronDelete maps to file.delete target=cron", "CronDelete", map[string]any{}, gov.ActFileDelete},
		{"CronList maps to file.read target=cron", "CronList", map[string]any{}, gov.ActFileRead},
		{"ScheduleWakeup maps to file.write", "ScheduleWakeup", map[string]any{}, gov.ActFileWrite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Normalize(HookInput{ToolName: tc.toolName, ToolInput: tc.toolInput, Cwd: "/cwd"})
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if a.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", a.Type, tc.wantType)
			}
		})
	}
}

// TestNormalize_AgentSkillFallbackTargetExtraction pins the target-
// extraction precedence for Agent/Skill: description > subagent_type >
// skill. The Target is the field policy rules + audit-log readers
// see, so order matters when multiple are present.
func TestNormalize_AgentSkillFallbackTargetExtraction(t *testing.T) {
	cases := []struct {
		name       string
		toolInput  map[string]any
		wantTarget string
	}{
		{"description wins over subagent_type", map[string]any{"description": "search", "subagent_type": "general-purpose"}, "search"},
		{"subagent_type wins over skill", map[string]any{"subagent_type": "general-purpose", "skill": "graphify"}, "general-purpose"},
		{"skill alone", map[string]any{"skill": "graphify"}, "graphify"},
		{"empty everything → empty target", map[string]any{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Normalize(HookInput{ToolName: "Agent", ToolInput: tc.toolInput, Cwd: "/cwd"})
			if err != nil {
				t.Fatalf("Normalize: %v", err)
			}
			if a.Target != tc.wantTarget {
				t.Errorf("Target = %q, want %q", a.Target, tc.wantTarget)
			}
		})
	}
}

func TestNormalize_BashReclassifiesShellCommands(t *testing.T) {
	cases := []struct {
		cmd  string
		want gov.ActionType
	}{
		{"git status", gov.ActGitStatus},
		{"git push origin main", gov.ActGitPush},
		{"gh pr create --title x", gov.ActGithubPRCreate},
		{"rm -rf go/", gov.ActFileRecursiveDelete}, // #58 closure: unified class, no longer shell.exec
		{"terraform destroy", gov.ActInfraDestroy},
		{"ls -la", gov.ActShellExec},
	}
	for _, tc := range cases {
		a, err := Normalize(HookInput{
			ToolName:  "Bash",
			ToolInput: map[string]any{"command": tc.cmd},
			Cwd:       "/tmp",
		})
		if err != nil {
			t.Errorf("Bash %q: err=%v", tc.cmd, err)
			continue
		}
		if a.Type != tc.want {
			t.Errorf("Bash %q: type=%s want %s", tc.cmd, a.Type, tc.want)
		}
		if a.Path != "/tmp" {
			t.Errorf("Bash %q: Path=%q want /tmp", tc.cmd, a.Path)
		}
	}
}

func TestNormalize_FileReadFromRead(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName:  "Read",
		ToolInput: map[string]any{"file_path": "/etc/hosts"},
		Cwd:       "/cwd",
	})
	if a.Type != gov.ActFileRead {
		t.Fatalf("Type=%s want file.read", a.Type)
	}
	if a.Target != "/etc/hosts" {
		t.Fatalf("Target=%q want /etc/hosts", a.Target)
	}
}

func TestNormalize_FileWriteFromEditWriteNotebook(t *testing.T) {
	for _, tool := range []string{"Edit", "Write"} {
		a, _ := Normalize(HookInput{
			ToolName:  tool,
			ToolInput: map[string]any{"file_path": "/tmp/x"},
		})
		if a.Type != gov.ActFileWrite {
			t.Errorf("%s: Type=%s want file.write", tool, a.Type)
		}
		if a.Target != "/tmp/x" {
			t.Errorf("%s: Target=%q want /tmp/x", tool, a.Target)
		}
	}
	// NotebookEdit uses notebook_path, not file_path.
	a, _ := Normalize(HookInput{
		ToolName:  "NotebookEdit",
		ToolInput: map[string]any{"notebook_path": "/tmp/x.ipynb"},
	})
	if a.Type != gov.ActFileWrite || a.Target != "/tmp/x.ipynb" {
		t.Fatalf("NotebookEdit: %+v", a)
	}
}

func TestNormalize_HTTPFromWebFetchAndSearch(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName:  "WebFetch",
		ToolInput: map[string]any{"url": "https://example.com"},
	})
	if a.Type != gov.ActHTTPRequest || a.Target != "https://example.com" {
		t.Fatalf("WebFetch: %+v", a)
	}
	b, _ := Normalize(HookInput{
		ToolName:  "WebSearch",
		ToolInput: map[string]any{"query": "go sqlite wal"},
	})
	if b.Type != gov.ActHTTPRequest || b.Target != "go sqlite wal" {
		t.Fatalf("WebSearch: %+v", b)
	}
}

func TestNormalize_DelegateFromTask(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName: "Task",
		ToolInput: map[string]any{
			"description":   "review PR #64",
			"subagent_type": "general-purpose",
		},
	})
	if a.Type != gov.ActDelegateTask {
		t.Fatalf("Type=%s want delegate.task", a.Type)
	}
	if a.Target != "review PR #64" {
		t.Fatalf("Target=%q want description", a.Target)
	}
}

func TestNormalize_DelegateTaskFallsBackToSubagentType(t *testing.T) {
	a, _ := Normalize(HookInput{
		ToolName:  "Task",
		ToolInput: map[string]any{"subagent_type": "Explore"},
	})
	if a.Target != "Explore" {
		t.Fatalf("Target=%q want Explore", a.Target)
	}
}

func TestNormalize_BrowseToolsAreFileRead(t *testing.T) {
	cases := []struct {
		name      string
		input     map[string]any
		wantTarget string
	}{
		{"Glob", map[string]any{"pattern": "**/*.go"}, "**/*.go"},
		{"Grep", map[string]any{"pattern": "TODO"}, "TODO"},
		{"LS", map[string]any{"path": "/etc"}, "/etc"},
	}
	for _, tc := range cases {
		a, _ := Normalize(HookInput{ToolName: tc.name, ToolInput: tc.input})
		if a.Type != gov.ActFileRead {
			t.Errorf("%s: type=%s want file.read", tc.name, a.Type)
		}
		if a.Target != tc.wantTarget {
			t.Errorf("%s: Target=%q want %q", tc.name, a.Target, tc.wantTarget)
		}
	}
}

func TestNormalize_TodoWriteIsFileWriteWithTodoTarget(t *testing.T) {
	a, _ := Normalize(HookInput{ToolName: "TodoWrite", ToolInput: map[string]any{"todos": []any{}}})
	if a.Type != gov.ActFileWrite {
		t.Fatalf("Type=%s want file.write", a.Type)
	}
	if a.Target != "todo" {
		t.Fatalf("Target=%q want todo (existing v2 normalize convention)", a.Target)
	}
}

func TestNormalize_UnknownToolFailsClosed(t *testing.T) {
	a, _ := Normalize(HookInput{ToolName: "FutureUnreleasedTool", ToolInput: map[string]any{"x": 1}})
	if a.Type != gov.ActUnknown {
		t.Fatalf("Type=%s want ActUnknown", a.Type)
	}
	// Params preserved so audit log captures what we couldn't classify.
	if a.Params == nil {
		t.Fatalf("Params should preserve raw input for audit")
	}
}

func TestNormalize_MissingFieldYieldsEmptyTarget(t *testing.T) {
	// No file_path → empty Target. Don't crash, don't substitute.
	a, _ := Normalize(HookInput{ToolName: "Read", ToolInput: map[string]any{}})
	if a.Type != gov.ActFileRead {
		t.Fatalf("Type=%s want file.read", a.Type)
	}
	if a.Target != "" {
		t.Fatalf("Target=%q want empty", a.Target)
	}
}
