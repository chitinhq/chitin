package gov

import (
	"strings"
	"testing"
)

func TestParseEscalateRule_AllFieldsExplicit(t *testing.T) {
	yaml := `
id: parse-test
mode: enforce
rules:
  - id: foo
    action: shell.exec
    effect: escalate
    channel: cli-only
    timeout_seconds: 1200
    remember_window_seconds: 0
    notify_template: "custom template body"
`
	p, err := parsePolicyYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Rules) != 1 {
		t.Fatalf("rules: got %d, want 1", len(p.Rules))
	}
	r := p.Rules[0]
	if r.Effect != EffectEscalate {
		t.Errorf("Effect: got %q, want %q", r.Effect, EffectEscalate)
	}
	if r.Escalation == nil {
		t.Fatal("Escalation: got nil, want struct")
	}
	if r.Escalation.Channel != "cli-only" {
		t.Errorf("Channel: %q", r.Escalation.Channel)
	}
	if r.Escalation.TimeoutSeconds != 1200 {
		t.Errorf("TimeoutSeconds: %d", r.Escalation.TimeoutSeconds)
	}
	if r.Escalation.RememberWindowSeconds != 0 {
		t.Errorf("RememberWindowSeconds: %d", r.Escalation.RememberWindowSeconds)
	}
	if r.Escalation.NotifyTemplate != "custom template body" {
		t.Errorf("NotifyTemplate: %q", r.Escalation.NotifyTemplate)
	}
}

func TestParseEscalateRule_DefaultsApplied(t *testing.T) {
	yaml := `
id: parse-test
mode: enforce
rules:
  - id: foo
    action: shell.exec
    effect: escalate
`
	p, err := parsePolicyYAML([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	r := p.Rules[0]
	if r.Escalation.Channel != "hermes" {
		t.Errorf("default Channel: %q want hermes", r.Escalation.Channel)
	}
	// Default lowered from 600 → 45 (PR #382 dogfood, 2026-05-07): the
	// Claude Code PreToolUse hook timeout is ~60s by default, so 600s
	// guarantees the harness kills the kernel before any operator can
	// approve. 45s leaves a 15s margin. Per-rule config can override.
	if r.Escalation.TimeoutSeconds != 45 {
		t.Errorf("default TimeoutSeconds: %d want 45", r.Escalation.TimeoutSeconds)
	}
	if r.Escalation.RememberWindowSeconds != 300 {
		t.Errorf("default RememberWindowSeconds: %d want 300", r.Escalation.RememberWindowSeconds)
	}
}

func TestParseEscalateRule_ValidationErrors(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantSubst string
	}{
		{
			name: "timeout too short",
			yaml: `
mode: enforce
rules:
  - id: x
    action: shell.exec
    effect: escalate
    timeout_seconds: 5
`,
			wantSubst: "timeout_seconds",
		},
		{
			name: "timeout too long",
			yaml: `
mode: enforce
rules:
  - id: x
    action: shell.exec
    effect: escalate
    timeout_seconds: 100000
`,
			wantSubst: "timeout_seconds",
		},
		{
			name: "unknown channel",
			yaml: `
mode: enforce
rules:
  - id: x
    action: shell.exec
    effect: escalate
    channel: weird
`,
			wantSubst: "channel",
		},
		{
			name: "escalate on unknown action",
			yaml: `
mode: enforce
rules:
  - id: x
    action: unknown
    effect: escalate
`,
			wantSubst: "unknown",
		},
		{
			name: "negative remember_window",
			yaml: `
mode: enforce
rules:
  - id: x
    action: shell.exec
    effect: escalate
    remember_window_seconds: -1
`,
			wantSubst: "remember_window_seconds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePolicyYAML([]byte(tc.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubst) {
				t.Errorf("err = %v, want substring %q", err, tc.wantSubst)
			}
		})
	}
}
