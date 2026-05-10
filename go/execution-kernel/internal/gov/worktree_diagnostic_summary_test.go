package gov

import (
	"encoding/json"
	"testing"
	"time"
)

func mkWorktreeDiagnosticDecision(t *testing.T, ts time.Time, agent, driver string, actionType ActionType, target string) string {
	t.Helper()
	type wire struct {
		Allowed                  bool   `json:"allowed"`
		Mode                     string `json:"mode"`
		RuleID                   string `json:"rule_id"`
		Agent                    string `json:"agent,omitempty"`
		Driver                   string `json:"driver,omitempty"`
		ActionType               string `json:"action_type"`
		ActionTarget             string `json:"action_target"`
		Ts                       string `json:"ts"`
		WorktreeDiagnosticRuleID string `json:"worktree_diagnostic_rule_id,omitempty"`
		WorktreeStatus           string `json:"worktree_status,omitempty"`
		WorktreeReason           string `json:"worktree_reason,omitempty"`
	}
	b, err := json.Marshal(wire{
		Allowed:                  true,
		Mode:                     "guide",
		RuleID:                   "allow",
		Agent:                    agent,
		Driver:                   driver,
		ActionType:               string(actionType),
		ActionTarget:             target,
		Ts:                       ts.UTC().Format(time.RFC3339),
		WorktreeDiagnosticRuleID: "worktree-required-diagnostic",
		WorktreeStatus:           "primary",
		WorktreeReason:           "side-effect action evaluated from primary git checkout",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReadRecent_RestoresActionFromWireFields(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	writeDecisionLines(t, dir, "2026-05-10", []string{
		mkWorktreeDiagnosticDecision(t, now.Add(-time.Minute), "codex", "codex", ActFileWrite, "src/main.go"),
	})

	out, err := ReadRecent(ReadRecentArgs{Dir: dir, WindowHours: 1, Limit: 10, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 decision, got %d", len(out))
	}
	if out[0].Action.Type != ActFileWrite || out[0].Action.Target != "src/main.go" {
		t.Fatalf("Action not restored from wire fields: %+v", out[0].Action)
	}
}

func TestReadWorktreeDiagnosticSummary(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	writeDecisionLines(t, dir, "2026-05-10", []string{
		mkDecision(t, now.Add(-4*time.Minute), "normal-row"),
		mkWorktreeDiagnosticDecision(t, now.Add(-3*time.Minute), "codex", "codex", ActFileWrite, "src/main.go"),
		mkWorktreeDiagnosticDecision(t, now.Add(-2*time.Minute), "codex", "codex", ActGitCommit, "commit"),
		mkWorktreeDiagnosticDecision(t, now.Add(-time.Minute), "hermes", "hermes", ActFileWrite, "README.md"),
	})

	summary, err := ReadWorktreeDiagnosticSummary(ReadRecentArgs{
		Dir: dir, WindowHours: 1, Limit: 10, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 3 {
		t.Fatalf("Total=%d want 3", summary.Total)
	}
	if len(summary.Recent) != 3 || summary.Recent[0].Agent != "hermes" || summary.Recent[0].ActionTarget != "README.md" {
		t.Fatalf("recent rows should be diagnostic-only newest-first, got %+v", summary.Recent)
	}
	if len(summary.ByAgent) != 2 || summary.ByAgent[0].Key != "codex" || summary.ByAgent[0].Count != 2 {
		t.Fatalf("ByAgent not sorted/counting correctly: %+v", summary.ByAgent)
	}
	if len(summary.ByAction) != 2 || summary.ByAction[0].Key != string(ActFileWrite) || summary.ByAction[0].Count != 2 {
		t.Fatalf("ByAction not sorted/counting correctly: %+v", summary.ByAction)
	}
}
