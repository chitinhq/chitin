package router

import (
	"math"
	"testing"
)

// blastRadiusAxes is exercised indirectly via ScoreBlastRadius, but
// we want complete coverage of each tool category and edge case.
// This file complements blast_radius_test.go which tests the
// ScoreBlastRadius top-level function.

func TestBlastRadiusAxes_AllReadTools(t *testing.T) {
	readTools := []string{"Read", "Glob", "Grep", "LS", "TaskGet", "TaskList", "TaskOutput", "ToolSearch", "AskUserQuestion", "EnterPlanMode", "ExitPlanMode"}
	for _, name := range readTools {
		axes, reason := blastRadiusAxes(HookInput{ToolName: name})
		if reason != "read-only-tool" {
			t.Errorf("%s: reason=%q want read-only-tool", name, reason)
		}
		if axes["reversibility"] != 1.0 {
			t.Errorf("%s: reversibility=%v want 1.0", name, axes["reversibility"])
		}
		if axes["scope"] != 0.0 || axes["visibility"] != 0.0 || axes["counterparties"] != 0.0 {
			t.Errorf("%s: scope=%v visibility=%v counterparties=%v; all want 0.0", name, axes["scope"], axes["visibility"], axes["counterparties"])
		}
	}
}

func TestBlastRadiusAxes_NetworkTools(t *testing.T) {
	netTools := []string{"WebFetch", "WebSearch", "PushNotification", "RemoteTrigger"}
	for _, name := range netTools {
		axes, reason := blastRadiusAxes(HookInput{ToolName: name})
		if reason != "outbound-network" {
			t.Errorf("%s: reason=%q want outbound-network", name, reason)
		}
		if axes["reversibility"] != 0.7 {
			t.Errorf("%s: reversibility=%v want 0.7", name, axes["reversibility"])
		}
		if axes["counterparties"] != 0.7 {
			t.Errorf("%s: counterparties=%v want 0.7", name, axes["counterparties"])
		}
	}
}

func TestBlastRadiusAxes_LocalWriteTools(t *testing.T) {
	writeTools := []string{"Edit", "Write", "NotebookEdit", "TodoWrite"}
	for _, name := range writeTools {
		axes, reason := blastRadiusAxes(HookInput{ToolName: name, ToolInput: map[string]interface{}{"file_path": "/tmp/normal.txt"}})
		if reason != "local-file-write" {
			t.Errorf("%s: reason=%q want local-file-write", name, reason)
		}
		if axes["reversibility"] != 0.6 {
			t.Errorf("%s: reversibility=%v want 0.6", name, axes["reversibility"])
		}
		if axes["scope"] != 0.2 {
			t.Errorf("%s: scope=%v want 0.2", name, axes["scope"])
		}
	}
}

func TestBlastRadiusAxes_GovernanceConfigWrite(t *testing.T) {
	governancePaths := []string{"/repo/chitin.yaml", "/home/user/project/CLAUDE.md", "/home/user/project/.claude/config.json"}
	for _, path := range governancePaths {
		axes, reason := blastRadiusAxes(HookInput{ToolName: "Write", ToolInput: map[string]interface{}{"file_path": path}})
		if reason != "governance-config-write" {
			t.Errorf("path=%s: reason=%q want governance-config-write", path, reason)
		}
		if axes["scope"] != 0.8 {
			t.Errorf("path=%s: scope=%v want 0.8", path, axes["scope"])
		}
	}
}

func TestBlastRadiusAxes_GeneratedOrVCSWrite(t *testing.T) {
	vcsPaths := []string{"/repo/node_modules/pkg/index.js", "/repo/.git/config", "/repo/dist/bundle.min.js", "/repo/build/output.o"}
	for _, path := range vcsPaths {
		axes, reason := blastRadiusAxes(HookInput{ToolName: "Edit", ToolInput: map[string]interface{}{"file_path": path}})
		if reason != "generated-or-vcs-write" {
			t.Errorf("path=%s: reason=%q want generated-or-vcs-write", path, reason)
		}
		if axes["scope"] != 0.9 {
			t.Errorf("path=%s: scope=%v want 0.9", path, axes["scope"])
		}
	}
}

func TestBlastRadiusAxes_NotebookPath(t *testing.T) {
	// Write with notebook_path instead of file_path
	axes, reason := blastRadiusAxes(HookInput{ToolName: "NotebookEdit", ToolInput: map[string]interface{}{"notebook_path": "/data/analysis.ipynb"}})
	if reason != "local-file-write" {
		t.Errorf("reason=%q want local-file-write", reason)
	}
	if axes["reversibility"] != 0.6 {
		t.Errorf("reversibility=%v want 0.6", axes["reversibility"])
	}
}

func TestBlastRadiusAxes_BashHardReset(t *testing.T) {
	axes, reason := blastRadiusAxes(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "git reset --hard HEAD~1"}})
	if reason != "hard-reset" {
		t.Errorf("reason=%q want hard-reset", reason)
	}
	if axes["reversibility"] != 0.0 {
		t.Errorf("reversibility=%v want 0.0", axes["reversibility"])
	}
	if axes["scope"] != 0.4 {
		t.Errorf("scope=%v want 0.4", axes["scope"])
	}
}

func TestBlastRadiusAxes_BashGitPush(t *testing.T) {
	axes, reason := blastRadiusAxes(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "git push origin feature/test"}})
	if reason != "git-push" {
		t.Errorf("reason=%q want git-push", reason)
	}
	if axes["reversibility"] != 0.2 {
		t.Errorf("reversibility=%v want 0.2", axes["reversibility"])
	}
}

func TestBlastRadiusAxes_BashGitHubStateChange(t *testing.T) {
	commands := map[string]string{
		"gh pr create --title test":  "github-state-change",
		"gh pr merge 123":            "github-state-change",
		"gh issue close 456":         "github-state-change",
	}
	for cmd, wantReason := range commands {
		_, reason := blastRadiusAxes(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": cmd}})
		if reason != wantReason {
			t.Errorf("cmd=%q: reason=%q want %q", cmd, reason, wantReason)
		}
	}
}

func TestBlastRadiusAxes_BashNetworkOut(t *testing.T) {
	commands := []string{"curl https://api.example.com/data", "wget -qO- https://example.com"}
	for _, cmd := range commands {
		axes, reason := blastRadiusAxes(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": cmd}})
		if reason != "network-out" {
			t.Errorf("cmd=%q: reason=%q want network-out", cmd, reason)
		}
		if axes["visibility"] != 0.3 {
			t.Errorf("cmd=%q: visibility=%v want 0.3", cmd, axes["visibility"])
		}
	}
}

func TestBlastRadiusAxes_BashGeneric(t *testing.T) {
	axes, reason := blastRadiusAxes(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "ls -la"}})
	if reason != "generic-shell-exec" {
		t.Errorf("reason=%q want generic-shell-exec", reason)
	}
	if axes["reversibility"] != 0.5 || axes["scope"] != 0.3 {
		t.Errorf("reversibility=%v scope=%v; want 0.5, 0.3", axes["reversibility"], axes["scope"])
	}
}

func TestBlastRadiusAxes_OrchestrationTools(t *testing.T) {
	orchTools := []string{"Task", "Agent", "Skill", "TaskCreate", "TaskUpdate", "TaskStop", "CronCreate", "CronDelete", "CronList", "ScheduleWakeup", "Monitor", "EnterWorktree", "ExitWorktree"}
	for _, name := range orchTools {
		axes, reason := blastRadiusAxes(HookInput{ToolName: name})
		if reason != "orchestration-shape" {
			t.Errorf("%s: reason=%q want orchestration-shape", name, reason)
		}
		if axes["reversibility"] != 0.5 {
			t.Errorf("%s: reversibility=%v want 0.5", name, axes["reversibility"])
		}
	}
}

func TestBlastRadiusAxes_ScoreFormula(t *testing.T) {
	// Verify the blended score formula:
	// score = (1-reversibility)*0.4 + scope*0.25 + visibility*0.2 + counterparties*0.15
	// Test with a known case: generic-shell-exec has (0.5, 0.3, 0.0, 0.0)
	// Expected: (1-0.5)*0.4 + 0.3*0.25 + 0.0 + 0.0 = 0.2 + 0.075 = 0.275
	score := ScoreBlastRadius(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "ls -la"}}, 0.0)
	if math.Abs(score.Score-0.275) > 0.001 {
		t.Errorf("generic-shell-exec score=%v want 0.275", score.Score)
	}

	// recursive-delete: (0.0, 1.0, 0.0, 0.0)
	// Expected: 1.0*0.4 + 1.0*0.25 + 0 + 0 = 0.65
	score = ScoreBlastRadius(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "rm -rf /tmp/foo"}}, 0.0)
	if math.Abs(score.Score-0.65) > 0.001 {
		t.Errorf("recursive-delete score=%v want 0.65", score.Score)
	}

	// force-push: (0.0, 0.5, 0.9, 0.7)
	// Expected: 1.0*0.4 + 0.5*0.25 + 0.9*0.2 + 0.7*0.15 = 0.4 + 0.125 + 0.18 + 0.105 = 0.81
	score = ScoreBlastRadius(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "git push --force origin main"}}, 0.0)
	if math.Abs(score.Score-0.81) > 0.001 {
		t.Errorf("force-push score=%v want 0.81", score.Score)
	}
}

func TestBlastRadiusAxes_AxisFields(t *testing.T) {
	// Verify that ScoreBlastRadius populates Axis map from blastRadiusAxes
	score := ScoreBlastRadius(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "rm -rf /tmp/foo"}}, 0.0)
	axisKeys := []string{"reversibility", "scope", "visibility", "counterparties"}
	for _, k := range axisKeys {
		if _, ok := score.Axis[k]; !ok {
			t.Errorf("Axis map missing key %q", k)
		}
	}
	if score.Axis["reversibility"] != 0.0 {
		t.Errorf("Axis[reversibility]=%v want 0.0", score.Axis["reversibility"])
	}
	if score.Axis["scope"] != 1.0 {
		t.Errorf("Axis[scope]=%v want 1.0", score.Axis["scope"])
	}
}

func TestBlastRadiusAxes_RM_RecursiveFlags(t *testing.T) {
	// Test multiple rm -r variants
	variants := []struct {
		cmd    string
		match  bool
	}{
		{"rm -rf /tmp/x", true},
		{"rm -fr /tmp/x", true},
		{"rm -r /tmp/x", true},
		{"rm --recursive /tmp/x", true},
		{"rm /tmp/x", false},       // plain rm, no recursive flag
		{"rm -i /tmp/x", false},    // interactive, not recursive
	}
	for _, tc := range variants {
		score := ScoreBlastRadius(HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": tc.cmd}}, 0.0)
		isRecursive := score.Reason == "recursive-delete"
		if isRecursive != tc.match {
			t.Errorf("cmd=%q: reason=%q; want recursive-delete=%v", tc.cmd, score.Reason, tc.match)
		}
	}
}