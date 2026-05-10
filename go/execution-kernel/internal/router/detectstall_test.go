package router

import (
	"testing"
	"time"
)

func TestDetectStall_NoWritesLongSession(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-200 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]interface{}{"decision": "allow", "action_type": "file.read"}},
		{Ts: now.Add(-100 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]interface{}{"decision": "allow", "action_type": "file.read"}},
	}
	stalled, reason := detectStall(events, 60, now)
	if !stalled {
		t.Error("expected stall for long session with no writes")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestDetectStall_NoWritesShortSession(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-10 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]interface{}{"decision": "allow", "action_type": "file.read"}},
	}
	stalled, _ := detectStall(events, 60, now)
	if stalled {
		t.Error("expected no stall for short session with no writes")
	}
}

func TestDetectStall_RecentWrites(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-10 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]interface{}{"decision": "allow", "action_type": "file.write"}},
	}
	stalled, _ := detectStall(events, 60, now)
	if stalled {
		t.Error("expected no stall when writes are recent")
	}
}

func TestDetectStall_StaleLastWrite(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-120 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]interface{}{"decision": "allow", "action_type": "file.write"}},
	}
	stalled, reason := detectStall(events, 60, now)
	if !stalled {
		t.Error("expected stall when last write is old")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestDetectStall_GitCommitCount(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-5 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]interface{}{"decision": "allow", "action_type": "git.commit"}},
	}
	stalled, _ := detectStall(events, 60, now)
	if stalled {
		t.Error("git.commit should count as a write — no stall expected")
	}
}

func TestDetectStall_GitPushCount(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-5 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]interface{}{"decision": "allow", "action_type": "git.push"}},
	}
	stalled, _ := detectStall(events, 60, now)
	if stalled {
		t.Error("git.push should count as a write — no stall expected")
	}
}

func TestDetectStall_DeniedWriteNotCounted(t *testing.T) {
	now := time.Now()
	events := []ChainEvent{
		{Ts: now.Add(-200 * time.Second).Format(time.RFC3339), EventType: "decision", Payload: map[string]interface{}{"decision": "deny", "action_type": "file.write"}},
	}
	stalled, _ := detectStall(events, 60, now)
	// Denied writes should not count as writes. With only a deny and a long wait, stall.
	// But events has only one event, and that event is denied, so it's filtered out.
	// Then we have 0 write events, so we check first event timestamp.
	if !stalled {
		t.Error("denied write should not prevent stall detection")
	}
}

func TestDetectStall_InvalidTimestamp(t *testing.T) {
	events := []ChainEvent{
		{Ts: "not-a-timestamp", EventType: "decision", Payload: map[string]interface{}{"decision": "allow", "action_type": "file.write"}},
	}
	stalled, _ := detectStall(events, 60, time.Now())
	if stalled {
		t.Error("invalid timestamp should not trigger stall")
	}
}

func TestDetectStall_EmptyEvents(t *testing.T) {
	stalled, _ := detectStall(nil, 60, time.Now())
	if stalled {
		t.Error("empty events should not trigger stall")
	}
}