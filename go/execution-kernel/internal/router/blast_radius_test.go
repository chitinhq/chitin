package router

import (
	"math"
	"testing"
)

func TestScoreBlastRadius_ReadOnly(t *testing.T) {
	score := ScoreBlastRadius(HookInput{ToolName: "Read"}, 0.6)
	if score.Score != 0 {
		t.Errorf("score=%v want 0", score.Score)
	}
	if score.Fired {
		t.Error("Fired=true; want false for read-only tool")
	}
	if score.Reason != "read-only-tool" {
		t.Errorf("reason=%q want read-only-tool", score.Reason)
	}
}

func TestScoreBlastRadius_RecursiveDelete(t *testing.T) {
	score := ScoreBlastRadius(HookInput{
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": "rm -rf /tmp/foo"},
	}, 0.6)
	if math.Abs(score.Score-0.65) > 0.01 {
		t.Errorf("score=%v want ~0.65 (recursive-delete)", score.Score)
	}
	if !score.Fired {
		t.Error("Fired=false; want true (rm-rf is blast)")
	}
	if score.Reason != "recursive-delete" {
		t.Errorf("reason=%q want recursive-delete", score.Reason)
	}
}

func TestScoreBlastRadius_ForcePush(t *testing.T) {
	score := ScoreBlastRadius(HookInput{
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": "git push --force origin main"},
	}, 0.6)
	if math.Abs(score.Score-0.81) > 0.01 {
		t.Errorf("score=%v want ~0.81 (force-push)", score.Score)
	}
	if !score.Fired {
		t.Error("Fired=false; want true")
	}
	if score.Reason != "force-push" {
		t.Errorf("reason=%q want force-push", score.Reason)
	}
}

func TestScoreBlastRadius_GovernanceWriteFlaggedReason(t *testing.T) {
	// Score = 0.36 — below default 0.6 threshold.
	// Kernel's no-governance-self-modification rule denies these
	// at the deterministic layer, so the heuristic doesn't NEED
	// to fire. What matters is the REASON tag for telemetry.
	score := ScoreBlastRadius(HookInput{
		ToolName:  "Edit",
		ToolInput: map[string]interface{}{"file_path": "/repo/chitin.yaml"},
	}, 0.6)
	if score.Reason != "governance-config-write" {
		t.Errorf("reason=%q want governance-config-write", score.Reason)
	}
	if score.Score < 0.3 {
		t.Errorf("score=%v want > 0.3", score.Score)
	}
	// Lower threshold catches it
	low := ScoreBlastRadius(HookInput{
		ToolName:  "Edit",
		ToolInput: map[string]interface{}{"file_path": "/repo/chitin.yaml"},
	}, 0.3)
	if !low.Fired {
		t.Error("Fired=false at threshold 0.3; want true")
	}
}

func TestScoreBlastRadius_PackagePublish(t *testing.T) {
	score := ScoreBlastRadius(HookInput{
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": "pnpm publish --access public"},
	}, 0.6)
	if math.Abs(score.Score-0.875) > 0.01 {
		t.Errorf("score=%v want ~0.875 (package-publish)", score.Score)
	}
	if score.Reason != "package-publish" {
		t.Errorf("reason=%q want package-publish", score.Reason)
	}
}

func TestScoreBlastRadius_UnknownTool(t *testing.T) {
	score := ScoreBlastRadius(HookInput{ToolName: "TotallyMadeUp"}, 0.6)
	if score.Reason != "unknown-tool:TotallyMadeUp" {
		t.Errorf("reason=%q want unknown-tool:TotallyMadeUp", score.Reason)
	}
	if score.Score <= 0 || score.Score >= 0.6 {
		t.Errorf("score=%v want > 0 AND < 0.6", score.Score)
	}
}
