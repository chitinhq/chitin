package router

import (
	"testing"
	"time"
)

func TestDetectStall_NoEvents(t *testing.T) {
	stalled, reason := detectStall(nil, 60, time.Now())
	if stalled {
		t.Errorf("expected no stall with no events, got reason: %s", reason)
	}
}

func TestDetectStall_RecentWrite(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{
			EventType: "decision",
			Ts:        now.Add(-5 * time.Second).Format(time.RFC3339),
			Payload:   map[string]any{"decision": "allow", "action_type": "file.write"},
		},
	}
	stalled, _ := detectStall(events, 60, now)
	if stalled {
		t.Error("expected no stall with recent write")
	}
}

func TestDetectStall_OldWrite(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{
			EventType: "decision",
			Ts:        now.Add(-120 * time.Second).Format(time.RFC3339),
			Payload:   map[string]any{"decision": "allow", "action_type": "file.write"},
		},
	}
	stalled, reason := detectStall(events, 60, now)
	if !stalled {
		t.Error("expected stall with old write")
	}
	if reason == "" {
		t.Error("expected non-empty stall reason")
	}
}

func TestDetectStall_NoWrites_LongSession(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{
			EventType: "decision",
			Ts:        now.Add(-120 * time.Second).Format(time.RFC3339),
			Payload:   map[string]any{"decision": "allow", "action_type": "shell.exec"},
		},
	}
	stalled, reason := detectStall(events, 60, now)
	if !stalled {
		t.Error("expected stall with long session and no writes")
	}
	if reason == "" {
		t.Error("expected non-empty stall reason")
	}
}

func TestDetectStall_DenyDoesNotCountAsWrite(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{
			EventType: "decision",
			Ts:        now.Add(-120 * time.Second).Format(time.RFC3339),
			Payload:   map[string]any{"decision": "deny", "action_type": "file.write"},
		},
	}
	stalled, _ := detectStall(events, 60, now)
	if !stalled {
		t.Error("denied writes should not count, so long session should stall")
	}
}

func TestDetectStall_NoWrites_ShortSession(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{
			EventType: "decision",
			Ts:        now.Add(-5 * time.Second).Format(time.RFC3339),
			Payload:   map[string]any{"decision": "allow", "action_type": "shell.exec"},
		},
	}
	stalled, _ := detectStall(events, 60, now)
	if stalled {
		t.Error("short session with no writes should not stall")
	}
}