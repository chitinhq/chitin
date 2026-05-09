package emit

import (
	"testing"
	"time"
)

func TestWithinDedupWindow_EmptyString(t *testing.T) {
	if withinDedupWindow("") {
		t.Error("empty timestamp should not be in dedup window")
	}
}

func TestWithinDedupWindow_InvalidTimestamp(t *testing.T) {
	if withinDedupWindow("not-a-timestamp") {
		t.Error("invalid timestamp should not be in dedup window")
	}
}

func TestWithinDedupWindow_RecentTimestamp(t *testing.T) {
	// A timestamp 1 second ago should be within the dedup window (which is minutes)
	ts := time.Now().Add(-1 * time.Second).Format(time.RFC3339)
	if !withinDedupWindow(ts) {
		t.Error("recent timestamp should be in dedup window")
	}
}

func TestWithinDedupWindow_OldTimestamp(t *testing.T) {
	// A timestamp far in the past should not be in the dedup window
	ts := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)
	if withinDedupWindow(ts) {
		t.Error("24-hour-old timestamp should not be in dedup window")
	}
}

func TestWithinDedupWindow_NanoFormat(t *testing.T) {
	ts := time.Now().Add(-1 * time.Second).Format(time.RFC3339Nano)
	if !withinDedupWindow(ts) {
		t.Error("recent nano-format timestamp should be in dedup window")
	}
}