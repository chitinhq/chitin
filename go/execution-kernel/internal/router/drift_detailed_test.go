package router

import "testing"

// Complements drift_test.go with edge cases for extractIntentFromChain,
// targetPathFromInput, and DetectDrift.

func TestExtractIntentFromChain_TaskAssignment(t *testing.T) {
	events := []ChainEvent{
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash"}},
		{EventType: "task_assignment", Payload: map[string]interface{}{
			"entry_id":   "entry-42",
			"task_class": "bugfix",
			"file_paths": []interface{}{"src/fix.go", "src/fix_test.go"},
		}},
	}
	intent := extractIntentFromChain(events)
	if intent.EntryID != "entry-42" {
		t.Errorf("EntryID=%q want entry-42", intent.EntryID)
	}
	if intent.TaskClass != "bugfix" {
		t.Errorf("TaskClass=%q want bugfix", intent.TaskClass)
	}
	if len(intent.FilePaths) != 2 {
		t.Fatalf("FilePaths len=%d want 2", len(intent.FilePaths))
	}
	if intent.FilePaths[0] != "src/fix.go" {
		t.Errorf("FilePaths[0]=%q want src/fix.go", intent.FilePaths[0])
	}
}

func TestExtractIntentFromChain_IntentEventType(t *testing.T) {
	events := []ChainEvent{
		{EventType: "intent", Payload: map[string]interface{}{
			"entry_id":   "intent-1",
			"task_class": "refactor",
			"file_paths": []interface{}{"apps/cli/main.ts"},
		}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash"}},
	}
	intent := extractIntentFromChain(events)
	if intent.EntryID != "intent-1" {
		t.Errorf("should find intent event first; got EntryID=%q", intent.EntryID)
	}
}

func TestExtractIntentFromChain_NoIntent(t *testing.T) {
	events := []ChainEvent{
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Read"}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash"}},
	}
	intent := extractIntentFromChain(events)
	if intent.EntryID != "" {
		t.Errorf("expected empty EntryID when no intent event; got %q", intent.EntryID)
	}
}

func TestExtractIntentFromChain_NilFilePaths(t *testing.T) {
	events := []ChainEvent{
		{EventType: "task_assignment", Payload: map[string]interface{}{
			"entry_id":   "entry-5",
			"task_class": "docs",
			// file_paths missing
		}},
	}
	intent := extractIntentFromChain(events)
	if intent.EntryID != "entry-5" {
		t.Errorf("EntryID=%q want entry-5", intent.EntryID)
	}
	if len(intent.FilePaths) != 0 {
		t.Errorf("FilePaths should be empty when not provided; got %v", intent.FilePaths)
	}
}

func TestTargetPathFromInput_FilePath(t *testing.T) {
	input := HookInput{ToolName: "Edit", ToolInput: map[string]interface{}{"file_path": "/src/main.go"}}
	got := targetPathFromInput(input)
	if got != "/src/main.go" {
		t.Errorf("got %q, want /src/main.go", got)
	}
}

func TestTargetPathFromInput_NotebookPath(t *testing.T) {
	input := HookInput{ToolName: "NotebookEdit", ToolInput: map[string]interface{}{"notebook_path": "/data/analysis.ipynb"}}
	got := targetPathFromInput(input)
	if got != "/data/analysis.ipynb" {
		t.Errorf("got %q, want /data/analysis.ipynb", got)
	}
}

func TestTargetPathFromInput_BashPathScraping(t *testing.T) {
	// Bash commands should have path-like tokens extracted
	cases := []struct {
		cmd  string
		want string
	}{
		{"cat src/main.go", "src/main.go"},
		{"python scripts/train.py", "scripts/train.py"},
		{"echo hello", ""}, // no path-like token
		{"ls -la /var/log", "/var/log"},
	}
	for _, tc := range cases {
		input := HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": tc.cmd}}
		got := targetPathFromInput(input)
		if got != tc.want {
			t.Errorf("cmd=%q: got %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestDetectDrift_NoTargetPath(t *testing.T) {
	intent := ChainEvent{
		EventType: "intent",
		Payload: map[string]interface{}{
			"entry_id":   "e1",
			"task_class": "refactor",
			"file_paths": []interface{}{"src/foo.go"},
		},
	}
	// Bash command with no path-like tokens
	res := DetectDrift(
		HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "echo hello"}},
		[]ChainEvent{intent},
		0.5,
	)
	if res.Fired {
		t.Errorf("Fired=true with no target path; want false (reason=%q)", res.Reason)
	}
	if res.Reason != "no-target-path" {
		t.Errorf("reason=%q want no-target-path", res.Reason)
	}
}

func TestDetectDrift_EmptyFilePaths(t *testing.T) {
	// Intent with entry_id but no file_paths
	intent := ChainEvent{
		EventType: "intent",
		Payload: map[string]interface{}{
			"entry_id":   "e2",
			"task_class": "explore",
		},
	}
	res := DetectDrift(
		HookInput{ToolName: "Edit", ToolInput: map[string]interface{}{"file_path": "/etc/passwd"}},
		[]ChainEvent{intent},
		0.5,
	)
	if res.Fired {
		t.Error("intent with no file_paths should not produce drift signal")
	}
	if res.Reason != "no-intent-recorded" {
		t.Errorf("reason=%q; when FilePaths is empty, EntryID should also be treated as no intent", res.Reason)
	}
}

func TestDetectDrift_LowBlastOutOfScope(t *testing.T) {
	// Write to a low-blast file path → not governance/vcs, not Bash
	// Score should be 0.5 (out-of-scope low-blast)
	intent := ChainEvent{
		EventType: "intent",
		Payload: map[string]interface{}{
			"entry_id":   "e3",
			"task_class": "refactor",
			"file_paths": []interface{}{"apps/cli/src/main.ts"},
		},
	}
	res := DetectDrift(
		HookInput{ToolName: "Edit", ToolInput: map[string]interface{}{"file_path": "/etc/passwd"}},
		[]ChainEvent{intent},
		0.5,
	)
	// Edit is a local-file-write (blast score ~0.2 scope, but scoreBlastRadius gives ~0.24)
	// The drift score is 0.5 for out-of-scope write (not high blast)
	if res.Score != 0.5 {
		t.Errorf("low-blast out-of-scope score=%v want 0.5 (reason=%q)", res.Score, res.Reason)
	}
}

func TestDetectDrift_HighBlastOutOfScope(t *testing.T) {
	// High-blast Bash writing out of scope
	intent := ChainEvent{
		EventType: "intent",
		Payload: map[string]interface{}{
			"entry_id":   "e4",
			"task_class": "refactor",
			"file_paths": []interface{}{"apps/cli/src/main.ts"},
		},
	}
	res := DetectDrift(
		HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "rm -rf /home/user/data"}},
		[]ChainEvent{intent},
		0.5,
	)
	// rm -rf is recursive-delete (blast > 0.5), so this should be hard drift = 0.8
	if res.Score < 0.7 {
		t.Errorf("high-blast out-of-scope score=%v want >= 0.8 (reason=%q)", res.Score, res.Reason)
	}
}