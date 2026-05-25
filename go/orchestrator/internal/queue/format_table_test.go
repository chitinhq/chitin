package queue

import (
	"strings"
	"testing"
	"time"
)

// fixedNow anchors every test's age math so expected strings stay
// stable across machines and clocks.
var fixedNow = time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

func TestFormatTable_HeaderAndColumns(t *testing.T) {
	entries := []Entry{{
		PRNumber:         1057,
		Title:            "fix: handle empty review payload",
		Reason:           "iteration_cap_hit",
		SpecRef:          "113",
		UpdatedAt:        fixedNow.Add(-3 * time.Hour),
		LastAutoActionAt: fixedNow.Add(-37 * time.Minute),
	}}

	got := FormatTable(entries, fixedNow)

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header + 1 row = 2 lines, got %d:\n%s", len(lines), got)
	}

	header := lines[0]
	for _, col := range []string{"PR", "TITLE", "REASON", "AGE", "LAST_AUTO", "SPEC_REF"} {
		if !strings.Contains(header, col) {
			t.Errorf("header missing column %q:\n%s", col, header)
		}
	}

	row := lines[1]
	for _, want := range []string{"#1057", "iteration_cap_hit", "3h", "37m", "113"} {
		if !strings.Contains(row, want) {
			t.Errorf("row missing %q:\n%s", want, row)
		}
	}
}

func TestFormatTable_ColumnsAlignAcrossRows(t *testing.T) {
	// Two rows with very different widths force tabwriter to pad —
	// confirms the header and both data rows share the same column
	// starts (the alignment guarantee FR-005 asks for). Trailing
	// length can differ because tabwriter doesn't right-pad the
	// last column, so we check column-start positions instead.
	entries := []Entry{
		{
			PRNumber:         1,
			Title:            "a",
			Reason:           "iteration_cap_hit",
			SpecRef:          "113",
			UpdatedAt:        fixedNow.Add(-2 * time.Hour),
			LastAutoActionAt: fixedNow.Add(-5 * time.Minute),
		},
		{
			PRNumber:         9999,
			Title:            "a much longer title that still fits under sixty chars",
			Reason:           "sibling_rebase_failed",
			SpecRef:          "114",
			UpdatedAt:        fixedNow.Add(-72 * time.Hour),
			LastAutoActionAt: fixedNow.Add(-90 * time.Minute),
		},
	}

	got := FormatTable(entries, fixedNow)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", len(lines), got)
	}

	// Header label positions establish the column boundaries; every
	// data row's matching cell must start at the same offset.
	header := lines[0]
	for _, probe := range []struct {
		col      string
		rowCells [2]string
	}{
		{"REASON", [2]string{"iteration_cap_hit", "sibling_rebase_failed"}},
		{"AGE", [2]string{"2h", "3d"}},
		{"LAST_AUTO", [2]string{"5m", "1h"}},
		{"SPEC_REF", [2]string{"113", "114"}},
	} {
		want := strings.Index(header, probe.col)
		if want < 0 {
			t.Fatalf("header missing %q:\n%s", probe.col, header)
		}
		for i, cell := range probe.rowCells {
			if at := strings.Index(lines[i+1], cell); at != want {
				t.Errorf("col %s on row %d starts at %d, want %d:\n%s",
					probe.col, i+1, at, want, got)
			}
		}
	}
}

func TestFormatTable_TitleTruncatedAtSixty(t *testing.T) {
	long := strings.Repeat("x", 100)
	got := FormatTable([]Entry{{
		PRNumber:  42,
		Title:     long,
		Reason:    "iteration_cap_hit",
		UpdatedAt: fixedNow.Add(-time.Minute),
	}}, fixedNow)

	if !strings.Contains(got, strings.Repeat("x", 59)+"…") {
		t.Errorf("expected title truncated to 59 x's + ellipsis:\n%s", got)
	}
	if strings.Contains(got, strings.Repeat("x", 61)) {
		t.Errorf("title was not truncated:\n%s", got)
	}
}

func TestFormatTable_EmptyEntriesEmitsFriendlyMessage(t *testing.T) {
	// Spec 114 "edge cases": when there are no escalations the queue
	// prints "✅ no PRs need attention" instead of an empty table.
	got := FormatTable(nil, fixedNow)
	want := "✅ no PRs need attention\n"
	if got != want {
		t.Fatalf("empty-input output:\n got %q\nwant %q", got, want)
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero is dash", time.Time{}, "-"},
		{"future clamps to 0s", fixedNow.Add(5 * time.Minute), "0s"},
		{"seconds", fixedNow.Add(-45 * time.Second), "45s"},
		{"minutes", fixedNow.Add(-12 * time.Minute), "12m"},
		{"hours", fixedNow.Add(-4 * time.Hour), "4h"},
		{"days", fixedNow.Add(-72 * time.Hour), "3d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatAge(fixedNow, tc.t); got != tc.want {
				t.Errorf("formatAge(%v) = %q, want %q", tc.t, got, tc.want)
			}
		})
	}
}

func TestFormatTable_EmptyFieldsRenderAsDash(t *testing.T) {
	// Title is intentionally hyphen-free so a stray "-" in the row
	// can only come from a placeholder cell — keeps the dash count
	// from masking a missing placeholder.
	got := FormatTable([]Entry{{
		PRNumber:  7,
		Title:     "fresh PR with no automation yet",
		Reason:    "",
		SpecRef:   "",
		UpdatedAt: fixedNow.Add(-time.Hour),
		// LastAutoActionAt zero-value
	}}, fixedNow)

	// Header line + one row; expect exactly three "-" placeholders
	// (REASON, LAST_AUTO, SPEC_REF). tabwriter pads with spaces so
	// counting "-" tokens is safe.
	row := strings.Split(strings.TrimRight(got, "\n"), "\n")[1]
	dashCount := strings.Count(row, "-")
	if dashCount != 3 {
		t.Errorf("want 3 dash placeholders (REASON, LAST_AUTO, SPEC_REF), got %d:\n%s", dashCount, row)
	}
}

func TestTruncateRunes_UnicodeSafe(t *testing.T) {
	// 5-rune string of 3-byte runes — naïve byte slicing would split
	// a codepoint at byte 5 and produce mojibake.
	in := "日本語日本"
	if got := truncateRunes(in, 3); got != "日本…" {
		t.Errorf("truncateRunes(unicode, 3) = %q, want %q", got, "日本…")
	}
	if got := truncateRunes(in, 10); got != in {
		t.Errorf("truncateRunes(unicode, 10) = %q, want %q", got, in)
	}
}
