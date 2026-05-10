package router

import (
	"testing"
)

func TestStringField(t *testing.T) {
	m := map[string]interface{}{
		"name":     "alice",
		"age":      30,
		"command":  "  ls -la  ",
		"missing":  "",
	}
	// String present
	if got := stringField(m, "name"); got != "alice" {
		t.Errorf("stringField(name) = %q, want %q", got, "alice")
	}
	// TrimSpace
	if got := stringField(m, "command"); got != "ls -la" {
		t.Errorf("stringField(command) = %q, want %q", got, "ls -la")
	}
	// Non-string value
	if got := stringField(m, "age"); got != "" {
		t.Errorf("stringField(age) = %q, want empty", got)
	}
	// Missing key
	if got := stringField(m, "nonexistent"); got != "" {
		t.Errorf("stringField(nonexistent) = %q, want empty", got)
	}
	// Empty string value
	if got := stringField(m, "missing"); got != "" {
		t.Errorf("stringField(missing) = %q, want empty", got)
	}
}

func TestBlastRadius_HardReset(t *testing.T) {
	input := HookInput{
		ToolName: "Bash",
		ToolInput: map[string]interface{}{
			"command": "git reset --hard HEAD~1",
		},
	}
	_, reason := blastRadiusAxes(input)
	if reason != "hard-reset" {
		t.Errorf("expected hard-reset, got %q", reason)
	}
}

func TestBlastRadius_GitPushNormal(t *testing.T) {
	input := HookInput{
		ToolName: "Bash",
		ToolInput: map[string]interface{}{
			"command": "git push origin main",
		},
	}
	_, reason := blastRadiusAxes(input)
	if reason != "git-push" {
		t.Errorf("expected git-push, got %q", reason)
	}
}

func TestBlastRadius_GitHubStateChange(t *testing.T) {
	input := HookInput{
		ToolName: "Bash",
		ToolInput: map[string]interface{}{
			"command": "gh pr create --title test",
		},
	}
	_, reason := blastRadiusAxes(input)
	if reason != "github-state-change" {
		t.Errorf("expected github-state-change, got %q", reason)
	}
}

func TestBlastRadius_NetworkOut(t *testing.T) {
	input := HookInput{
		ToolName: "Bash",
		ToolInput: map[string]interface{}{
			"command": "curl -s https://example.com",
		},
	}
	_, reason := blastRadiusAxes(input)
	if reason != "network-out" {
		t.Errorf("expected network-out, got %q", reason)
	}
}

func TestBlastRadius_Orchestration(t *testing.T) {
	tools := []string{"Task", "Agent", "Skill", "CronCreate", "EnterWorktree"}
	for _, tool := range tools {
		input := HookInput{ToolName: tool, ToolInput: map[string]interface{}{}}
		_, reason := blastRadiusAxes(input)
		if reason != "orchestration-shape" {
			t.Errorf("tool=%q: expected orchestration-shape, got %q", tool, reason)
		}
	}
}

func TestBlastRadius_GovernanceConfigWrite(t *testing.T) {
	input := HookInput{
		ToolName: "Edit",
		ToolInput: map[string]interface{}{
			"file_path": "/project/chitin.yaml",
		},
	}
	_, reason := blastRadiusAxes(input)
	if reason != "governance-config-write" {
		t.Errorf("expected governance-config-write, got %q", reason)
	}
}

func TestBlastRadius_GeneratedOrVcsWrite(t *testing.T) {
	input := HookInput{
		ToolName: "Edit",
		ToolInput: map[string]interface{}{
			"file_path": "/project/node_modules/foo/index.js",
		},
	}
	_, reason := blastRadiusAxes(input)
	if reason != "generated-or-vcs-write" {
		t.Errorf("expected generated-or-vcs-write, got %q", reason)
	}
}

func TestBlastRadius_LocalFileWrite(t *testing.T) {
	input := HookInput{
		ToolName: "Write",
		ToolInput: map[string]interface{}{
			"file_path": "/home/user/code/main.go",
		},
	}
	axes, reason := blastRadiusAxes(input)
	if reason != "local-file-write" {
		t.Errorf("expected local-file-write, got %q", reason)
	}
	if axes["scope"] != 0.2 {
		t.Errorf("expected scope=0.2, got %f", axes["scope"])
	}
}

func TestBlastRadius_NotebookPath(t *testing.T) {
	input := HookInput{
		ToolName: "NotebookEdit",
		ToolInput: map[string]interface{}{
			"notebook_path": "/project/analysis.ipynb",
		},
	}
	_, reason := blastRadiusAxes(input)
	if reason != "local-file-write" {
		t.Errorf("expected local-file-write via notebook_path, got %q", reason)
	}
}

func TestBlastRadius_UnknownTool(t *testing.T) {
	input := HookInput{
		ToolName: "SomeNewTool",
		ToolInput: map[string]interface{}{},
	}
	_, reason := blastRadiusAxes(input)
	if reason != "unknown-tool:SomeNewTool" {
		t.Errorf("expected unknown-tool:SomeNewTool, got %q", reason)
	}
}

func TestFloatRound(t *testing.T) {
	if got := floatRound(0.12345, 3); got != 0.123 {
		t.Errorf("floatRound(0.12345, 3) = %f, want 0.123", got)
	}
	if got := floatRound(0.9995, 3); got != 1.0 {
		t.Errorf("floatRound(0.9995, 3) = %f, want 1.0", got)
	}
	if got := floatRound(0.5, 0); got != 1.0 {
		t.Errorf("floatRound(0.5, 0) = %f, want 1.0", got)
	}
}