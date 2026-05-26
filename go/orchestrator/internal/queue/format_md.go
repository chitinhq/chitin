package queue

import (
	"fmt"
	"strings"
	"time"
)

// FormatMarkdown renders entries as a GitHub-flavoured markdown table
// (FR-006). Columns: PR, Title, Reason, Age, Last Auto, Spec. The PR
// cell is a clickable markdown link when Entry.URL is non-empty; the
// Reason cell is prefixed by an emoji drawn from the closed FR-008
// reason taxonomy for at-a-glance scannability in a Discord/Slack
// digest.
//
// Empty input produces a header-only table (matching the table
// renderer's contract); the CLI is responsible for the
// "no PRs need attention" friendly message per the spec 114 edge
// cases — the renderer always returns a syntactically valid table.
//
// The `now` argument anchors the Age and Last Auto columns; a zero
// `now` falls back to time.Now() so production callers can elide it.
func FormatMarkdown(entries []Entry, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}

	var b strings.Builder
	b.WriteString("| PR | Title | Reason | Age | Last Auto | Spec |\n")
	b.WriteString("|----|-------|--------|-----|-----------|------|\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			mdPRCell(e),
			mdEscape(e.Title),
			mdReasonCell(e.Reason),
			mdAge(now, e.UpdatedAt),
			mdAge(now, e.LastAutoActionAt),
			mdDash(e.SpecRef),
		)
	}
	return b.String()
}

// mdPRCell renders the identity cell. For PR-bearing rows it emits a
// markdown link `[#1234](url)` (or bare `#1234` when URL is unset, e.g.
// chain-only entries without a live PR snapshot). For no-PR silent-drop
// rows (PRNumber<=0) it emits `<spec_ref>/<task_id>` so the cell still
// identifies the work unit. Returns "-" if neither identity is present.
func mdPRCell(e Entry) string {
	if e.PRNumber <= 0 {
		if e.SpecRef != "" && e.TaskID != "" {
			return mdEscape(e.SpecRef + "/" + e.TaskID)
		}
		return "-"
	}
	if e.URL == "" {
		return fmt.Sprintf("#%d", e.PRNumber)
	}
	return fmt.Sprintf("[#%d](%s)", e.PRNumber, e.URL)
}

// mdReasonCell prefixes the reason kind with its taxonomy emoji. An
// empty reason renders as a dash; an unknown kind falls back to ❓ so
// future additions to the taxonomy are visible rather than silent.
func mdReasonCell(reason string) string {
	if reason == "" {
		return "-"
	}
	return reasonEmoji(reason) + " " + reason
}

// reasonEmoji maps the FR-008 closed taxonomy to a single emoji.
// Picks are tuned for distinctness at small sizes — pairs of close
// shapes (loop vs skip, lock vs hand) read different even when the
// digest is collapsed in a Discord preview.
func reasonEmoji(reason string) string {
	switch reason {
	case "iteration_cap_hit":
		return "🔁"
	case "iteration_completed_with_skips":
		return "⏭️"
	case "human_reviewer_present":
		return "👤"
	case "sibling_rebase_failed":
		return "🔀"
	case "silent_drop":
		return "📭"
	case "lease_lost":
		return "🔓"
	case "dialectic_request_changes":
		return "✋"
	case "stale_no_automation":
		return "🕰️"
	case "conflicting_persistent":
		return "⚔️"
	default:
		return "❓"
	}
}

// mdEscape escapes characters that would break a markdown table cell:
// pipes (collapse the cell boundary) and newlines (split the row).
// Empty strings render as a dash so the cell is visibly present.
func mdEscape(s string) string {
	if s == "" {
		return "-"
	}
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// mdDash returns "-" for an empty string so empty cells are visible.
func mdDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// mdAge renders the elapsed duration between t and now as a compact
// magnitude (e.g. "37s", "12m", "4h", "3d"). Mirrors the table
// renderer's formatAge so both formats agree on the same row's age
// strings; defined locally to avoid coupling the markdown renderer to
// the table renderer's private helper. Zero t renders as "-"; future
// t (clock skew) clamps to "0s".
func mdAge(now, t time.Time) string {
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
