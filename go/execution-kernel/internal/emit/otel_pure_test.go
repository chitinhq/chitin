package emit

import (
	"testing"
)

func TestSpanIDFromHash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "short"},
		{"exactly16charsxx", "exactly16charsxx"},
		{"this-is-a-very-long-hash-string", "this-is-a-very-l"},
		{"", ""},
		{"1234567890123456extra", "1234567890123456"},
	}
	for _, tc := range tests {
		got := spanIDFromHash(tc.input)
		if got != tc.want {
			t.Errorf("spanIDFromHash(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTsToUnixNano(t *testing.T) {
	ts := "2026-05-09T13:00:00Z"
	nano, err := tsToUnixNano(ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nano <= 0 {
		t.Errorf("expected positive nano timestamp, got %d", nano)
	}
}

func TestTsToUnixNano_InvalidInput(t *testing.T) {
	_, err := tsToUnixNano("not-a-timestamp")
	if err == nil {
		t.Error("expected error for invalid timestamp")
	}
}

func TestEndpointFromEnv_Empty(t *testing.T) {
	// When neither env var is set, should return empty
	result := endpointFromEnv()
	// Result depends on env — just verify it doesn't panic
	_ = result
}