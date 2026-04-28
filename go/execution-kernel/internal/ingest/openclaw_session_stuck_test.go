package ingest

import "testing"

func TestTranslateSessionStuck_ValidMinimal(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{
			"openclaw.state": "awaiting_model",
		},
		intAttrs: map[string]int64{
			"openclaw.ageMs": 120000,
		},
	}})

	got, quarantined := translateSingle(t, payload, translateSessionStuck)
	if len(quarantined) != 0 {
		t.Fatalf("quarantined=%+v", quarantined)
	}
	if len(got) != 1 {
		t.Fatalf("got=%d, want 1", len(got))
	}
	if got[0].State != "awaiting_model" || got[0].AgeMs != 120000 {
		t.Fatalf("mismatch: %+v", got[0])
	}
	if got[0].QueueDepthPresent {
		t.Fatalf("queue_depth_present=true, want false")
	}
	if got[0].EventType() != "session_stuck" {
		t.Fatalf("event_type=%q, want session_stuck", got[0].EventType())
	}
}

func TestTranslateSessionStuck_AllOptional(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{
			"openclaw.state":      "awaiting_model",
			"openclaw.sessionId":  "sess-abc",
			"openclaw.sessionKey": "key-xyz",
		},
		intAttrs: map[string]int64{
			"openclaw.ageMs":      120000,
			"openclaw.queueDepth": 5,
		},
	}})

	got, _ := translateSingle(t, payload, translateSessionStuck)
	s := got[0]
	if s.SessionIDExternal != "sess-abc" {
		t.Fatalf("session_id_external=%q", s.SessionIDExternal)
	}
	if s.SessionKey != "key-xyz" {
		t.Fatalf("session_key=%q", s.SessionKey)
	}
	if !s.QueueDepthPresent || s.QueueDepth != 5 {
		t.Fatalf("queue_depth: %+v", s)
	}
}

func TestTranslateSessionStuck_MissingState(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		intAttrs:   map[string]int64{"openclaw.ageMs": 120000},
	}})

	_, quarantined := translateSingle(t, payload, translateSessionStuck)
	if len(quarantined) != 1 || quarantined[0].Reason != "missing_required_attr:openclaw.state" {
		t.Fatalf("reason=%+v", quarantined)
	}
}

func TestTranslateSessionStuck_MissingAgeMs(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{"openclaw.state": "awaiting_model"},
	}})

	_, quarantined := translateSingle(t, payload, translateSessionStuck)
	if len(quarantined) != 1 || quarantined[0].Reason != "missing_required_attr:openclaw.ageMs" {
		t.Fatalf("reason=%+v", quarantined)
	}
}

func TestTranslateSessionStuck_NegativeAgeMs(t *testing.T) {
	start, _ := sampleTs(0)
	payload := buildFixture(t, "openclaw", []fixtureSpan{{
		name:       "openclaw.session.stuck",
		traceID:    sampleTraceID(0x55),
		spanID:     sampleSpanID(0x66),
		startNanos: start,
		endNanos:   start,
		stringAttrs: map[string]string{"openclaw.state": "awaiting_model"},
		intAttrs:   map[string]int64{"openclaw.ageMs": -1},
	}})

	_, quarantined := translateSingle(t, payload, translateSessionStuck)
	if len(quarantined) != 1 || quarantined[0].Reason != "invalid_value:openclaw.ageMs" {
		t.Fatalf("reason=%+v", quarantined)
	}
}
