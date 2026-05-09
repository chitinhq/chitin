package chain

import "testing"

func TestLogicalHash(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   string
		wantEmpty bool
	}{
		{"empty event type returns empty", "", `{"decision":"allow"}`, true},
		{"non-empty event type produces hash", "decision", `{"decision":"allow"}`, false},
		{"non-empty event type with empty payload", "decision", "", false},
		{"different event types produce different hashes", "decision", `{"decision":"allow"}`, false},
		{"same input produces same hash (deterministic)", "session_start", `{"session_id":"abc"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LogicalHash(tt.eventType, []byte(tt.payload))
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("LogicalHash(%q, %q) = %q, want empty string", tt.eventType, tt.payload, got)
				}
			} else {
				if got == "" {
					t.Errorf("LogicalHash(%q, %q) = empty, want non-empty hash", tt.eventType, tt.payload)
				}
			}
		})
	}

	// Determinism: same input produces same output
	got1 := LogicalHash("decision", []byte(`{"decision":"allow"}`))
	got2 := LogicalHash("decision", []byte(`{"decision":"allow"}`))
	if got1 != got2 {
		t.Errorf("LogicalHash is non-deterministic: %q != %q", got1, got2)
	}

	// Different event types produce different hashes
	gotA := LogicalHash("session_start", []byte(`{}`))
	gotB := LogicalHash("decision", []byte(`{}`))
	if gotA == gotB {
		t.Errorf("different event types should produce different hashes, got same: %q", gotA)
	}

	// Different payloads produce different hashes
	gotX := LogicalHash("decision", []byte(`{"decision":"allow"}`))
	gotY := LogicalHash("decision", []byte(`{"decision":"deny"}`))
	if gotX == gotY {
		t.Errorf("different payloads should produce different hashes, got same: %q", gotX)
	}

	// Hash is hex-encoded sha256 (64 chars)
	h := LogicalHash("decision", []byte(`{}`))
	if len(h) != 64 {
		t.Errorf("LogicalHash hex length = %d, want 64 (sha256 hex)", len(h))
	}
}