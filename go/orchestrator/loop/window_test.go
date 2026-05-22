package loop

import (
	"testing"
	"time"
)

// ts is a tiny fixed-time helper — minute n of a fixed day. Tests use it so a
// window's bounds and a record's timestamp are exact and comparable.
func ts(minute int) time.Time {
	return time.Date(2026, 5, 21, 12, minute, 0, 0, time.UTC)
}

// rec builds a telemetry record for tests.
func rec(id string, src TelemetrySource, minute int, outcome, sig, specRef string) TelemetryRecord {
	return TelemetryRecord{
		ID: id, Source: src, Timestamp: ts(minute),
		Kind: "test_event", Outcome: outcome, Signature: sig, SpecRef: specRef,
		Summary: id + " summary",
	}
}

// TestWindow_Contains_Bounds proves the (Since, Until] half-open/closed
// boundary: a record AT the lower bound belongs to the prior cycle; a record
// AT the upper bound belongs to this cycle. Two consecutive cycles never both
// claim — and never skip — the boundary instant (spec 078 FR-011).
func TestWindow_Contains_Bounds(t *testing.T) {
	w := TelemetryWindow{Since: ts(10), Until: ts(20)}

	cases := []struct {
		name   string
		minute int
		want   bool
	}{
		{"at lower bound is excluded", 10, false},
		{"just above lower bound is included", 11, true},
		{"interior is included", 15, true},
		{"at upper bound is included", 20, true},
		{"just above upper bound is excluded", 21, false},
		{"below the window is excluded", 5, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := w.Contains(rec("r", SourceCI, c.minute, "failure", "sig", "076"))
			if got != c.want {
				t.Errorf("Contains(minute %d) = %v, want %v", c.minute, got, c.want)
			}
		})
	}
}

// TestWindow_Contains_ZeroSince proves a zero Since is an unbounded lower
// bound — the first cycle ingests everything up to Until.
func TestWindow_Contains_ZeroSince(t *testing.T) {
	w := TelemetryWindow{Until: ts(20)} // Since is the zero time.
	if !w.Contains(rec("r", SourceCI, 1, "failure", "sig", "076")) {
		t.Error("a zero Since must be an unbounded lower bound — the first cycle ingests from the start")
	}
}

// TestWindow_Empty proves an empty/zero-record window reports Empty — the
// valid empty-cycle outcome (spec 078 edge case: empty telemetry window).
func TestWindow_Empty(t *testing.T) {
	if !(TelemetryWindow{}).Empty() {
		t.Error("a zero-record window must report Empty")
	}
	w := TelemetryWindow{Records: []TelemetryRecord{rec("r", SourceCI, 5, "failure", "s", "076")}}
	if w.Empty() {
		t.Error("a window with a record must not report Empty")
	}
}

// TestWindow_Sorted_Deterministic proves Sorted imposes a total, replay-stable
// order regardless of input order — timestamp, source, kind, then id.
func TestWindow_Sorted_Deterministic(t *testing.T) {
	w := TelemetryWindow{Records: []TelemetryRecord{
		rec("z", SourceCI, 5, "failure", "s", "076"),
		rec("a", SourceCI, 5, "failure", "s", "076"), // same ts/source — id breaks the tie.
		rec("m", SourceAgent, 3, "failure", "s", "076"),
	}}
	got := w.Sorted()
	want := []string{"m", "a", "z"} // minute 3 first; then minute 5 ordered by id.
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("Sorted()[%d].ID = %q, want %q (full: %v)", i, got[i].ID, id, ids(got))
		}
	}
}

// TestWindow_Merge proves Merge accumulates a source's records into the window.
func TestWindow_Merge(t *testing.T) {
	w := TelemetryWindow{Since: ts(0), Until: ts(60)}
	w.Merge([]TelemetryRecord{rec("a", SourceCI, 5, "failure", "s", "076")})
	w.Merge([]TelemetryRecord{rec("b", SourcePR, 6, "failure", "s", "076")})
	if len(w.Records) != 2 {
		t.Fatalf("after two merges window has %d records, want 2", len(w.Records))
	}
}

// ids extracts record ids, for readable failure messages.
func ids(recs []TelemetryRecord) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.ID
	}
	return out
}
