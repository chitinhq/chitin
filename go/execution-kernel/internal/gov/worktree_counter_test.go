package gov

import (
	"testing"
)

func TestWorktreeDenyDoesNotEscalateInGuideMode(t *testing.T) {
	repo, _ := newWorktreeFixture(t) // primary checkout

	g := newWorktreeGate(t, repo, "guide") // guide mode on primary checkout

	// Before the fix: 12 worktree denials would push to lockdown.
	// After the fix: counter should stay at "normal".
	initialLevel := g.Counter.Level("agent1")
	for i := 0; i < 12; i++ {
		g.Evaluate(Action{Type: ActFileWrite, Target: "test.md"}, "agent1", nil)
	}
	finalLevel := g.Counter.Level("agent1")

	t.Logf("After 12 worktree denials (guide mode): level=%s (was %s)", finalLevel, initialLevel)

	if finalLevel != initialLevel {
		t.Errorf("worktree-denied decision incremented escalation counter — expect %s, got %s", initialLevel, finalLevel)
	}
}
