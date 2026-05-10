package replay

import (
	"testing"
)

func TestExtractAxis(t *testing.T) {
	ev := map[string]interface{}{
		"agent_instance_id": "claude-123",
	}
	payload := map[string]interface{}{
		"tool_name":    "Bash",
		"action_type": "shell.exec",
		"rule_id":     "r1",
		"decision":     "allow",
	}

	if got := extractAxis(ev, payload, "tool_name"); got != "Bash" {
		t.Errorf("tool_name axis = %q, want %q", got, "Bash")
	}
	if got := extractAxis(ev, payload, "action_type"); got != "shell.exec" {
		t.Errorf("action_type axis = %q, want %q", got, "shell.exec")
	}
	if got := extractAxis(ev, payload, "rule_id"); got != "r1" {
		t.Errorf("rule_id axis = %q, want %q", got, "r1")
	}
	if got := extractAxis(ev, payload, "decision"); got != "allow" {
		t.Errorf("decision axis = %q, want %q", got, "allow")
	}
	if got := extractAxis(ev, payload, "agent"); got != "claude-123" {
		t.Errorf("agent axis = %q, want %q", got, "claude-123")
	}
	// Unknown axis returns empty
	if got := extractAxis(ev, payload, "unknown_axis"); got != "" {
		t.Errorf("unknown axis = %q, want empty", got)
	}
	// Missing field returns empty
	payloadEmpty := map[string]interface{}{}
	if got := extractAxis(ev, payloadEmpty, "tool_name"); got != "" {
		t.Errorf("missing tool_name = %q, want empty", got)
	}
}