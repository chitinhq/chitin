package router

import (
	"testing"
	"time"
)

func TestDetectStall_NoWritesLongSession(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-300 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]any{"decision": "allow", "action_type": "shell.exec"}},
	}
	stalled, reason := detectStall(events, 60, now)
	if !stalled {
		t.Error("expected stall with no writes in 300s session")
	}
	if reason != "no-writes-in-300s" {
		t.Errorf("reason = %q, want no-writes-in-300s", reason)
	}
}

func TestDetectStall_NoWritesShortSession(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-10 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]any{"decision": "allow", "action_type": "shell.exec"}},
	}
	stalled, _ := detectStall(events, 60, now)
	if stalled {
		t.Error("no stall expected with session shorter than maxStallSeconds")
	}
}

func TestDetectStall_EmptyEvents(t *testing.T) {
	stalled, reason := detectStall(nil, 60, time.Now())
	if stalled {
		t.Error("no stall expected with empty events")
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
}

func TestDetectStall_RecentWrite(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-10 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]any{"decision": "allow", "action_type": "file.write"}},
	}
	stalled, _ := detectStall(events, 60, now)
	if stalled {
		t.Error("no stall expected with recent write")
	}
}

func TestDetectStall_StaleWrite(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-200 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]any{"decision": "allow", "action_type": "file.write"}},
	}
	stalled, reason := detectStall(events, 60, now)
	if !stalled {
		t.Error("expected stall with stale write")
	}
	if reason != "no-writes-since-200s-ago" {
		t.Errorf("reason = %q, want no-writes-since-200s-ago", reason)
	}
}

func TestDetectStall_DenyEventsIgnored(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-300 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]any{"decision": "deny", "action_type": "file.write"}},
	}
	// Deny events don't count as writes, but the session has been going long
	stalled, _ := detectStall(events, 60, now)
	if !stalled {
		t.Error("expected stall — deny events don't count as writes")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q, want 'short'", got)
	}
	long := "this is a very long string that should be truncated"
	if got := truncate(long, 10); len(got) > 13 { // 10 + "…" (3 bytes)
		t.Errorf("truncate long result too long: %d chars", len(got))
	}
}

func TestStringField(t *testing.T) {
	m := map[string]interface{}{
		"present":  "value",
		"nonstr":  42,
		"spaced":  "  hello  ",
	}
	if got := stringField(m, "present"); got != "value" {
		t.Errorf("stringField(present) = %q, want 'value'", got)
	}
	if got := stringField(m, "missing"); got != "" {
		t.Errorf("stringField(missing) = %q, want empty", got)
	}
	if got := stringField(m, "nonstr"); got != "" {
		t.Errorf("stringField(nonstr) = %q, want empty", got)
	}
	if got := stringField(m, "spaced"); got != "hello" {
		t.Errorf("stringField(spaced) = %q, want 'hello'", got)
	}
}