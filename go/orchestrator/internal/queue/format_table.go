package queue

import (
	"bytes"
	"fmt"
	"text/tabwriter"
	"time"
)

// titleMaxRunes is the FR-005 hard cap for the TITLE column. Titles
// longer than this are truncated with a trailing ellipsis so the table
// stays scannable in a typical 200-column terminal.
const titleMaxRunes = 60

// emptyQueueMessage is the spec 114 "edge cases" output when the
// caller has nothing to escalate — friendlier than a header-only
// table.
const emptyQueueMessage = "✅ no PRs need attention\n"

// FormatTable renders entries as a fixed-column text table using
// text/tabwriter (FR-005). Columns: PR, TITLE, REASON, AGE,
// LAST_AUTO, SPEC_REF.
//
// Ordering is the caller's responsibility — the filter (T004) sorts
// entries before handing them off; this function is a pure renderer
// and preserves the input order so tests are deterministic.
//
// The `now` argument anchors the AGE and LAST_AUTO columns; passing a
// zero `now` (the default) tells the renderer to fall back to
// time.Now() so production callers can elide the argument.
func FormatTable(entries []Entry, now time.Time) string {
	if len(entries) == 0 {
		return emptyQueueMessage
	}
	if now.IsZero() {
		now = time.Now()
	}

	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PR\tTITLE\tREASON\tAGE\tLAST_AUTO\tSPEC_REF")
	for _, e := range entries {
		fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\t%s\n",
			e.PRNumber,
			truncateRunes(e.Title, titleMaxRunes),
			defaultDash(e.Reason),
			formatAge(now, e.UpdatedAt),
			formatAge(now, e.LastAutoActionAt),
			defaultDash(e.SpecRef),
		)
	}
	_ = tw.Flush()
	return buf.String()
}

// truncateRunes shortens s to at most n runes, replacing the final
// character with U+2026 (HORIZONTAL ELLIPSIS) when truncation occurred.
// Rune-aware so multi-byte titles don't slice mid-codepoint.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(rs[:n-1]) + "…"
}

// defaultDash returns "-" for an empty string so empty cells render as
// a visible placeholder rather than a column-collapsing blank.
func defaultDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatAge renders the elapsed duration between t and now as a
// compact magnitude with a single unit suffix (e.g. "37s", "12m",
// "4h", "3d"). Width is variable — small magnitudes are two chars,
// three-digit magnitudes are four. A zero t renders as "-" since
// "now - zero" is meaningless. A future t (clock skew or test
// injection) clamps to "0s" rather than producing a negative duration.
func formatAge(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
