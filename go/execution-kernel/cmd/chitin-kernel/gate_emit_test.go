package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

// TestBuildDecisionEvent_DecisionStr exhausts the (Allowed, Mode) state
// space and asserts decisionStr maps to the OUTCOME (allow/deny/guide),
// NOT the policy mode. Closes #77 audit.
//
// The five reachable cases (six minus the impossible Allowed=false +
// monitor — monitor flips that to Allowed=true at gate-time):
func TestBuildDecisionEvent_DecisionStr(t *testing.T) {
	cases := []struct {
		name    string
		allowed bool
		mode    string
		want    string
	}{
		{"allow under enforce", true, "enforce", "allow"},
		{"allow under guide (was the contested case)", true, "guide", "allow"},
		{"allow under monitor (override flipped a deny)", true, "monitor", "allow"},
		{"deny under enforce (hard block)", false, "enforce", "deny"},
		{"deny under guide (soft deny — model can retry)", false, "guide", "guide"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &gov.Decision{
				Allowed: tc.allowed,
				Mode:    tc.mode,
				RuleID:  "test-rule",
				Ts:      "2026-05-02T22:00:00Z",
			}
			ev := buildDecisionEvent(d, "test-chain", "claude-code")
			var payload map[string]any
			if err := json.Unmarshal(ev.Payload, &payload); err != nil {
				t.Fatalf("payload: %v", err)
			}
			got, _ := payload["decision"].(string)
			if got != tc.want {
				t.Errorf("decisionStr: got %q, want %q (Allowed=%v Mode=%q)",
					got, tc.want, tc.allowed, tc.mode)
			}
		})
	}
}

func TestBuildDecisionEvent_CarriesTypedIdentity(t *testing.T) {
	d := &gov.Decision{
		Allowed: true,
		Mode:    "enforce",
		RuleID:  "default-allow-shell",
		Ts:      "2026-05-10T12:00:00Z",
		Agent:   "hermes",
		Action:  gov.Action{Type: gov.ActShellExec, Target: "ls"},

		AgentInstanceID:   "inst-123",
		AgentFingerprint:  "8b77a3e91c04",
		Driver:            "hermes",
		Model:             "qwen3.6:27b",
		Role:              "researcher",
		StationPromptHash: "sha256:prompt",
		SkillsToolsHash:   "sha256:tools",
		SoulLens:          "karpathy",
		ClaimedAuthority:  "worker",
		Authority:         "worker",
		WorkflowID:        "wf-123",
	}

	ev := buildDecisionEvent(d, "test-chain", "hermes")
	if ev.AgentInstanceID != "inst-123" {
		t.Fatalf("AgentInstanceID: got %q want inst-123", ev.AgentInstanceID)
	}
	if !isLowerHexLen(ev.AgentFingerprint, 64) {
		t.Fatalf("event AgentFingerprint must be 64 lowercase hex, got %q", ev.AgentFingerprint)
	}
	if ev.AgentFingerprint == d.AgentFingerprint {
		t.Fatalf("12-char dispatch fingerprint must be expanded for event envelope compatibility")
	}
	for k, want := range map[string]string{
		"agent":               "hermes",
		"agent_instance_id":   "inst-123",
		"agent_fingerprint":   "8b77a3e91c04",
		"driver":              "hermes",
		"model":               "qwen3.6:27b",
		"role":                "researcher",
		"station_prompt_hash": "sha256:prompt",
		"skills_tools_hash":   "sha256:tools",
		"soul_lens":           "karpathy",
		"authority":           "worker",
		"workflow_id":         "wf-123",
	} {
		if got := ev.Labels[k]; got != want {
			t.Fatalf("label %s: got %q want %q", k, got, want)
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["claimed_authority"] != "worker" {
		t.Fatalf("payload claimed_authority: got %v want worker", payload["claimed_authority"])
	}
	if payload["agent_fingerprint"] != "8b77a3e91c04" ||
		payload["station_prompt_hash"] != "sha256:prompt" ||
		payload["skills_tools_hash"] != "sha256:tools" ||
		payload["soul_lens"] != "karpathy" {
		t.Fatalf("payload missing typed identity: %#v", payload)
	}
}

func TestBuildDecisionEvent_PreservesFullEnvelopeFingerprint(t *testing.T) {
	full := strings.Repeat("a", 64)
	ev := buildDecisionEvent(&gov.Decision{
		Allowed:          true,
		Mode:             "enforce",
		RuleID:           "default-allow-shell",
		Ts:               "2026-05-10T12:00:00Z",
		AgentFingerprint: full,
	}, "test-chain", "codex")
	if ev.AgentFingerprint != full {
		t.Fatalf("full fingerprint changed: got %q want %q", ev.AgentFingerprint, full)
	}
}
