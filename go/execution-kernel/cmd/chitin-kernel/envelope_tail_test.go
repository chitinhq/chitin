package main

import (
	"bytes"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func TestProcessLine_Empty(t *testing.T) {
	state := &tailState{denials: map[string]int64{}}
	var buf bytes.Buffer
	processLine("", "", state, &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty line, got %q", buf.String())
	}
}

func TestProcessLine_Allowed(t *testing.T) {
	state := &tailState{denials: map[string]int64{}}
	var buf bytes.Buffer
	line := `{"allowed":true,"mode":"strict","rule_id":"r1","reason":"","agent":"claude-code","action_type":"file.read","action_target":"/tmp/test","ts":"2026-05-09T12:00:00Z","envelope_id":"env1","tier":"T0","cost_usd":0.01}`
	processLine(line, "", state, &buf)
	output := buf.String()
	if output == "" {
		t.Error("expected output for allowed line")
	}
	if !bytes.Contains(buf.Bytes(), []byte("ALLOW")) {
		t.Errorf("expected ALLOW in output, got %q", output)
	}
}

func TestProcessLine_Denied(t *testing.T) {
	state := &tailState{denials: map[string]int64{}}
	var buf bytes.Buffer
	line := `{"allowed":false,"mode":"strict","rule_id":"r1","reason":"dangerous","agent":"claude-code","action_type":"bash","action_target":"rm -rf /","ts":"2026-05-09T12:00:00Z","envelope_id":"env1","tier":"T0","cost_usd":0}`
	processLine(line, "", state, &buf)
	if state.denials["env1"] != 1 {
		t.Errorf("expected 1 denial for env1, got %d", state.denials["env1"])
	}
	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("DENY")) {
		t.Errorf("expected DENY in output, got %q", output)
	}
}

func TestProcessLine_FilterMatch(t *testing.T) {
	state := &tailState{denials: map[string]int64{}}
	var buf bytes.Buffer
	line := `{"allowed":true,"mode":"strict","rule_id":"r1","agent":"claude-code","action_type":"file.read","action_target":"/tmp/test","ts":"2026-05-09T12:00:00Z","envelope_id":"env1","tier":"T0"}`
	processLine(line, "env1", state, &buf)
	if buf.Len() == 0 {
		t.Error("expected output when filter matches")
	}
}

func TestProcessLine_FilterMismatch(t *testing.T) {
	state := &tailState{denials: map[string]int64{}}
	var buf bytes.Buffer
	line := `{"allowed":true,"mode":"strict","rule_id":"r1","agent":"claude-code","action_type":"file.read","action_target":"/tmp/test","ts":"2026-05-09T12:00:00Z","envelope_id":"env1","tier":"T0"}`
	processLine(line, "env-other", state, &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no output when filter doesn't match, got %q", buf.String())
	}
}

func TestProcessLine_InvalidJSON(t *testing.T) {
	state := &tailState{denials: map[string]int64{}}
	var buf bytes.Buffer
	// processLine writes parse errors to stderr — we just verify it doesn't crash
	processLine("not-json", "", state, &buf)
	if buf.Len() != 0 {
		t.Errorf("expected no stdout for invalid JSON, got %q", buf.String())
	}
}

func TestFormatRow(t *testing.T) {
	r := auditRow{
		Allowed:      true,
		Mode:         "strict",
		RuleID:       "r1",
		Agent:        "claude-code",
		ActionType:   "file.read",
		ActionTarget: "/tmp/test.go",
		Ts:           "2026-05-09T12:00:00Z",
		EnvelopeID:   "env1",
		Tier:         gov.Tier("T0"),
		CostUSD:      0.01,
	}
	got := formatRow(r)
	if got == "" {
		t.Error("formatRow returned empty string")
	}
	// Should contain ALLOW, agent, action type
	if !bytes.Contains([]byte(got), []byte("ALLOW")) {
		t.Errorf("formatRow should contain ALLOW, got %q", got)
	}
}

func TestFormatRow_Deny(t *testing.T) {
	r := auditRow{
		Allowed:      false,
		Agent:        "copilot",
		ActionType:   "bash",
		ActionTarget: "rm -rf /",
		Ts:           "2026-05-09T12:00:00Z",
		Tier:         gov.Tier("T0"),
	}
	got := formatRow(r)
	if !bytes.Contains([]byte(got), []byte("DENY")) {
		t.Errorf("formatRow for denied should contain DENY, got %q", got)
	}
}

func TestFormatRow_LongTarget(t *testing.T) {
	longPath := "/very/long/path/that/exceeds/sixty/characters/and/should/be/truncated/in/the/output"
	r := auditRow{
		Allowed:      true,
		Agent:        "claude-code",
		ActionType:   "file.read",
		ActionTarget: longPath,
		Ts:           "2026-05-09T12:00:00Z",
		Tier:         gov.Tier("T0"),
	}
	got := formatRow(r)
	if !bytes.Contains([]byte(got), []byte("...")) {
		t.Errorf("long target should be truncated with ..., got %q", got)
	}
}

func TestFormatRow_MissingAgent(t *testing.T) {
	r := auditRow{
		Allowed:    true,
		Agent:      "",
		ActionType: "file.read",
		Ts:         "2026-05-09T12:00:00Z",
		Tier:       gov.Tier("T0"),
	}
	got := formatRow(r)
	if !bytes.Contains([]byte(got), []byte("-")) {
		t.Errorf("missing agent should show -, got %q", got)
	}
}