package hookdispatch

import "testing"

func TestHookToEventType(t *testing.T) {
	tests := []struct {
		hook string
		want string
	}{
		{"SessionStart", "session_start"},
		{"UserPromptSubmit", "user_prompt"},
		{"PreToolUse", "intended"},
		{"PreCompact", "compaction"},
	}
	for _, tt := range tests {
		got := HookToEventType(tt.hook, nil)
		if got != tt.want {
			t.Errorf("HookToEventType(%q) = %q; want %q", tt.hook, got, tt.want)
		}
	}
}

func TestHookToEventType_PostToolUseSuccessVsFailure(t *testing.T) {
	success := map[string]any{"tool_response": map[string]any{"ok": true}}
	if got := HookToEventType("PostToolUse", success); got != "executed" {
		t.Errorf("PostToolUse success → %q, want executed", got)
	}
	fail := map[string]any{"error": "timeout"}
	if got := HookToEventType("PostToolUse", fail); got != "failed" {
		t.Errorf("PostToolUse failure → %q, want failed", got)
	}
}

func TestHookToEventType_SubagentStop(t *testing.T) {
	if got := HookToEventType("SubagentStop", nil); got != "session_end" {
		t.Errorf("SubagentStop → %q, want session_end (subagent's session)", got)
	}
}

func TestHookToEventType_SessionEnd(t *testing.T) {
	if got := HookToEventType("SessionEnd", nil); got != "session_end" {
		t.Errorf("SessionEnd → %q, want session_end", got)
	}
}

func TestHookToEventType_StopNotSubscribed(t *testing.T) {
	if got := HookToEventType("Stop", nil); got != "" {
		t.Errorf("Stop should return empty (not subscribed), got %q", got)
	}
}
