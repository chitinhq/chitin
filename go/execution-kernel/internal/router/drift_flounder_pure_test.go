package router

import (
	"strings"
	"testing"
	"time"
)

// --- drift pure functions ---

func TestTruncate(t *testing.T) {
	// Short string — no truncation
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("truncate(short) = %q, want %q", got, "hello")
	}
	// Exactly at limit
	got = truncate("12345", 5)
	if got != "12345" {
		t.Errorf("truncate(exact) = %q, want %q", got, "12345")
	}
	// Over limit
	got = truncate("abcdefghijklmnopqrstuvwxyz", 10)
	if got != "abcdefghij…" {
		t.Errorf("truncate(long) = %q, want %q", got, "abcdefghij…")
	}
}

func TestPathOverlap_EmptyDeclaredElement(t *testing.T) {
	// PathOverlap with empty string in declared list
	if pathOverlap("apps/foo", []string{"", "apps/"}) {
		// This should match on "apps/" but empty should be skipped
	}
	// Single empty declared — should return false
	if pathOverlap("apps/foo", []string{""}) {
		t.Error("pathOverlap with only empty declared should return false")
	}
}

func TestTargetPathFromInput_BashPath(t *testing.T) {
	// Bash with command containing a path
	got := targetPathFromInput(HookInput{
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": "cat /etc/hosts"},
	})
	if got != "/etc/hosts" {
		t.Errorf("targetPathFromInput(Bash) = %q, want %q", got, "/etc/hosts")
	}
}

func TestTargetPathFromInput_NotebookPath(t *testing.T) {
	got := targetPathFromInput(HookInput{
		ToolName:  "NotebookEdit",
		ToolInput: map[string]interface{}{"notebook_path": "/path/to/notebook.ipynb"},
	})
	if got != "/path/to/notebook.ipynb" {
		t.Errorf("targetPathFromInput(NotebookEdit) = %q, want %q", got, "/path/to/notebook.ipynb")
	}
}

func TestTargetPathFromInput_NoPath(t *testing.T) {
	got := targetPathFromInput(HookInput{
		ToolName:  "Bash",
		ToolInput: map[string]interface{}{"command": "echo hello"},
	})
	if got != "" {
		t.Errorf("targetPathFromInput(Bash no path) = %q, want empty", got)
	}
}

func TestDetectDrift_NoTargetPath(t *testing.T) {
	intent := ChainEvent{
		EventType: "intent",
		Payload: map[string]interface{}{
			"entry_id":   "e1",
			"task_class": "fix",
			"file_paths": []interface{}{"src/main.go"},
		},
	}
	res := DetectDrift(
		HookInput{ToolName: "Bash", ToolInput: map[string]interface{}{"command": "echo hi"}},
		[]ChainEvent{intent},
		0.5,
	)
	if res.Fired {
		t.Errorf("Fired=true with no target path; want false (reason=%q)", res.Reason)
	}
	if res.Reason != "no-target-path" {
		t.Errorf("reason=%q, want no-target-path", res.Reason)
	}
}

// --- floundering pure functions ---

func TestDetectLoop_ShorterThanMax(t *testing.T) {
	// Fewer events than maxLoopCount → false
	ev := func(target string) ChainEvent {
		return ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"tool_name":     "Bash",
				"action_target": target,
			},
		}
	}
	detected, reason := detectLoop([]ChainEvent{ev("x"), ev("x")}, 5)
	if detected {
		t.Errorf("detectLoop(short events) = true; want false (reason=%q)", reason)
	}
}

func TestDetectLoop_NoTarget(t *testing.T) {
	// Events with tool_name but empty target → should not trigger
	events := make([]ChainEvent, 5)
	for i := range events {
		events[i] = ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"tool_name":     "Bash",
				"action_target": "",
			},
		}
	}
	detected, _ := detectLoop(events, 3)
	if detected {
		t.Error("detectLoop(empty target) = true; want false")
	}
}

func TestDetectLoop_NonDecisionEvents(t *testing.T) {
	// Non-decision events should be filtered out
	events := make([]ChainEvent, 5)
	for i := range events {
		events[i] = ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "other",
			Payload:   map[string]interface{}{},
		}
	}
	detected, _ := detectLoop(events, 3)
	if detected {
		t.Error("detectLoop(non-decision events) = true; want false")
	}
}

func TestDetectStall_RecentWrite(t *testing.T) {
	// Write event happened recently → no stall
	events := []ChainEvent{
		{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":    "allow",
				"action_type": "file.write",
			},
		},
	}
	now := mustParse("2026-05-03T20:00:30Z")
	detected, _ := detectStall(events, 600, now)
	if detected {
		t.Error("detectStall(recent write) = true; want false")
	}
}

func TestDetectStall_StaleWrite(t *testing.T) {
	// Write event was a long time ago → stall detected
	events := []ChainEvent{
		{
			Ts:        "2026-05-03T19:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":    "allow",
				"action_type": "file.write",
			},
		},
	}
	now := mustParse("2026-05-03T20:11:00Z")
	detected, reason := detectStall(events, 600, now)
	if !detected {
		t.Error("detectStall(stale write) = false; want true")
	}
	if !strings.HasPrefix(reason, "no-writes-since-") {
		t.Errorf("reason=%q, want prefix no-writes-since-", reason)
	}
}

func TestDetectStall_NoWritesSessionTooShort(t *testing.T) {
	// No writes, but session hasn't been going long enough → no stall
	events := []ChainEvent{
		{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":    "allow",
				"action_type": "file.read",
			},
		},
	}
	now := mustParse("2026-05-03T20:01:00Z")
	detected, _ := detectStall(events, 600, now)
	if detected {
		t.Error("detectStall(session too short) = true; want false")
	}
}

func TestDetectStall_InvalidTimestamp(t *testing.T) {
	// Invalid timestamp → no stall
	events := []ChainEvent{
		{
			Ts:        "not-a-timestamp",
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":    "allow",
				"action_type": "file.read",
			},
		},
	}
	now := mustParse("2026-05-03T21:00:00Z")
	detected, _ := detectStall(events, 600, now)
	if detected {
		t.Error("detectStall(invalid ts) = true; want false")
	}
}

func TestDetectStall_WriteWithInvalidTimestamp(t *testing.T) {
	// Write event with invalid timestamp → no stall
	events := []ChainEvent{
		{
			Ts:        "2026-05-03T19:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":    "allow",
				"action_type": "file.read",
			},
		},
		{
			Ts:        "invalid-ts",
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":    "allow",
				"action_type": "file.write",
			},
		},
	}
	now := mustParse("2026-05-03T21:00:00Z")
	detected, _ := detectStall(events, 600, now)
	if detected {
		t.Error("detectStall(write with invalid ts) = true; want false")
	}
}

func TestDetectStall_EmptyEvents(t *testing.T) {
	detected, _ := detectStall(nil, 600, time.Now())
	if detected {
		t.Error("detectStall(empty) = true; want false")
	}
}

func TestDetectDenialCascade_Boundary(t *testing.T) {
	// Exactly 4 out of 5 denied → cascade
	denial := func(id string) ChainEvent {
		return ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":      "deny",
				"tool_name":     "Bash",
				"action_target": id,
			},
		}
	}
	allow := ChainEvent{
		Ts:        "2026-05-03T20:00:00Z",
		EventType: "decision",
		Payload: map[string]interface{}{
			"decision":      "allow",
			"tool_name":     "Read",
			"action_target": "/tmp/readme",
		},
	}
	events := []ChainEvent{allow, denial("a"), denial("b"), denial("c"), denial("d")}
	detected, reason := detectDenialCascade(events)
	if !detected {
		t.Error("detectDenialCascade(4/5) = false; want true")
	}
	if reason[:10] != "denial-cas" {
		t.Errorf("reason=%q, want denial-cascade prefix", reason)
	}
}

func TestDetectDenialCascade_Only3(t *testing.T) {
	// Only 3 out of 5 denied → no cascade
	denial := func(id string) ChainEvent {
		return ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload: map[string]interface{}{
				"decision":      "deny",
				"tool_name":     "Bash",
				"action_target": id,
			},
		}
	}
	allow := ChainEvent{
		Ts:        "2026-05-03T20:00:00Z",
		EventType: "decision",
		Payload: map[string]interface{}{
			"decision":      "allow",
			"tool_name":     "Read",
			"action_target": "/tmp/readme",
		},
	}
	events := []ChainEvent{allow, allow, denial("a"), denial("b"), denial("c")}
	detected, _ := detectDenialCascade(events)
	if detected {
		t.Error("detectDenialCascade(3/5) = true; want false")
	}
}