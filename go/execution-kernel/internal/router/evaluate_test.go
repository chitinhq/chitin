package router

import (
	"testing"
	"time"
)

func TestEvaluateHeuristics_AllowReadOnly(t *testing.T) {
	policy := DefaultPolicy()
	policy.Enabled = true
	res := EvaluateHeuristics(
		HookInput{ToolName: "Read", ToolInput: map[string]interface{}{"file_path": "/etc/hosts"}},
		policy, nil, false, "", time.Now(),
	)
	if res.Decision != "allow" {
		t.Errorf("decision=%q want allow", res.Decision)
	}
	if res.AdvisorNeeded {
		t.Error("advisor_needed=true on read-only tool; want false")
	}
	if res.AdvisorRequest != nil {
		t.Error("advisor_request non-nil when advisor not needed")
	}
	if res.HeuristicOutcome.BlastRadius == nil {
		t.Error("blast_radius outcome missing")
	}
}

func TestEvaluateHeuristics_BlastRadiusFiresAdvisor(t *testing.T) {
	policy := DefaultPolicy()
	policy.Enabled = true
	res := EvaluateHeuristics(
		HookInput{
			ToolName:  "Bash",
			ToolInput: map[string]interface{}{"command": "rm -rf /tmp/foo"},
			SessionID: "sess-1",
		},
		policy, nil, false, "", time.Now(),
	)
	if !res.AdvisorNeeded {
		t.Error("advisor_needed=false on rm-rf; want true (blast above threshold)")
	}
	if res.AdvisorRequest == nil {
		t.Fatal("advisor_request nil despite advisor_needed=true")
	}
	if res.AdvisorRequest.ProposedAction.ToolName != "Bash" {
		t.Errorf("proposed_action.tool_name=%q want Bash", res.AdvisorRequest.ProposedAction.ToolName)
	}
	if res.AdvisorRequest.HeuristicOutcome.BlastRadius == nil ||
		!res.AdvisorRequest.HeuristicOutcome.BlastRadius.Fired {
		t.Error("advisor_request.heuristic_outcome.blast_radius didn't fire")
	}
}

func TestEvaluateHeuristics_KernelDenyTriggersAdvisorWhenConfigured(t *testing.T) {
	policy := DefaultPolicy()
	policy.Enabled = true
	policy.Advisor.When = []string{"kernel_denied"}
	res := EvaluateHeuristics(
		HookInput{ToolName: "Read", ToolInput: map[string]interface{}{"file_path": "/etc/hosts"}},
		policy, nil, true, "policy_invalid: missing rule", time.Now(),
	)
	if res.Decision != "deny" {
		t.Errorf("decision=%q want deny", res.Decision)
	}
	if !res.AdvisorNeeded {
		t.Error("advisor_needed=false on kernel_denied trigger; want true")
	}
	if res.AdvisorRequest == nil || res.AdvisorRequest.Question == "" {
		t.Fatal("advisor_request missing question on kernel-deny path")
	}
}

func TestEvaluateHeuristics_PolicyDisabledStillBuildsOutcome(t *testing.T) {
	policy := DefaultPolicy()
	policy.Enabled = true
	policy.Advisor.Enabled = false
	res := EvaluateHeuristics(
		HookInput{
			ToolName:  "Bash",
			ToolInput: map[string]interface{}{"command": "rm -rf /tmp/foo"},
		},
		policy, nil, false, "", time.Now(),
	)
	if res.AdvisorNeeded {
		t.Error("advisor_needed=true with advisor disabled; want false")
	}
	if res.HeuristicOutcome.BlastRadius == nil || !res.HeuristicOutcome.BlastRadius.Fired {
		t.Error("blast-radius should still fire even when advisor is off")
	}
}

func TestEvaluateHeuristics_FlounderingFires(t *testing.T) {
	policy := DefaultPolicy()
	policy.Enabled = true
	ev := func(target string) ChainEvent {
		return ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"tool_name":     "Bash",
				"action_target": target,
				"decision":      "allow",
			},
		}
	}
	now, _ := time.Parse(time.RFC3339, "2026-05-03T20:00:30Z")
	res := EvaluateHeuristics(
		HookInput{ToolName: "Read", ToolInput: map[string]interface{}{"file_path": "/etc/hosts"}},
		policy,
		[]ChainEvent{ev("rm /tmp/x"), ev("rm /tmp/x"), ev("rm /tmp/x")},
		false, "",
		now,
	)
	if res.HeuristicOutcome.Floundering == nil || !res.HeuristicOutcome.Floundering.Fired {
		t.Error("floundering didn't fire on three identical calls")
	}
	if !res.AdvisorNeeded {
		t.Error("advisor_needed=false despite floundering; want true (default trigger)")
	}
}
