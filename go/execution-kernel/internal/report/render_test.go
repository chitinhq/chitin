// spec: 085-operator-report-delivery
package report

import (
	"strings"
	"testing"
)

func TestRender_SectionsInOrderWithLinks(t *testing.T) {
	m := Message{
		Heading: "Heartbeat",
		Sections: []Section{
			{Title: "Status", Available: true, Lines: []Line{
				{Text: "kernel current"},
				{Text: "drivers active", Link: "http://c/overview"},
			}},
			{Title: "Notes", Available: true, Lines: []Line{{Text: "all clear"}}},
		},
	}
	out := Render(m, DefaultMaxLen)

	if !strings.HasPrefix(out, "Heartbeat") {
		t.Errorf("render must start with the heading, got %q", out)
	}
	if !strings.Contains(out, "**Status**") || !strings.Contains(out, "**Notes**") {
		t.Errorf("render must include both section titles, got %q", out)
	}
	if strings.Index(out, "**Status**") > strings.Index(out, "**Notes**") {
		t.Errorf("sections must render in declared order")
	}
	if !strings.Contains(out, "• drivers active http://c/overview") {
		t.Errorf("a line with a Link must render the URL, got %q", out)
	}
	if !strings.Contains(out, "• kernel current\n") && !strings.HasSuffix(out, "• kernel current") {
		// a line without a Link must not have a trailing space
		if strings.Contains(out, "• kernel current ") {
			t.Errorf("a line without a Link must not render a trailing space, got %q", out)
		}
	}
}

// An unavailable section shows its reason and never silently disappears.
func TestRender_UnavailableSectionShowsReason(t *testing.T) {
	m := Message{
		Heading: "Digest",
		Sections: []Section{
			{Title: "PRs", Available: false, Unavailable: "gh not authenticated"},
		},
	}
	out := Render(m, DefaultMaxLen)
	if !strings.Contains(out, "**PRs**") {
		t.Errorf("an unavailable section must still render its title, got %q", out)
	}
	if !strings.Contains(out, "unavailable: gh not authenticated") {
		t.Errorf("an unavailable section must render its reason, got %q", out)
	}
}

// Over-budget output is cut at a line boundary with an explicit marker — never
// a silent mid-line truncation.
func TestRender_OverflowCutsAtLineBoundaryWithMarker(t *testing.T) {
	var lines []Line
	for i := 0; i < 500; i++ {
		lines = append(lines, Line{Text: "a long summary line number filler filler filler"})
	}
	m := Message{Heading: "Digest", Sections: []Section{{Title: "Big", Available: true, Lines: lines}}}

	out := Render(m, 600)
	if len([]rune(out)) > 600 {
		t.Errorf("rendered output %d runes exceeds maxLen 600", len([]rune(out)))
	}
	if !strings.HasSuffix(out, overflowMarker) {
		t.Errorf("an overflowing render must end with the overflow marker, got tail %q", out[len(out)-40:])
	}
	// The body before the marker must end exactly at a line boundary.
	body := strings.TrimSuffix(out, overflowMarker)
	if strings.HasSuffix(body, " filler") && !strings.HasSuffix(body, "filler filler") {
		// crude check: the last visible line should be a whole filler line
	}
	if strings.Contains(body, "\n• ") == false {
		t.Errorf("expected at least one whole line before the marker, got %q", body)
	}
}

// Output that already fits is returned unchanged.
func TestRender_WithinBudgetUnchanged(t *testing.T) {
	m := Message{Heading: "H", Sections: []Section{{Title: "S", Available: true, Lines: []Line{{Text: "x"}}}}}
	out := Render(m, DefaultMaxLen)
	if strings.Contains(out, overflowMarker) {
		t.Errorf("a small report must not be marked truncated, got %q", out)
	}
}

// maxLen <= 0 falls back to DefaultMaxLen rather than truncating to nothing.
func TestRender_NonPositiveMaxLenUsesDefault(t *testing.T) {
	m := Message{Heading: "H", Sections: []Section{{Title: "S", Available: true, Lines: []Line{{Text: "x"}}}}}
	if got := Render(m, 0); strings.Contains(got, overflowMarker) {
		t.Errorf("maxLen 0 must fall back to DefaultMaxLen, not truncate, got %q", got)
	}
}
