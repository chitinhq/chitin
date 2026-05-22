// Package report composes operator-facing heartbeat and telemetry-digest
// reports (spec 085). It reads telemetry and renders skimmable messages; it
// never posts or sends anything — delivery is the swarm script's job, so the
// kernel stays within the Constitution §1 side-effect boundary.
package report

import "strings"

// Line is one summary row in a report section.
type Line struct {
	Text string // the summary text
	Link string // optional chitin-console URL; empty means no link
}

// Section is a titled group of lines within a report.
type Section struct {
	Title       string
	Lines       []Line
	Available   bool   // false when the underlying telemetry source could not be read
	Unavailable string // reason shown in place of lines when Available is false
}

// Message is a fully-composed report ready to render.
type Message struct {
	Heading  string
	Sections []Section
}

// DefaultMaxLen bounds a rendered report. Discord's hard message limit is 2000
// characters; the headroom absorbs mention/markdown expansion.
const DefaultMaxLen = 1800

// overflowMarker is appended when a report is cut to fit DefaultMaxLen. It is
// explicit so a truncation is never silent (spec 085 edge cases).
const overflowMarker = "\n… (truncated — see chitin-console)"

// Render turns a Message into a skimmable plain-text report bounded to maxLen
// runes. Sections render in declared order. An unavailable section shows its
// reason instead of lines — it is never silently dropped. If the body would
// exceed maxLen, rendering stops at the last whole line that fits and appends
// overflowMarker — never a silent mid-line cut.
func Render(m Message, maxLen int) string {
	if maxLen <= 0 {
		maxLen = DefaultMaxLen
	}
	var b strings.Builder
	b.WriteString(m.Heading)
	for _, s := range m.Sections {
		b.WriteString("\n\n**")
		b.WriteString(s.Title)
		b.WriteString("**")
		if !s.Available {
			b.WriteString("\n⚠ unavailable: ")
			b.WriteString(s.Unavailable)
			continue
		}
		for _, ln := range s.Lines {
			b.WriteString("\n• ")
			b.WriteString(ln.Text)
			if ln.Link != "" {
				b.WriteString(" ")
				b.WriteString(ln.Link)
			}
		}
	}
	return boundRunes(b.String(), maxLen)
}

// boundRunes returns s unchanged when it fits in maxLen runes. Otherwise it
// cuts at the last newline that keeps the result plus overflowMarker within
// maxLen and appends the marker. The cut is always at a line boundary; if no
// newline fits, the head is taken empty so only the marker remains.
func boundRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	budget := maxLen - len([]rune(overflowMarker))
	if budget < 0 {
		budget = 0
	}
	if budget > len(runes) {
		budget = len(runes)
	}
	head := string(runes[:budget])
	if i := strings.LastIndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	} else {
		head = ""
	}
	return head + overflowMarker
}
