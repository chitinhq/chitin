package ingest

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/emit"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/event"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// openclawEnvelopeTemplate is a known-valid template for emitting model_turn events.
func openclawEnvelopeTemplate() *event.Event {
	return &event.Event{
		SchemaVersion:    "2",
		RunID:            "550e8400-e29b-41d4-a716-446655441000",
		SessionID:        "550e8400-e29b-41d4-a716-446655441001",
		Surface:          "openclaw-gateway",
		AgentInstanceID:  "550e8400-e29b-41d4-a716-446655441002",
		AgentFingerprint: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
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

func TestEmitEvents_SynthesizedFixtureEndToEnd(t *testing.T) {
	em, logPath := newTestEmitter(t)
	dir := filepath.Dir(logPath)
	rs := loadFixture(t)
	spans, quarantined, err := ParseOpenClawSpans(rs)
	if err != nil {
		t.Fatal(err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("want 0 quarantined, got %d", len(quarantined))
	}
	tmpl := openclawEnvelopeTemplate()
	n, err := EmitEvents(em, dir, tmpl, spans, quarantined)
	if err != nil {
		t.Fatalf("EmitEvents: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 emitted, got %d", n)
	}
	lines := readJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("want 1 line in JSONL, got %d", len(lines))
	}
	ev := lines[0]
	if ev["event_type"] != "model_turn" {
		t.Errorf("event_type: got %v", ev["event_type"])
	}
	traceIDHex := "0102030405060708090a0b0c0d0e0f10"
	spanIDHex := "a1a2a3a4a5a6a7a8"
	wantChainID := "otel:" + traceIDHex + ":" + spanIDHex
	if ev["chain_id"] != wantChainID {
		t.Errorf("chain_id: got %v, want %v", ev["chain_id"], wantChainID)
	}
	if ev["surface"] != "openclaw-gateway" {
		t.Errorf("surface: got %v", ev["surface"])
	}
	labels, _ := ev["labels"].(map[string]any)
	if labels["source"] != "otel" || labels["dialect"] != "openclaw" {
		t.Errorf("labels: got %+v", labels)
	}
	payload, _ := ev["payload"].(map[string]any)
	if payload["model_name"] != "qwen2.5:0.5b" || payload["provider"] != "ollama" {
		t.Errorf("payload model/provider: got %+v", payload)
	}
	if int(payload["input_tokens"].(float64)) != 42 ||
		int(payload["output_tokens"].(float64)) != 17 {
		t.Errorf("payload tokens: got %+v", payload)
	}
	if payload["session_id_external"] != "sp1-fixture-session" {
		t.Errorf("payload session_id_external: got %v", payload["session_id_external"])
	}
	if _, ok := ev["this_hash"]; !ok {
		t.Error("this_hash absent")
	}
	if ev["prev_hash"] != nil {
		t.Errorf("prev_hash: want nil for seq=0, got %v", ev["prev_hash"])
	}
}

func TestEmitEvents_Idempotent(t *testing.T) {
	em, logPath := newTestEmitter(t)
	dir := filepath.Dir(logPath)
	rs := loadFixture(t)
	spans, q, _ := ParseOpenClawSpans(rs)
	tmpl := openclawEnvelopeTemplate()
	if _, err := EmitEvents(em, dir, tmpl, spans, q); err != nil {
		t.Fatal(err)
	}
	// Second call, same inputs — EmitEvents detects chain already exists and skips.
	n, err := EmitEvents(em, dir, tmpl, spans, q)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second emit should be no-op, emitted %d", n)
	}
	lines := readJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Errorf("JSONL should have 1 line after idempotent replay, has %d", len(lines))
	}
}

func TestEmitEvents_BadTemplateMissingRunID(t *testing.T) {
	em, logPath := newTestEmitter(t)
	dir := filepath.Dir(logPath)
	rs := loadFixture(t)
	spans, q, _ := ParseOpenClawSpans(rs)
	bad := openclawEnvelopeTemplate()
	bad.RunID = ""
	n, err := EmitEvents(em, dir, bad, spans, q)
	if err == nil {
		t.Fatal("want error for missing run_id, got nil")
	}
	if n != 0 {
		t.Errorf("want 0 emitted on validation fail, got %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "otel-quarantine")); err == nil {
		t.Error("quarantine dir should not exist on validation fail")
	}
}

// TestEmitEvents_DoesNotMutateTemplateLabels is a regression test for the
// shallow-copy labels bug: cloneTemplate copies tmpl by value, but the
// Labels field is a map — a shared reference. Before the fix, appending
// OTEL labels to ev.Labels silently mutated tmpl.Labels. This would be
// latent for one-span batches but corrupts the template on iteration N+1
// of a multi-span batch.
//
// Invariant under test: after EmitEvents returns, tmpl.Labels is
// byte-identical to its pre-call state — no keys added, no values
// changed, even in the presence of key collisions with OTEL-provided
// labels.
func TestEmitEvents_DoesNotMutateTemplateLabels(t *testing.T) {
	em, logPath := newTestEmitter(t)
	dir := filepath.Dir(logPath)

	// Two-span payload so the emit loop iterates twice. The second
	// iteration is where the old bug manifested — template pre-polluted
	// by iteration 1's labels.
	rs := loadFixture(t)
	origSpan := rs[0].ScopeSpans[0].Spans[0]
	twin := cloneSpan(origSpan)
	twin.StartTimeUnixNano = origSpan.StartTimeUnixNano + 1_000_000_000 // +1s
	twin.EndTimeUnixNano = origSpan.EndTimeUnixNano + 1_000_000_000
	twin.SpanId = []byte{0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8}
	rs[0].ScopeSpans[0].Spans = append(rs[0].ScopeSpans[0].Spans, twin)

	spans, quarantined, err := ParseOpenClawSpans(rs)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("setup: want 2 spans, got %d", len(spans))
	}

	// Template has a non-OTEL label plus a collision on "source" — the
	// event should get the OTEL value ("otel") but the template's
	// original value ("pre-existing-marker") must survive.
	tmpl := openclawEnvelopeTemplate()
	tmpl.Labels = map[string]string{
		"custom_label": "preserved",
		"source":       "pre-existing-marker",
	}
	want := map[string]string{
		"custom_label": "preserved",
		"source":       "pre-existing-marker",
	}

	n, err := EmitEvents(em, dir, tmpl, spans, quarantined)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 emitted, got %d", n)
	}

	if len(tmpl.Labels) != len(want) {
		t.Fatalf("template labels size changed: got %d keys %+v, want %d keys %+v",
			len(tmpl.Labels), tmpl.Labels, len(want), want)
	}
	for k, v := range want {
		if got := tmpl.Labels[k]; got != v {
			t.Errorf("tmpl.Labels[%q]: got %q, want %q", k, got, v)
		}
	}
	// Explicitly assert no OTEL-namespace key leaked in.
	for _, leaked := range []string{"dialect", "otel_trace_id", "otel_span_id"} {
		if _, ok := tmpl.Labels[leaked]; ok {
			t.Errorf("template polluted with OTEL label %q: %+v", leaked, tmpl.Labels)
		}
	}

	// Confirm the emitted events actually got the OTEL-wins merge.
	lines := readJSONLLines(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("want 2 JSONL lines, got %d", len(lines))
	}
	for i, ev := range lines {
		labels, _ := ev["labels"].(map[string]any)
		if labels["source"] != "otel" {
			t.Errorf("event[%d] labels.source: got %v, want %q (OTEL must win on key collision)",
				i, labels["source"], "otel")
		}
		if labels["custom_label"] != "preserved" {
			t.Errorf("event[%d] labels.custom_label: got %v, want %q (template non-colliding key must be copied into event)",
				i, labels["custom_label"], "preserved")
		}
	}
}

func TestEmitEvents_MixedBatch(t *testing.T) {
	em, logPath := newTestEmitter(t)
	dir := filepath.Dir(logPath)
	rs := loadFixture(t)
	// Append one unmapped-name span by cloning the mapped one and renaming.
	// openclaw.message.processed does NOT route to any of the four mapped
	// translators (model.usage, webhook.processed, webhook.error,
	// session.stuck), so it must quarantine with unmapped_span_name.
	unmapped := cloneSpan(rs[0].ScopeSpans[0].Spans[0])
	unmapped.Name = "openclaw.message.processed"
	unmapped.SpanId = []byte{0xc1, 0xc2, 0xc3, 0xc4, 0xc5, 0xc6, 0xc7, 0xc8}
	rs[0].ScopeSpans[0].Spans = append(rs[0].ScopeSpans[0].Spans, unmapped)
	spans, q, _ := ParseOpenClawSpans(rs)
	if len(spans) != 1 || len(q) != 1 {
		t.Fatalf("want 1/1 spans/quarantined pre-emit, got %d/%d", len(spans), len(q))
	}
	n, err := EmitEvents(em, dir, openclawEnvelopeTemplate(), spans, q)
	if err != nil {
		t.Fatalf("EmitEvents: %v", err)
	}
	if n != 1 {
		t.Errorf("want 1 emitted, got %d", n)
	}
	lines := readJSONLLines(t, logPath)
	if len(lines) != 1 {
		t.Errorf("want 1 JSONL line, got %d", len(lines))
	}
	qdir := filepath.Join(dir, "otel-quarantine")
	entries, err := os.ReadDir(qdir)
	if err != nil {
		t.Fatalf("read quarantine dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 quarantine file, got %d", len(entries))
	}
	qBytes, _ := os.ReadFile(filepath.Join(qdir, entries[0].Name()))
	var qRec map[string]any
	if err := json.Unmarshal(qBytes, &qRec); err != nil {
		t.Fatalf("quarantine file not JSON: %v", err)
	}
	if qRec["reason"] != "unmapped_span_name:openclaw.message.processed" {
		t.Errorf("quarantine reason: got %v", qRec["reason"])
	}
}

// --- boundary-case helpers ---

// setupTestEmitter returns an emitter wired to a fresh temp dir plus a
// valid openclaw envelope template. Wraps newTestEmitter for the
// openclaw-integration case where every test needs all three.
func setupTestEmitter(t *testing.T) (*emit.Emitter, string, *event.Event) {
	t.Helper()
	em, logPath := newTestEmitter(t)
	return em, filepath.Dir(logPath), openclawEnvelopeTemplate()
}

// readEmittedEvents reads <dir>/events.jsonl and decodes each line into a
// typed *event.Event so callers can assert on struct fields (ChainID,
// Seq, etc.) without map-based type gymnastics.
func readEmittedEvents(t *testing.T, dir string) []*event.Event {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatalf("open events.jsonl: %v", err)
	}
	defer f.Close()
	var out []*event.Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev event.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("decode event line: %v", err)
		}
		out = append(out, &ev)
	}
	return out
}

// --- boundary-case tests (SP-2 T11) ---

// TestOpenClawIngest_EmptyPayload: decoding an empty TracesData yields
// (nil, nil, nil) from ParseOpenClawSpans — no spans, no quarantine, no error.
func TestOpenClawIngest_EmptyPayload(t *testing.T) {
	td := &tracepb.TracesData{}
	data, _ := proto.Marshal(td)

	rs, err := DecodeTraces(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	spans, quarantined, err := ParseOpenClawSpans(rs)
	if err != nil || spans != nil || quarantined != nil {
		t.Fatalf("empty: spans=%v quarantined=%v err=%v", spans, quarantined, err)
	}
}

// TestOpenClawIngest_AllQuarantine: a batch of entirely unmapped span
// names produces 0 translated, 2 quarantined, no error.
func TestOpenClawIngest_AllQuarantine(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		{name: "something.else", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01), startNanos: start, endNanos: end},
		{name: "another.span", traceID: sampleTraceID(0x02), spanID: sampleSpanID(0x02), startNanos: start, endNanos: end},
	})

	rs, _ := DecodeTraces(payload)
	spans, quarantined, _ := ParseOpenClawSpans(rs)
	if len(spans) != 0 {
		t.Fatalf("spans=%d, want 0", len(spans))
	}
	if len(quarantined) != 2 {
		t.Fatalf("quarantined=%d, want 2", len(quarantined))
	}
}

// TestOpenClawIngest_MixedValidInvalid: one valid + one missing-provider
// span yields 1 translated, 1 quarantined, no error.
func TestOpenClawIngest_MixedValidInvalid(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01),
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
		// Missing openclaw.provider
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x02), spanID: sampleSpanID(0x02),
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
	})

	rs, _ := DecodeTraces(payload)
	spans, quarantined, _ := ParseOpenClawSpans(rs)
	if len(spans) != 1 {
		t.Fatalf("spans=%d, want 1", len(spans))
	}
	if len(quarantined) != 1 {
		t.Fatalf("quarantined=%d, want 1", len(quarantined))
	}
}

// TestOpenClawIngest_IdenticalTsSortsBySpanID: two events with identical
// ts must be emitted in span_id lexicographic order. The tie-breaker
// is the secondary sort key in EmitEvents.
func TestOpenClawIngest_IdenticalTsSortsBySpanID(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0xbb), // lex LATER
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x02), spanID: sampleSpanID(0xaa), // lex EARLIER
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
	})

	rs, _ := DecodeTraces(payload)
	em, dir, tmpl := setupTestEmitter(t)
	spans, quarantined, _ := ParseOpenClawSpans(rs)
	n, err := EmitEvents(em, dir, tmpl, spans, quarantined)
	if err != nil || n != 2 {
		t.Fatalf("emit: n=%d err=%v", n, err)
	}

	events := readEmittedEvents(t, dir)
	if len(events) != 2 {
		t.Fatalf("events=%d", len(events))
	}
	first := events[0]
	if !strings.HasSuffix(first.ChainID, ":"+hex.EncodeToString(sampleSpanID(0xaa))) {
		t.Fatalf("first chain_id=%q, want suffix for 0xaa", first.ChainID)
	}
}

// TestOpenClawIngest_DuplicateSpanIDQuarantinesLaterSpans: when two spans
// share (trace_id, span_id), the first is translated and the rest
// quarantine with reason "duplicate_span_id". Validates T10's dedup.
func TestOpenClawIngest_DuplicateSpanIDQuarantinesLaterSpans(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01),
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
		},
		{
			name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01), // same trace+span
			startNanos: start, endNanos: end,
			stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
			intAttrs:    map[string]int64{"openclaw.tokens.input": 9, "openclaw.tokens.output": 9},
		},
	})

	rs, _ := DecodeTraces(payload)
	spans, quarantined, _ := ParseOpenClawSpans(rs)
	if len(spans) != 1 {
		t.Fatalf("spans=%d, want 1 (first wins)", len(spans))
	}
	if len(quarantined) != 1 || quarantined[0].Reason != "duplicate_span_id" {
		t.Fatalf("quarantined=%+v, want 1 with reason=duplicate_span_id", quarantined)
	}
}

// TestOpenClawIngest_IdempotentReplay: ingesting the same payload twice
// emits the event on the first call and 0 new events on the second
// (chain_id already in the index).
func TestOpenClawIngest_IdempotentReplay(t *testing.T) {
	start, end := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name: "openclaw.model.usage", traceID: sampleTraceID(0x01), spanID: sampleSpanID(0x01),
		startNanos: start, endNanos: end,
		stringAttrs: map[string]string{"openclaw.provider": "ollama", "openclaw.model": "qwen"},
		intAttrs:    map[string]int64{"openclaw.tokens.input": 1, "openclaw.tokens.output": 2},
	}})

	em, dir, tmpl := setupTestEmitter(t)

	rs1, _ := DecodeTraces(payload)
	spans1, q1, _ := ParseOpenClawSpans(rs1)
	n1, _ := EmitEvents(em, dir, tmpl, spans1, q1)

	rs2, _ := DecodeTraces(payload)
	spans2, q2, _ := ParseOpenClawSpans(rs2)
	n2, _ := EmitEvents(em, dir, tmpl, spans2, q2)

	if n1 != 1 || n2 != 0 {
		t.Fatalf("n1=%d n2=%d, want 1 and 0", n1, n2)
	}
}
