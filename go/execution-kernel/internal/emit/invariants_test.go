package emit

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

// minimalEvent builds a valid v2 event of the given type / payload on
// the given chain, with the given timestamp. Strict-enough for the
// kernel emit path; mirrors minimalSessionStart's invariant fields.
func minimalEvent(chainID, eventType string, seq int64, ts string, payload string) *event.Event {
	return &event.Event{
		SchemaVersion:    "2",
		RunID:            "550e8400-e29b-41d4-a716-446655440000",
		SessionID:        "550e8400-e29b-41d4-a716-446655440001",
		Surface:          "claude-code",
		DriverIdentity:   event.DriverIdentity{User: "u", MachineID: "m", MachineFingerprint: "a" + repeat("0", 63)},
		AgentInstanceID:  "550e8400-e29b-41d4-a716-446655440002",
		AgentFingerprint: "b" + repeat("0", 63),
		EventType:        eventType,
		ChainID:          chainID,
		ChainType:        "session",
		Seq:              seq,
		Ts:               ts,
		Labels:           map[string]string{},
		Payload:          json.RawMessage(payload),
	}
}

// TestEmit_SessionEndIsLast_Refuses asserts the #3 invariant: once a
// chain has session_end as its tail, any non-session_end emit on that
// chain returns ErrSessionEnded.
func TestEmit_SessionEndIsLast_Refuses(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}

	// Seed: session_start, then session_end.
	if err := e.Emit(minimalSessionStart("chainA", 0)); err != nil {
		t.Fatal(err)
	}
	if err := e.Emit(minimalEvent("chainA", "session_end", 0,
		"2026-05-02T12:00:00.000Z", `{"reason":"normal"}`)); err != nil {
		t.Fatalf("seed session_end: %v", err)
	}

	// Now try to emit a pre_tool_use after session_end. Must fail.
	bad := minimalEvent("chainA", "pre_tool_use", 0,
		"2026-05-02T12:00:01.000Z", `{"tool_name":"Bash"}`)
	err := e.Emit(bad)
	if err == nil {
		t.Fatalf("expected ErrSessionEnded, got nil — chain corrupted")
	}
	if !errors.Is(err, ErrSessionEnded) {
		t.Errorf("expected ErrSessionEnded wrap, got %v", err)
	}
	if !strings.Contains(err.Error(), "chainA") {
		t.Errorf("error should identify the chain, got: %v", err)
	}
}

// TestEmit_SessionEndIsLast_AllowsSecondSessionEnd asserts the carve-out:
// session_end after session_end is allowed (re-emit / late observation).
// Ensures the invariant doesn't break legitimate idempotent close paths.
func TestEmit_SessionEndIsLast_AllowsSecondSessionEnd(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}

	if err := e.Emit(minimalSessionStart("chainA", 0)); err != nil {
		t.Fatal(err)
	}
	if err := e.Emit(minimalEvent("chainA", "session_end", 0,
		"2026-05-02T12:00:00.000Z", `{"reason":"normal"}`)); err != nil {
		t.Fatal(err)
	}
	// Same logical content within the dedup window → idempotent skip
	// (next test covers that). Use distinct content + future ts so this
	// is a genuinely different session_end re-emit.
	second := minimalEvent("chainA", "session_end", 0,
		time.Now().UTC().Add(IdempotentDedupWindow+1*time.Second).Format(time.RFC3339Nano),
		`{"reason":"late_observation"}`)
	if err := e.Emit(second); err != nil {
		t.Errorf("second session_end must be allowed (carve-out): %v", err)
	}
}

// TestEmit_IdempotentDedup_SkipsRetryWithinWindow asserts the #16
// invariant: a re-emit of identical logical content within the dedup
// window is treated as a duplicate. The chain index must NOT advance
// and the JSONL must NOT grow a duplicate line.
func TestEmit_IdempotentDedup_SkipsRetryWithinWindow(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := e.Emit(minimalEvent("chainA", "session_start", 0, now,
		`{"cwd":"/","client_info":{"name":"x","version":"1"},"model":{"name":"m","provider":"p"},"system_prompt_hash":"`+repeat("0", 64)+`","tool_allowlist_hash":"`+repeat("0", 64)+`","agent_version":"1"}`)); err != nil {
		t.Fatal(err)
	}

	infoBefore, _ := idx.Get("chainA")
	linesBefore := readLines(t, e.LogPath)

	// Retry the SAME logical event with a slightly later timestamp.
	// The retry's logical_hash matches the chain tail's, so dedup fires.
	retry := minimalEvent("chainA", "session_start", 0,
		time.Now().UTC().Add(1*time.Second).Format(time.RFC3339Nano),
		`{"cwd":"/","client_info":{"name":"x","version":"1"},"model":{"name":"m","provider":"p"},"system_prompt_hash":"`+repeat("0", 64)+`","tool_allowlist_hash":"`+repeat("0", 64)+`","agent_version":"1"}`)
	if err := e.Emit(retry); err != nil {
		t.Fatalf("retry should succeed (no-op), got: %v", err)
	}

	infoAfter, _ := idx.Get("chainA")
	linesAfter := readLines(t, e.LogPath)

	if infoBefore.LastSeq != infoAfter.LastSeq {
		t.Errorf("LastSeq advanced on retry: before=%d after=%d (idempotent dedup failed)",
			infoBefore.LastSeq, infoAfter.LastSeq)
	}
	if len(linesBefore) != len(linesAfter) {
		t.Errorf("JSONL grew on retry: before=%d after=%d lines (duplicate written)",
			len(linesBefore), len(linesAfter))
	}
	if retry.ThisHash != infoBefore.LastHash {
		t.Errorf("retry ThisHash should match chain tail: got %q want %q",
			retry.ThisHash, infoBefore.LastHash)
	}
}

// TestEmit_IdempotentDedup_DistinctContentStillEmits asserts that
// content-different events on the same chain DO emit. Otherwise the
// dedup logic would silently swallow legitimate distinct events with
// happen-to-be-similar payloads.
func TestEmit_IdempotentDedup_DistinctContentStillEmits(t *testing.T) {
	dir, idx := newEnv(t)
	e := Emitter{LogPath: filepath.Join(dir, "events.jsonl"), Index: idx}

	if err := e.Emit(minimalSessionStart("chainA", 0)); err != nil {
		t.Fatal(err)
	}
	infoAfterFirst, _ := idx.Get("chainA")

	// Distinct event_type AND distinct payload — different logical hash.
	if err := e.Emit(minimalEvent("chainA", "pre_tool_use", 0,
		time.Now().UTC().Format(time.RFC3339Nano),
		`{"tool_name":"Bash","tool_input":{"command":"echo hi"}}`)); err != nil {
		t.Fatal(err)
	}
	infoAfterSecond, _ := idx.Get("chainA")

	if infoAfterSecond.LastSeq != infoAfterFirst.LastSeq+1 {
		t.Errorf("distinct event must advance LastSeq: before=%d after=%d",
			infoAfterFirst.LastSeq, infoAfterSecond.LastSeq)
	}
}
