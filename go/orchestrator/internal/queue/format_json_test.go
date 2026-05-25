package queue

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// jsonFixedTs anchors the timestamp fields so the marshalled output
// stays byte-stable across machines and clocks.
var jsonFixedTs = time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

// TestFormatJSON_Empty_ReturnsEmptyArray pins the spec 114 edge-case
// contract: an empty queue renders as `[]\n`, not `null\n` or "".
// Downstream tools can iterate without a nil guard.
func TestFormatJSON_Empty_ReturnsEmptyArray(t *testing.T) {
	for _, name := range []string{"nil-slice", "empty-slice"} {
		t.Run(name, func(t *testing.T) {
			var in []Entry
			if name == "empty-slice" {
				in = []Entry{}
			}
			got, err := FormatJSON(in)
			if err != nil {
				t.Fatalf("FormatJSON: %v", err)
			}
			if got != "[]\n" {
				t.Errorf("want %q, got %q", "[]\n", got)
			}
		})
	}
}

// TestFormatJSON_RoundTripsThroughUnmarshal satisfies T012's contract:
// `json output round-trips through json.Unmarshal`. The decoded slice
// must equal the source slice field-for-field, including the raw
// TriggeringEvent payload.
func TestFormatJSON_RoundTripsThroughUnmarshal(t *testing.T) {
	want := []Entry{{
		PRNumber:         1057,
		Title:            "fix: handle empty review payload",
		URL:              "https://github.com/chitinhq/chitin/pull/1057",
		Reason:           "iteration_cap_hit",
		SpecRef:          "113-pr-comment-respond-loop",
		UpdatedAt:        jsonFixedTs.Add(-3 * time.Hour),
		LastAutoActionAt: jsonFixedTs.Add(-37 * time.Minute),
		TriggeringEvent: &EscalationEvent{
			EventType: "pr_iteration_escalated",
			Reason:    "iteration_cap_hit",
			PRNumber:  1057,
			Ts:        jsonFixedTs.Add(-37 * time.Minute),
			RunID:     "run-abc-123",
			Payload: json.RawMessage(`{"pr_number":1057,"reason":"iteration_cap_hit",` +
				`"rounds_attempted":3,"last_review_id":"RV_42"}`),
		},
	}}

	out, err := FormatJSON(want)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}

	var got []Entry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("Unmarshal: %v\n--- output:\n%s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	g, w := got[0], want[0]
	if g.PRNumber != w.PRNumber || g.Title != w.Title || g.URL != w.URL ||
		g.Reason != w.Reason || g.SpecRef != w.SpecRef {
		t.Errorf("scalar fields drifted:\n got %+v\nwant %+v", g, w)
	}
	if !g.UpdatedAt.Equal(w.UpdatedAt) || !g.LastAutoActionAt.Equal(w.LastAutoActionAt) {
		t.Errorf("timestamps drifted: got (%s,%s) want (%s,%s)",
			g.UpdatedAt, g.LastAutoActionAt, w.UpdatedAt, w.LastAutoActionAt)
	}
	if g.TriggeringEvent == nil {
		t.Fatal("TriggeringEvent dropped on round-trip")
	}
	if g.TriggeringEvent.EventType != w.TriggeringEvent.EventType ||
		g.TriggeringEvent.Reason != w.TriggeringEvent.Reason ||
		g.TriggeringEvent.PRNumber != w.TriggeringEvent.PRNumber ||
		g.TriggeringEvent.RunID != w.TriggeringEvent.RunID {
		t.Errorf("TriggeringEvent fields drifted: got %+v want %+v",
			g.TriggeringEvent, w.TriggeringEvent)
	}
	if !g.TriggeringEvent.Ts.Equal(w.TriggeringEvent.Ts) {
		t.Errorf("TriggeringEvent.Ts drifted: got %s want %s",
			g.TriggeringEvent.Ts, w.TriggeringEvent.Ts)
	}
	// Payload is json.RawMessage — re-decode both sides to a map so
	// whitespace differences in the raw bytes do not cause a spurious
	// inequality (the round-trip re-marshal can rearrange keys).
	var gp, wp map[string]any
	if err := json.Unmarshal(g.TriggeringEvent.Payload, &gp); err != nil {
		t.Fatalf("decode got payload: %v", err)
	}
	if err := json.Unmarshal(w.TriggeringEvent.Payload, &wp); err != nil {
		t.Fatalf("decode want payload: %v", err)
	}
	for k, v := range wp {
		if gp[k] != v {
			t.Errorf("payload[%s]: got %v want %v", k, gp[k], v)
		}
	}
}

// TestFormatJSON_RawPayloadSurvivesVerbatim guards the "raw triggering
// escalation event payload" half of FR-007: the renderer MUST NOT
// rewrite the payload bytes — opaque pass-through is the contract that
// lets downstream tooling key on payload-derived fields the renderer
// hasn't been taught about.
func TestFormatJSON_RawPayloadSurvivesVerbatim(t *testing.T) {
	// A payload key the queue package does not know about — including a
	// value with `<` which would be escaped by Go's default HTML-safe
	// encoder. The renderer disables HTML escaping precisely to keep
	// such bytes intact.
	raw := json.RawMessage(`{"future_field":"<unknown>","seq":42}`)
	in := []Entry{{
		PRNumber:  9001,
		Title:     "x",
		Reason:    "lease_lost",
		UpdatedAt: jsonFixedTs,
		TriggeringEvent: &EscalationEvent{
			EventType: "pr_iteration_escalated",
			Reason:    "lease_lost",
			PRNumber:  9001,
			Ts:        jsonFixedTs,
			Payload:   raw,
		},
	}}
	out, err := FormatJSON(in)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	if !strings.Contains(out, `"future_field": "<unknown>"`) {
		t.Errorf("raw payload field not present verbatim in output:\n%s", out)
	}
	if !strings.Contains(out, `"seq": 42`) {
		t.Errorf("raw payload numeric field not present verbatim:\n%s", out)
	}
	// Negative case: when the encoder HTML-escapes, the `<` byte
	// becomes the six-byte JSON unicode escape sequence
	// (backslash + "u003c"). Assert that did NOT happen — FR-007
	// requires verbatim payload bytes so downstream tooling can
	// hash-compare events without normalising encodings.
	if strings.Contains(out, "\\u003c") {
		t.Errorf("payload was HTML-escaped; FR-007 requires verbatim bytes:\n%s", out)
	}
}

// TestFormatJSON_OmitsAbsentOptionalFields confirms the omitempty tags
// on Entry: an entry with only the required fields does not surface a
// noisy `"url":""` / `"spec_ref":""` / `"triggering_event":null` set.
// Keeping the JSON narrow matters for the digest path where the
// payload is embedded in a Discord post.
func TestFormatJSON_OmitsAbsentOptionalFields(t *testing.T) {
	in := []Entry{{
		PRNumber:  42,
		Title:     "chain-only entry",
		Reason:    "stale_no_automation",
		UpdatedAt: jsonFixedTs,
	}}
	out, err := FormatJSON(in)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	// Pointer-typed (`*EscalationEvent`) and string-typed
	// (`url`, `spec_ref`) zero values respect `omitempty` and must be
	// absent. The `last_auto_action_at` field is `time.Time` which
	// Go's encoding/json does NOT treat as empty when zero (struct
	// zero-values are never omitted) — that field intentionally
	// renders as "0001-01-01T00:00:00Z" so a JSON consumer can
	// distinguish a real timestamp from an unset one by inspecting
	// the time.Time's `IsZero()` after parse.
	for _, banned := range []string{
		`"url"`,
		`"spec_ref"`,
		`"triggering_event"`,
	} {
		if strings.Contains(out, banned) {
			t.Errorf("optional field %s surfaced when unset:\n%s", banned, out)
		}
	}
	// Required fields still present.
	for _, want := range []string{
		`"pr_number": 42`,
		`"title": "chain-only entry"`,
		`"reason": "stale_no_automation"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("required field missing: want %s in:\n%s", want, out)
		}
	}
}

// TestFormatJSON_OutputIsIndentedAndNewlineTerminated pins the human-
// readable shape — two-space indent (the same shape `gh api` emits)
// and a trailing newline so `> queue.json` produces a POSIX text file.
func TestFormatJSON_OutputIsIndentedAndNewlineTerminated(t *testing.T) {
	out, err := FormatJSON([]Entry{{
		PRNumber:  1,
		Title:     "t",
		Reason:    "iteration_cap_hit",
		UpdatedAt: jsonFixedTs,
	}})
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output not newline-terminated: %q", out)
	}
	// Array level adds two spaces, object level adds two more —
	// the first field of each entry lives at four spaces.
	if !strings.Contains(out, "\n    \"pr_number\"") {
		t.Errorf("expected two-space indent at each nesting level:\n%s", out)
	}
}

// TestFormatJSON_PreservesOrder confirms the renderer is a pure pass-
// through over the caller's []Entry order. The filter (T004) is
// responsible for ordering; the renderer must not silently re-sort.
func TestFormatJSON_PreservesOrder(t *testing.T) {
	in := []Entry{
		{PRNumber: 3, Title: "c", Reason: "iteration_cap_hit", UpdatedAt: jsonFixedTs},
		{PRNumber: 1, Title: "a", Reason: "iteration_cap_hit", UpdatedAt: jsonFixedTs},
		{PRNumber: 2, Title: "b", Reason: "iteration_cap_hit", UpdatedAt: jsonFixedTs},
	}
	out, err := FormatJSON(in)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	var got []Entry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 3 || got[0].PRNumber != 3 || got[1].PRNumber != 1 || got[2].PRNumber != 2 {
		nums := []int{}
		for _, e := range got {
			nums = append(nums, e.PRNumber)
		}
		t.Errorf("order changed: got PR numbers %v want [3 1 2]", nums)
	}
}
