package queue

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// T012 pins the minimal acceptance contract spec 114 names for each
// format renderer:
//
//   - FormatTable     — output is column-aligned (FR-005)
//   - FormatMarkdown  — output is a valid markdown table (FR-006)
//   - FormatJSON      — output round-trips through json.Unmarshal (FR-007)
//
// Each sibling task (T005/T006/T007) carries its own thorough renderer
// test file with broader coverage (edge cases, truncation, emoji
// taxonomy, raw-payload preservation). This file is the narrow
// "renderer contract holds at all" suite the task description spells
// out — kept terse so it's the first thing future readers grep for.
//
// Like T011, this file fails to compile against main in isolation by
// design; it compiles cleanly once the spec-114 bundle merges together
// (T005/T006/T007 supply types.go + the three renderers).

// t012FixedNow anchors age math so column widths and rendered cells
// stay deterministic regardless of when the test runs.
var t012FixedNow = time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

// t012Entries are a small fixture covering every column the renderers
// touch: number widths that force tabwriter padding, a reason from the
// FR-008 taxonomy, age values that exercise each magnitude branch,
// and a spec_ref.
func t012Entries() []Entry {
	return []Entry{
		{
			PRNumber:         7,
			Title:            "short",
			URL:              "https://github.com/chitinhq/chitin/pull/7",
			Reason:           "iteration_cap_hit",
			SpecRef:          "113",
			UpdatedAt:        t012FixedNow.Add(-2 * time.Hour),
			LastAutoActionAt: t012FixedNow.Add(-5 * time.Minute),
		},
		{
			PRNumber:         12345,
			Title:            "much longer title that still fits under sixty",
			URL:              "https://github.com/chitinhq/chitin/pull/12345",
			Reason:           "sibling_rebase_failed",
			SpecRef:          "114",
			UpdatedAt:        t012FixedNow.Add(-3 * 24 * time.Hour),
			LastAutoActionAt: t012FixedNow.Add(-90 * time.Minute),
		},
	}
}

// TestT012_FormatTable_OutputIsColumnAligned verifies the table
// renderer aligns every data row's cells to the header's column
// offsets — the FR-005 alignment guarantee. text/tabwriter does the
// padding; this test is the regression gate that nobody downgrades
// it to plain Sprintf later.
func TestT012_FormatTable_OutputIsColumnAligned(t *testing.T) {
	got := FormatTable(t012Entries(), t012FixedNow)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows = 3 lines, got %d:\n%s", len(lines), got)
	}

	header := lines[0]
	// Each probe is a column header label and the literal cell text
	// expected on each of the two data rows. Every row's cell MUST
	// start at the same byte offset as the header label — that's the
	// column-alignment invariant.
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
			t.Fatalf("header missing column %q:\n%s", probe.col, header)
		}
		for i, cell := range probe.rowCells {
			if at := strings.Index(lines[i+1], cell); at != want {
				t.Errorf("col %s on row %d starts at byte %d, want %d:\n%s",
					probe.col, i+1, at, want, got)
			}
		}
	}
}

// TestT012_FormatMarkdown_OutputIsValidMarkdownTable verifies the
// markdown renderer emits a GFM-conformant table — header row,
// separator row of dashes, then data rows — with a consistent pipe
// count throughout. That's the structural minimum a GFM parser needs
// to recognise the block as a table.
func TestT012_FormatMarkdown_OutputIsValidMarkdownTable(t *testing.T) {
	got := FormatMarkdown(t012Entries(), t012FixedNow)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want header + separator + 2 rows = 4 lines, got %d:\n%s",
			len(lines), got)
	}

	// Every row begins and ends with a pipe — GFM's table-row signature.
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "|") || !strings.HasSuffix(ln, "|") {
			t.Errorf("line %d missing pipe boundary: %q", i, ln)
		}
	}

	// Pipe counts must agree across header, separator, and every data
	// row — a parser that sees a row with fewer pipes than the header
	// drops it from the table.
	want := strings.Count(lines[0], "|")
	if want < 2 {
		t.Fatalf("header has fewer than 2 pipes: %q", lines[0])
	}
	for i, ln := range lines[1:] {
		if got := strings.Count(ln, "|"); got != want {
			t.Errorf("line %d has %d pipes, want %d (matches header): %q",
				i+1, got, want, ln)
		}
	}

	// Separator row is pure pipes + dashes + spaces — GFM's rule for
	// distinguishing a table from a generic pipe-delimited block.
	sep := lines[1]
	for _, r := range sep {
		switch r {
		case '|', '-', ' ':
		default:
			t.Errorf("separator row contains non-table rune %q: %s", r, sep)
		}
	}
	if !strings.Contains(sep, "---") {
		t.Errorf("separator row missing a 3-dash run: %q", sep)
	}
}

// TestT012_FormatJSON_OutputRoundTripsThroughUnmarshal verifies that
// the JSON renderer's output is valid JSON and that decoding it back
// through json.Unmarshal reproduces the input slice. This is the
// FR-007 machine-readability contract — downstream tooling must be
// able to consume the queue without bespoke parsing.
func TestT012_FormatJSON_OutputRoundTripsThroughUnmarshal(t *testing.T) {
	entries := t012Entries()

	out, err := FormatJSON(entries)
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}

	var got []Entry
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("Unmarshal failed — output is not valid JSON: %v\noutput:\n%s",
			err, out)
	}

	if !reflect.DeepEqual(got, entries) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", got, entries)
	}
}
