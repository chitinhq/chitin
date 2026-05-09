package emit

import (
	"testing"
	"time"
)

func TestWithinDedupWindow_ValidRecentTimestamp(t *testing.T) {
	ts := time.Now().UTC().Add(-10 * time.Second).Format(time.RFC3339)
	if !withinDedupWindow(ts) {
		t.Fatalf("expected recent timestamp to be within dedup window: %s", ts)
	}
}

func TestWithinDedupWindow_StaleTimestamp(t *testing.T) {
	ts := time.Now().UTC().Add(-60 * time.Second).Format(time.RFC3339)
	if withinDedupWindow(ts) {
		t.Fatalf("expected stale timestamp to be outside dedup window: %s", ts)
	}
}

func TestWithinDedupWindow_EmptyString(t *testing.T) {
	if withinDedupWindow("") {
		t.Fatal("empty string should not be within dedup window")
	}
}

func TestWithinDedupWindow_InvalidFormat(t *testing.T) {
	if withinDedupWindow("not-a-timestamp") {
		t.Fatal("invalid timestamp should not be within dedup window")
	}
}

func TestWithinDedupWindow_NanoVariant(t *testing.T) {
	ts := time.Now().UTC().Add(-5 * time.Second).Format(time.RFC3339Nano)
	if !withinDedupWindow(ts) {
		t.Fatalf("expected RFC3339Nano timestamp to be within dedup window: %s", ts)
	}
}

func TestWithinDedupWindow_ExactlyAtBoundary(t *testing.T) {
	// Just inside the 30s window
	ts := time.Now().UTC().Add(-29 * time.Second).Format(time.RFC3339)
	if !withinDedupWindow(ts) {
		t.Fatal("29s old timestamp should be within 30s dedup window")
	}
	// Just outside the 30s window
	ts2 := time.Now().UTC().Add(-31 * time.Second).Format(time.RFC3339)
	if withinDedupWindow(ts2) {
		t.Fatal("31s old timestamp should be outside 30s dedup window")
	}
}