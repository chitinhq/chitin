package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
)

// hermesEnvelopeTemplate is a known-valid template for emitting model_turn
// events through the hermes translator. Shape mirrors openclawEnvelopeTemplate
// so only the dialect-specific labels differ at emit time.
func hermesEnvelopeTemplate() *event.Event {
	return &event.Event{
		SchemaVersion:    "2",
		RunID:            "550e8400-e29b-41d4-a716-446655442000",
		SessionID:        "550e8400-e29b-41d4-a716-446655442001",
		Surface:          "hermes-cli",
		AgentInstanceID:  "550e8400-e29b-41d4-a716-446655442002",
		AgentFingerprint: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ChainID:          "placeholder-will-be-overridden",
		ChainType:        "session",
		Labels:           map[string]string{},
		DriverIdentity: event.DriverIdentity{
			User:               "red",
			MachineID:          "chimera-ant",
			MachineFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}
}

// realCaptureBytes returns the Phase B real capture bytes, checked in at
// docs/observations/2026-04-21-hermes-post-api-request-capture.jsonl.
func realCaptureBytes(t *testing.T) []byte {
	t.Helper()
	// Walk up from internal/ingest/ to repo root to locate the observation file.
	path := filepath.Join("..", "..", "..", "..", "docs", "observations",
		"2026-04-21-hermes-post-api-request-capture.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read real capture: %v", err)
	}
	return data
}

func TestEmitHermesTurns_RealCaptureEndToEnd(t *testing.T) {
	em, logPath := newTestEmitter(t)
	dir := filepath.Dir(logPath)
	raw := realCaptureBytes(t)

	turns, quarantined, err := ParseHermesEvents(raw)
	if err != nil {
		t.Fatalf("ParseHermesEvents: %v", err)
	}

	// The Phase B capture has 22 post_api_request events and 59 supporting
	// events (on_session_start/end, pre_tool_call, post_tool_call). See
	// docs/observations/2026-04-21-hermes-post-api-request-capture.md.
	if len(turns) != 22 {
		t.Fatalf("want 22 turns from real capture, got %d", len(turns))
	}
	if len(quarantined) != 59 {
		t.Fatalf("want 59 quarantined from real capture, got %d", len(quarantined))
	}

	tmpl := hermesEnvelopeTemplate()
	n, err := EmitHermesTurns(em, dir, tmpl, turns, quarantined)
	if err != nil {
		t.Fatalf("EmitHermesTurns: %v", err)
	}
	if n != 22 {
		t.Fatalf("want 22 emitted, got %d", n)
	}

	lines := readJSONLLines(t, logPath)
	if len(lines) != 22 {
		t.Fatalf("want 22 JSONL lines, got %d", len(lines))
	}

	// Validate the first event's structure — all hermes emissions share
	// the same envelope shape, so checking one is sufficient for field-plumbing.
	ev := lines[0]
	if ev["event_type"] != "model_turn" {
		t.Errorf("event_type: got %v", ev["event_type"])
	}
	chainID, _ := ev["chain_id"].(string)
	if len(chainID) < len("hermes:")+32+1+16 || chainID[:7] != "hermes:" {
		t.Errorf("chain_id shape: got %q (want hermes:<32hex>:<16hex>)", chainID)
	}
	if ev["surface"] != "hermes" {
		t.Errorf("surface: got %v (translator always overrides with ModelTurn.Surface)", ev["surface"])
	}
	labels, _ := ev["labels"].(map[string]any)
	if labels["source"] != "hermes-plugin" || labels["dialect"] != "hermes" {
		t.Errorf("labels: got %+v", labels)
	}
	payload, _ := ev["payload"].(map[string]any)
	if payload["provider"] != "custom" {
		t.Errorf("payload provider: got %v (want \"custom\" — hermes normalizes ollama-launch)", payload["provider"])
	}

	// Quarantine files should have been written — 59 events, one file each.
	qdir := filepath.Join(dir, "hermes-quarantine")
	entries, err := os.ReadDir(qdir)
	if err != nil {
		t.Fatalf("read hermes-quarantine dir: %v", err)
	}
	if len(entries) != 59 {
		t.Errorf("want 59 quarantine files, got %d", len(entries))
	}
}

func TestEmitHermesTurns_Idempotent(t *testing.T) {
	em, logPath := newTestEmitter(t)
	dir := filepath.Dir(logPath)
	raw := realCaptureBytes(t)
	turns, q, _ := ParseHermesEvents(raw)
	tmpl := hermesEnvelopeTemplate()

	if _, err := EmitHermesTurns(em, dir, tmpl, turns, q); err != nil {
		t.Fatal(err)
	}
	// Same inputs replayed — chain_ids already exist in the index, no new events.
	n, err := EmitHermesTurns(em, dir, tmpl, turns, q)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second emit should be no-op, emitted %d", n)
	}
	lines := readJSONLLines(t, logPath)
	if len(lines) != 22 {
		t.Errorf("JSONL should still have 22 lines after idempotent replay, has %d", len(lines))
	}
}

func TestEmitHermesTurns_BadTemplateMissingRunID(t *testing.T) {
	em, logPath := newTestEmitter(t)
	dir := filepath.Dir(logPath)
	raw := realCaptureBytes(t)
	turns, q, _ := ParseHermesEvents(raw)

	bad := hermesEnvelopeTemplate()
	bad.RunID = ""
	n, err := EmitHermesTurns(em, dir, bad, turns, q)
	if err == nil {
		t.Fatal("want error for missing run_id, got nil")
	}
	if n != 0 {
		t.Errorf("want 0 emitted on validation fail, got %d", n)
	}
	// Invariant: validation fails BEFORE side-effects, so the quarantine
	// dir must not be created.
	if _, err := os.Stat(filepath.Join(dir, "hermes-quarantine")); err == nil {
		t.Error("quarantine dir should not exist on validation fail")
	}
}
