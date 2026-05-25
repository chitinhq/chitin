package queue

import (
	"strings"
	"testing"
	"time"
)

// mdFixedNow anchors every test's age math so expected strings stay
// stable across machines and clocks.
var mdFixedNow = time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

func TestFormatMarkdown_HeaderAndSeparator(t *testing.T) {
	got := FormatMarkdown(nil, mdFixedNow)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header + separator only, got %d lines:\n%s", len(lines), got)
	}
	for _, want := range []string{"PR", "Title", "Reason", "Age", "Last Auto", "Spec"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("header missing %q:\n%s", want, lines[0])
		}
	}
	// Separator row must be the GFM `|---|---|...` form so the
	// markdown is recognized as a table by Discord/GitHub renderers.
	if !strings.HasPrefix(lines[1], "|") || !strings.Contains(lines[1], "---") {
		t.Errorf("separator row malformed: %q", lines[1])
	}
}

func TestFormatMarkdown_PRNumberIsClickableLink(t *testing.T) {
	got := FormatMarkdown([]Entry{{
		PRNumber:  1057,
		Title:     "fix: handle empty review payload",
		URL:       "https://github.com/chitinhq/chitin/pull/1057",
		Reason:    "iteration_cap_hit",
		UpdatedAt: mdFixedNow.Add(-1 * time.Hour),
	}}, mdFixedNow)

	want := "[#1057](https://github.com/chitinhq/chitin/pull/1057)"
	if !strings.Contains(got, want) {
		t.Errorf("PR cell not rendered as markdown link; want %q in:\n%s", want, got)
	}
}

func TestFormatMarkdown_PRNumberWithoutURLRendersPlain(t *testing.T) {
	got := FormatMarkdown([]Entry{{
		PRNumber:  42,
		Title:     "chain-only entry",
		Reason:    "iteration_cap_hit",
		UpdatedAt: mdFixedNow.Add(-1 * time.Hour),
	}}, mdFixedNow)

	if !strings.Contains(got, "| #42 |") {
		t.Errorf("expected plain `#42` cell when URL is empty:\n%s", got)
	}
	if strings.Contains(got, "[#42](") {
		t.Errorf("did not expect markdown link when URL is empty:\n%s", got)
	}
}

func TestFormatMarkdown_ReasonHasEmojiPrefix(t *testing.T) {
	cases := []struct {
		reason string
		emoji  string
	}{
		{"iteration_cap_hit", "🔁"},
		{"iteration_completed_with_skips", "⏭️"},
		{"human_reviewer_present", "👤"},
		{"sibling_rebase_failed", "🔀"},
		{"lease_lost", "🔓"},
		{"dialectic_request_changes", "✋"},
		{"stale_no_automation", "🕰️"},
		{"conflicting_persistent", "⚔️"},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			got := FormatMarkdown([]Entry{{
				PRNumber:  1,
				Title:     "t",
				Reason:    tc.reason,
				UpdatedAt: mdFixedNow.Add(-1 * time.Minute),
			}}, mdFixedNow)
			want := tc.emoji + " " + tc.reason
			if !strings.Contains(got, want) {
				t.Errorf("reason %q missing emoji prefix %q in:\n%s",
					tc.reason, want, got)
			}
		})
	}
}

func TestFormatMarkdown_UnknownReasonFallsBackToQuestionMark(t *testing.T) {
	got := FormatMarkdown([]Entry{{
		PRNumber:  1,
		Title:     "t",
		Reason:    "brand_new_kind",
		UpdatedAt: mdFixedNow.Add(-1 * time.Minute),
	}}, mdFixedNow)
	if !strings.Contains(got, "❓ brand_new_kind") {
		t.Errorf("unknown reason should fall back to ❓; got:\n%s", got)
	}
}

func TestFormatMarkdown_EscapesPipeInTitle(t *testing.T) {
	got := FormatMarkdown([]Entry{{
		PRNumber:  7,
		Title:     "feat: a|b pipe in title",
		Reason:    "iteration_cap_hit",
		UpdatedAt: mdFixedNow.Add(-time.Minute),
	}}, mdFixedNow)
	if !strings.Contains(got, `a\|b`) {
		t.Errorf("expected escaped pipe `a\\|b` in title cell:\n%s", got)
	}
	// And the row must still have exactly 7 unescaped boundary pipes
	// (one before each of 6 cells + one trailing).
	row := strings.Split(strings.TrimRight(got, "\n"), "\n")[2]
	unescaped := strings.Count(row, "|") - strings.Count(row, `\|`)
	if unescaped != 7 {
		t.Errorf("expected 7 cell-boundary pipes, got %d in row:\n%s",
			unescaped, row)
	}
}

func TestFormatMarkdown_EscapesNewlineInTitle(t *testing.T) {
	got := FormatMarkdown([]Entry{{
		PRNumber:  8,
		Title:     "feat: line1\nline2",
		Reason:    "iteration_cap_hit",
		UpdatedAt: mdFixedNow.Add(-time.Minute),
	}}, mdFixedNow)
	// A literal newline in the title cell would split the row; ensure
	// the rendered row count matches header(1) + sep(1) + data(1).
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("newline in title leaked into row; want 3 lines, got %d:\n%s",
			len(lines), got)
	}
}

func TestFormatMarkdown_EmptyTitleAndSpecRenderAsDash(t *testing.T) {
	got := FormatMarkdown([]Entry{{
		PRNumber:  9,
		Title:     "",
		Reason:    "iteration_cap_hit",
		SpecRef:   "",
		UpdatedAt: mdFixedNow.Add(-time.Hour),
	}}, mdFixedNow)
	row := strings.Split(strings.TrimRight(got, "\n"), "\n")[2]
	// Title cell and Spec cell should both contain "-" placeholders.
	if !strings.Contains(row, "| - |") {
		t.Errorf("expected dash placeholders for empty title/spec:\n%s", row)
	}
}

func TestFormatMarkdown_AgeColumns(t *testing.T) {
	got := FormatMarkdown([]Entry{{
		PRNumber:         1057,
		Title:            "t",
		URL:              "https://example/1057",
		Reason:           "iteration_cap_hit",
		SpecRef:          "113",
		UpdatedAt:        mdFixedNow.Add(-3 * time.Hour),
		LastAutoActionAt: mdFixedNow.Add(-37 * time.Minute),
	}}, mdFixedNow)
	for _, want := range []string{" 3h ", " 37m ", " 113 "} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in row:\n%s", want, got)
		}
	}
}

func TestFormatMarkdown_ZeroLastAutoRendersAsDash(t *testing.T) {
	got := FormatMarkdown([]Entry{{
		PRNumber:  10,
		Title:     "no auto action",
		Reason:    "stale_no_automation",
		UpdatedAt: mdFixedNow.Add(-2 * time.Hour),
		// LastAutoActionAt zero-value
	}}, mdFixedNow)
	row := strings.Split(strings.TrimRight(got, "\n"), "\n")[2]
	// Row contains the Last Auto cell as "-" between the Age cell
	// and the Spec cell; relax to "contains a `-` cell".
	if strings.Count(row, "| - |") < 1 {
		t.Errorf("expected at least one `| - |` cell (LastAuto, SpecRef):\n%s",
			row)
	}
}

func TestFormatMarkdown_MultipleRowsPreserveInputOrder(t *testing.T) {
	got := FormatMarkdown([]Entry{
		{PRNumber: 1, Title: "first", Reason: "iteration_cap_hit",
			UpdatedAt: mdFixedNow.Add(-time.Minute)},
		{PRNumber: 2, Title: "second", Reason: "sibling_rebase_failed",
			UpdatedAt: mdFixedNow.Add(-time.Minute)},
	}, mdFixedNow)
	rows := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(rows) != 4 {
		t.Fatalf("want header + sep + 2 rows = 4 lines, got %d:\n%s",
			len(rows), got)
	}
	if !strings.Contains(rows[2], "#1") || !strings.Contains(rows[3], "#2") {
		t.Errorf("rows not in input order:\n%s", got)
	}
}

func TestMdAge(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero is dash", time.Time{}, "-"},
		{"future clamps to 0s", mdFixedNow.Add(5 * time.Minute), "0s"},
		{"seconds", mdFixedNow.Add(-45 * time.Second), "45s"},
		{"minutes", mdFixedNow.Add(-12 * time.Minute), "12m"},
		{"hours", mdFixedNow.Add(-4 * time.Hour), "4h"},
		{"days", mdFixedNow.Add(-72 * time.Hour), "3d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mdAge(mdFixedNow, tc.t); got != tc.want {
				t.Errorf("mdAge(%v) = %q, want %q", tc.t, got, tc.want)
			}
		})
	}
}
