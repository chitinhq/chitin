package emit

import (
	"testing"
	"time"
)

func TestWithinDedupWindow(t *testing.T) {
	// Empty string → false (no dedup)
	if withinDedupWindow("") {
		t.Error("empty string should return false")
	}
	// Invalid format → false
	if withinDedupWindow("not-a-timestamp") {
		t.Error("invalid timestamp should return false")
	}
	// Recent timestamp → true (within dedup window)
	recent := time.Now().Add(-time.Second).Format(time.RFC3339Nano)
	if !withinDedupWindow(recent) {
		t.Error("recent timestamp should be within dedup window")
	}
	// RFC3339 (without nano) → true
	recentRFC := time.Now().Add(-time.Second).Format(time.RFC3339)
	if !withinDedupWindow(recentRFC) {
		t.Error("recent RFC3339 timestamp should be within dedup window")
	}
	// Very old timestamp → false (beyond dedup window)
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if withinDedupWindow(old) {
		t.Error("old timestamp should be outside dedup window")
	}
}

func TestSpanIDFromHash(t *testing.T) {
	// Normal 64-char hash
	h := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	got := spanIDFromHash(h)
	if got != "0123456789abcdef" {
		t.Errorf("spanIDFromHash(64-char) = %q, want %q", got, "0123456789abcdef")
	}
	// Short hash
	short := "abcd"
	got = spanIDFromHash(short)
	if got != "abcd" {
		t.Errorf("spanIDFromHash(short) = %q, want %q", got, "abcd")
	}
}

func TestTsToUnixNano(t *testing.T) {
	// Known timestamp
	ts := "2026-05-09T12:00:00.000000000Z"
	got, err := tsToUnixNano(ts)
	if err != nil {
		t.Fatalf("tsToUnixNano(%q) error: %v", ts, err)
	}
	// Verify it's in the right ballpark (should be after 2020 and before 2030)
	if got < 1577836800000000000 || got > 1893456000000000000 {
		t.Errorf("tsToUnixNano(%q) = %d, want value in 2020-2030 range", ts, got)
	}

	// Invalid timestamp
	_, err = tsToUnixNano("not-a-timestamp")
	if err == nil {
		t.Error("expected error for invalid timestamp")
	}
}

func TestTraceIDFromChainID(t *testing.T) {
	// Valid UUID
	got := traceIDFromChainID("505c4216-bc0a-49d1-b512-55df4d6563c0")
	if got != "505c4216bc0a49d1b51255df4d6563c0" {
		t.Errorf("traceIDFromChainID(UUID) = %q, want %q", got, "505c4216bc0a49d1b51255df4d6563c0")
	}

	// Already hex without hyphens (32 hex chars)
	short := "0123456789abcdef0123456789abcdef"
	got = traceIDFromChainID(short)
	if got != short {
		t.Errorf("traceIDFromChainID(32-hex) = %q, want %q", got, short)
	}

	// Too short
	got = traceIDFromChainID("abc")
	if got != "" {
		t.Errorf("traceIDFromChainID(short) = %q, want empty string", got)
	}

	// Contains non-hex characters
	got = traceIDFromChainID("505c4216-bc0a-49d1-b512-55df4d6563zz")
	if got != "" {
		t.Errorf("traceIDFromChainID(non-hex) = %q, want empty string", got)
	}

	// Uppercase hex should normalize to lowercase
	got = traceIDFromChainID("AABBCCDD-1122-3344-5566-7788990011AA")
	if got != "aabbccdd1122334455667788990011aa" {
		t.Errorf("traceIDFromChainID(uppercase) = %q, want %q", got, "aabbccdd1122334455667788990011aa")
	}
}