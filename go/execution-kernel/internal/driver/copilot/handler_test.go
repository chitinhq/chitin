package copilot

import (
	"testing"
)

func TestLockdownError_Error(t *testing.T) {
	lde := &LockdownError{Agent: "glm-agent", Count: 3}
	msg := lde.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if !containsStr(msg, "glm-agent") {
		t.Errorf("expected agent name in message, got: %s", msg)
	}
	if !containsStr(msg, "3") {
		t.Errorf("expected denial count in message, got: %s", msg)
	}
	if !containsStr(msg, "chitin-lockdown") {
		t.Errorf("expected 'chitin-lockdown' prefix in message, got: %s", msg)
	}
}

func TestLockdownError_ZeroCount(t *testing.T) {
	lde := &LockdownError{Agent: "copilot", Count: 0}
	msg := lde.Error()
	if !containsStr(msg, "copilot") {
		t.Errorf("expected agent name in message, got: %s", msg)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}