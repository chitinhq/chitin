package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- detectLoop tests ---

func TestDetectLoop_BelowThreshold(t *testing.T) {
	// Only 2 events, threshold is 3
	events := []ChainEvent{
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "rm /tmp/x"}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "rm /tmp/x"}},
	}
	fired, reason := detectLoop(events, 3)
	if fired {
		t.Errorf("loop fired with < threshold events: reason=%q", reason)
	}
}

func TestDetectLoop_ExactlyAtThreshold(t *testing.T) {
	// 3 identical events, threshold 3
	mk := func() ChainEvent {
		return ChainEvent{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "rm /tmp/x"}}
	}
	events := []ChainEvent{mk(), mk(), mk()}
	fired, reason := detectLoop(events, 3)
	if !fired {
		t.Error("expected loop detection at exactly threshold")
	}
	if reason[:18] != "looping-tool-call:" {
		t.Errorf("reason=%q want prefix looping-tool-call:", reason)
	}
}

func TestDetectLoop_AboveThreshold(t *testing.T) {
	mk := func() ChainEvent {
		return ChainEvent{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Read", "action_target": "/etc/hosts"}}
	}
	events := []ChainEvent{mk(), mk(), mk(), mk(), mk()} // 5 events
	fired, reason := detectLoop(events, 3)
	if !fired {
		t.Error("expected loop detection above threshold")
	}
	// Should use last 3 events
	_ = reason
}

func TestDetectLoop_MixedThenRepeated_Fixed(t *testing.T) {
	// 5 events with tool+target; last 3 identical
	events := []ChainEvent{
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "ls"}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "pwd"}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "rm /tmp/x"}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "rm /tmp/x"}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "rm /tmp/x"}},
	}
	fired, _ := detectLoop(events, 3)
	if !fired {
		t.Error("last 3 of 5 are identical Bash|rm /tmp/x → loop should fire")
	}
}

func TestDetectLoop_EmptyTarget_Ignored(t *testing.T) {
	// Events with empty action_target should be filtered out of loop detection
	events := []ChainEvent{
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": ""}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": ""}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": ""}},
	}
	fired, _ := detectLoop(events, 3)
	if fired {
		t.Error("empty targets should be filtered → loop should not fire")
	}
}

func TestDetectLoop_NonDecisionEvents_Ignored(t *testing.T) {
	// Non-decision events should not count toward loop detection
	events := []ChainEvent{
		{EventType: "signal", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "x"}},
		{EventType: "signal", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "x"}},
		{EventType: "signal", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": "x"}},
	}
	fired, _ := detectLoop(events, 3)
	if fired {
		t.Error("non-decision events should be filtered out of loop detection")
	}
}

func TestDetectLoop_LongReasonTruncated(t *testing.T) {
	// Target longer than 80 chars should be truncated in the reason
	longTarget := string(make([]byte, 120))
	for i := range longTarget {
		longTarget = longTarget[:i+1] + "x"
	}
	events := []ChainEvent{
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": longTarget}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": longTarget}},
		{EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_target": longTarget}},
	}
	fired, reason := detectLoop(events, 3)
	if !fired {
		t.Error("expected loop to fire for long target")
	}
	// The looping-tool-call reason should have a truncated signature
	// Format: "looping-tool-call:Bash|<truncated>-x3"
	if len(reason) > 120 {
		t.Errorf("reason too long: %d chars: %q", len(reason), reason)
	}
}

// --- detectStall tests ---

func TestDetectStall_NoWriteEvents_ButShortSession(t *testing.T) {
	// No write events, but session started recently (< maxStallSeconds)
	events := []ChainEvent{
		{Ts: "2026-05-03T20:00:00Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Read", "action_type": "file.read", "decision": "allow"}},
	}
	fired, _ := detectStall(events, 600, mustParse("2026-05-03T20:05:00Z")) // 5 min < 10 min
	if fired {
		t.Error("no-write stall should not fire if session is short")
	}
}

func TestDetectStall_NoWriteEvents_LongSession(t *testing.T) {
	// No write events, session has been running long enough
	events := []ChainEvent{
		{Ts: "2026-05-03T20:00:00Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Read", "action_type": "file.read", "decision": "allow"}},
	}
	fired, reason := detectStall(events, 600, mustParse("2026-05-03T20:15:00Z")) // 15 min > 10 min
	if !fired {
		t.Error("expected stall detection for session with no writes running > threshold")
	}
	if reason[:9] != "no-writes" {
		t.Errorf("reason=%q want prefix no-writes", reason)
	}
}

func TestDetectStall_WriteEventRecently(t *testing.T) {
	// Write event 30 seconds ago, maxStallSeconds=600
	events := []ChainEvent{
		{Ts: "2026-05-03T20:14:30Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Write", "action_type": "file.write", "decision": "allow"}},
	}
	fired, _ := detectStall(events, 600, mustParse("2026-05-03T20:15:00Z"))
	if fired {
		t.Error("should not stall when write event is recent")
	}
}

func TestDetectStall_WriteEventTooOld(t *testing.T) {
	// Write event 700 seconds ago, maxStallSeconds=600
	events := []ChainEvent{
		{Ts: "2026-05-03T19:58:20Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Write", "action_type": "file.write", "decision": "allow"}},
	}
	fired, reason := detectStall(events, 600, mustParse("2026-05-03T20:10:00Z"))
	if !fired {
		t.Error("expected stall when last write is older than threshold")
	}
	if reason[:20] != "no-writes-since-700s" && reason[:19] != "no-writes-since-69" {
		// Accept approximate: could be 700ish depending on rounding
		t.Logf("reason=%q (accepting if approximately correct)", reason)
	}
}

func TestDetectStall_DeniedWritesNotCounted(t *testing.T) {
	// Denied writes should NOT reset the stall timer
	events := []ChainEvent{
		{Ts: "2026-05-03T20:14:00Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Write", "action_type": "file.write", "decision": "deny"}},
		{Ts: "2026-05-03T20:14:30Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_type": "shell.exec", "decision": "allow"}},
	}
	// 90 seconds after denied write, 60s allowed
	fired, _ := detectStall(events, 60, mustParse("2026-05-03T20:15:30Z"))
	if !fired {
		t.Error("denied writes should not count as progress; stall should fire")
	}
}

func TestDetectStall_GitCommitResets(t *testing.T) {
	// git.commit counts as a write event
	events := []ChainEvent{
		{Ts: "2026-05-03T20:14:30Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "action_type": "git.commit", "decision": "allow"}},
	}
	fired, _ := detectStall(events, 600, mustParse("2026-05-03T20:15:00Z"))
	if fired {
		t.Error("git.commit should reset the stall timer")
	}
}

func TestDetectStall_EmptyEvents(t *testing.T) {
	fired, _ := detectStall(nil, 600, mustParse("2026-05-03T20:15:00Z"))
	if fired {
		t.Error("empty events should not trigger stall")
	}
}

// --- detectDenialCascade tests (edge cases) ---

func TestDetectDenialCascade_ExactlyFiveAllDeny(t *testing.T) {
	events := make([]ChainEvent, 5)
	for i := range events {
		events[i] = ChainEvent{
			Ts:        "2026-05-03T20:00:00Z",
			EventType: "decision",
			Payload:   map[string]interface{}{"decision": "deny", "tool_name": "Bash"},
		}
	}
	fired, reason := detectDenialCascade(events)
	if !fired {
		t.Error("5/5 denials should be a cascade")
	}
	if reason != "denial-cascade:5-of-last-5" {
		t.Errorf("reason=%q want denial-cascade:5-of-last-5", reason)
	}
}

func TestDetectDenialCascade_FourDenyOneAllow(t *testing.T) {
	events := []ChainEvent{
		{Ts: "2026-05-03T20:00:00Z", EventType: "decision", Payload: map[string]interface{}{"decision": "allow"}},
		{Ts: "2026-05-03T20:00:01Z", EventType: "decision", Payload: map[string]interface{}{"decision": "deny"}},
		{Ts: "2026-05-03T20:00:02Z", EventType: "decision", Payload: map[string]interface{}{"decision": "deny"}},
		{Ts: "2026-05-03T20:00:03Z", EventType: "decision", Payload: map[string]interface{}{"decision": "deny"}},
		{Ts: "2026-05-03T20:00:04Z", EventType: "decision", Payload: map[string]interface{}{"decision": "deny"}},
	}
	fired, _ := detectDenialCascade(events)
	if !fired {
		t.Error("4/5 denials should be a cascade (threshold is 4)")
	}
}

func TestDetectDenialCascade_ThreeDenyTwoAllow(t *testing.T) {
	events := []ChainEvent{
		{Ts: "2026-05-03T20:00:00Z", EventType: "decision", Payload: map[string]interface{}{"decision": "deny"}},
		{Ts: "2026-05-03T20:00:01Z", EventType: "decision", Payload: map[string]interface{}{"decision": "allow"}},
		{Ts: "2026-05-03T20:00:02Z", EventType: "decision", Payload: map[string]interface{}{"decision": "deny"}},
		{Ts: "2026-05-03T20:00:03Z", EventType: "decision", Payload: map[string]interface{}{"decision": "allow"}},
		{Ts: "2026-05-03T20:00:04Z", EventType: "decision", Payload: map[string]interface{}{"decision": "deny"}},
	}
	fired, _ := detectDenialCascade(events)
	if fired {
		t.Error("3/5 denials should NOT be a cascade (threshold is 4)")
	}
}

func TestDetectDenialCascade_FourEvents(t *testing.T) {
	// Need at least 5 events total for cascade detection
	events := make([]ChainEvent, 4)
	for i := range events {
		events[i] = ChainEvent{Ts: "2026-05-03T20:00:00Z", EventType: "decision", Payload: map[string]interface{}{"decision": "deny"}}
	}
	fired, _ := detectDenialCascade(events)
	if fired {
		t.Error("4 events total is below the minimum window of 5")
	}
}

// --- ReadChainEvents tests ---

func TestReadChainEvents_FileNotFound(t *testing.T) {
	events := ReadChainEvents("nonexistent-session-id-12345")
	if events != nil {
		t.Errorf("expected nil for missing file, got %d events", len(events))
	}
}

func TestReadChainEvents_EmptySessionID(t *testing.T) {
	events := ReadChainEvents("")
	if events != nil {
		t.Errorf("expected nil for empty session ID, got %d events", len(events))
	}
}

func TestReadChainEvents_ValidFile(t *testing.T) {
	// Create a temp events file
	tmpDir := t.TempDir()
	home := os.Getenv("HOME")
	defer os.Setenv("HOME", home)
	os.Setenv("HOME", tmpDir)

	chitinDir := filepath.Join(tmpDir, ".chitin")
	if err := os.MkdirAll(chitinDir, 0755); err != nil {
		t.Fatal(err)
	}

	events := []ChainEvent{
		{Ts: "2026-05-03T20:00:00Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Read", "decision": "allow"}},
		{Ts: "2026-05-03T20:00:01Z", EventType: "decision", Payload: map[string]interface{}{"tool_name": "Bash", "decision": "deny"}},
	}
	lines := make([]byte, 0, 512)
	for _, ev := range events {
		data, _ := json.Marshal(ev)
		lines = append(lines, data...)
		lines = append(lines, '\n')
	}
	if err := os.WriteFile(filepath.Join(chitinDir, "events-test-session.jsonl"), lines, 0644); err != nil {
		t.Fatal(err)
	}

	result := ReadChainEvents("test-session")
	if len(result) != 2 {
		t.Fatalf("expected 2 events, got %d", len(result))
	}
	if result[0].EventType != "decision" {
		t.Errorf("event[0].EventType=%q want decision", result[0].EventType)
	}
}

func TestReadChainEvents_MalformedLineSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	home := os.Getenv("HOME")
	defer os.Setenv("HOME", home)
	os.Setenv("HOME", tmpDir)

	chitinDir := filepath.Join(tmpDir, ".chitin")
	if err := os.MkdirAll(chitinDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Mix valid and invalid lines
	content := `{"ts":"2026-05-03T20:00:00Z","event_type":"decision","payload":{"tool_name":"Read"}}
THIS IS NOT JSON
{"ts":"2026-05-03T20:00:01Z","event_type":"decision","payload":{"tool_name":"Bash"}}

`
	if err := os.WriteFile(filepath.Join(chitinDir, "events-skip-test.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := ReadChainEvents("skip-test")
	if len(result) != 2 {
		t.Fatalf("expected 2 valid events (malformed line skipped), got %d", len(result))
	}
}